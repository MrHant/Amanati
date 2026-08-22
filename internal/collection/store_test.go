// External test package: these tests need a real importer, and importers
// import this one.
package collection_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mrhant/amanati/internal/collection"
	_ "github.com/mrhant/amanati/internal/collection/postman"
)

const (
	collectionJSON = `{
  "info": { "name": "Demo", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json" },
  "variable": [{ "key": "host", "value": "example.com" }],
  "item": [{ "name": "Get", "request": { "method": "GET", "url": "https://{{host}}/x" } }]
}`
	prodEnvJSON = `{
  "name": "prod",
  "values": [{ "key": "host", "value": "api.example.com", "enabled": true }],
  "_postman_variable_scope": "environment"
}`
	stagingEnvJSON = `{
  "name": "staging",
  "values": [{ "key": "host", "value": "staging.example.com", "enabled": true }],
  "_postman_variable_scope": "environment"
}`
)

// writeAll drops a set of files into one temp directory.
func writeAll(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestImportAttachesSiblingEnvironments(t *testing.T) {
	dir := writeAll(t, map[string]string{
		"demo.postman_collection.json":     collectionJSON,
		"prod.postman_environment.json":    prodEnvJSON,
		"staging.postman_environment.json": stagingEnvJSON,
		"package.json":                     `{"name":"unrelated"}`,
	})

	c, err := collection.NewStore().Import(filepath.Join(dir, "demo.postman_collection.json"))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	// os.ReadDir sorts, so the order here is not the map iteration order above.
	names := c.EnvNames()
	if len(names) != 2 || names[0] != "prod" || names[1] != "staging" {
		t.Fatalf("environments = %v, want the two exported next to the collection", names)
	}
	if c.ActiveEnv() != "prod" {
		t.Errorf("active = %q, want the first environment selected on import", c.ActiveEnv())
	}
	if got := c.Vars(c.ActiveEnv()).Expand("{{host}}"); got != "api.example.com" {
		t.Errorf("expanded = %q, want the environment to win over the collection variable", got)
	}
}

func TestImportWithoutEnvironments(t *testing.T) {
	dir := writeAll(t, map[string]string{"demo.postman_collection.json": collectionJSON})

	c, err := collection.NewStore().Import(filepath.Join(dir, "demo.postman_collection.json"))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(c.EnvNames()) != 0 || c.ActiveEnv() != "" {
		t.Errorf("envs = %v / active = %q, want none", c.EnvNames(), c.ActiveEnv())
	}
	if got := c.Vars("").Expand("{{host}}"); got != "example.com" {
		t.Errorf("expanded = %q, want the collection variable", got)
	}
}

func TestImportEnvAttachesAndSelects(t *testing.T) {
	dir := writeAll(t, map[string]string{"demo.postman_collection.json": collectionJSON})
	elsewhere := writeAll(t, map[string]string{"staging.postman_environment.json": stagingEnvJSON})

	store := collection.NewStore()
	c, err := store.Import(filepath.Join(dir, "demo.postman_collection.json"))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	got, name, err := store.ImportEnv(c.ID, filepath.Join(elsewhere, "staging.postman_environment.json"))
	if err != nil {
		t.Fatalf("ImportEnv: %v", err)
	}
	if name != "staging" || got != c {
		t.Fatalf("ImportEnv = %q on %v", name, got)
	}
	if c.ActiveEnv() != "staging" {
		t.Errorf("active = %q, want the freshly imported environment", c.ActiveEnv())
	}
	if got := c.Vars(c.ActiveEnv()).Expand("{{host}}"); got != "staging.example.com" {
		t.Errorf("expanded = %q", got)
	}
}

func TestImportEnvReplacesSameName(t *testing.T) {
	dir := writeAll(t, map[string]string{
		"demo.postman_collection.json":     collectionJSON,
		"staging.postman_environment.json": stagingEnvJSON,
	})
	updated := writeAll(t, map[string]string{
		"staging.postman_environment.json": `{"name":"staging","values":[{"key":"host","value":"new.example.com"}],"_postman_variable_scope":"environment"}`,
	})

	store := collection.NewStore()
	c, err := store.Import(filepath.Join(dir, "demo.postman_collection.json"))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if _, _, err := store.ImportEnv(c.ID, filepath.Join(updated, "staging.postman_environment.json")); err != nil {
		t.Fatalf("ImportEnv: %v", err)
	}

	if names := c.EnvNames(); len(names) != 1 {
		t.Fatalf("environments = %v, want the re-import to replace rather than duplicate", names)
	}
	if got := c.Vars("staging").Expand("{{host}}"); got != "new.example.com" {
		t.Errorf("expanded = %q, want the re-imported value", got)
	}
}

func TestImportEnvErrors(t *testing.T) {
	dir := writeAll(t, map[string]string{
		"demo.postman_collection.json":     collectionJSON,
		"staging.postman_environment.json": stagingEnvJSON,
	})
	store := collection.NewStore()
	c, err := store.Import(filepath.Join(dir, "demo.postman_collection.json"))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if _, _, err := store.ImportEnv("nope", filepath.Join(dir, "staging.postman_environment.json")); err == nil {
		t.Error("expected an error for an unknown collection")
	}
	if _, _, err := store.ImportEnv(c.ID, filepath.Join(dir, "demo.postman_collection.json")); err == nil {
		t.Error("expected an error when handed a collection instead of an environment")
	}
}

func TestImportRejectsEnvironmentFileWithHint(t *testing.T) {
	dir := writeAll(t, map[string]string{"staging.postman_environment.json": stagingEnvJSON})

	_, err := collection.NewStore().Import(filepath.Join(dir, "staging.postman_environment.json"))
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "environment") {
		t.Errorf("error = %q, want it to name the problem", err)
	}
}

func TestSetEnv(t *testing.T) {
	dir := writeAll(t, map[string]string{
		"demo.postman_collection.json":     collectionJSON,
		"staging.postman_environment.json": stagingEnvJSON,
	})
	store := collection.NewStore()
	c, err := store.Import(filepath.Join(dir, "demo.postman_collection.json"))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if err := store.SetEnv(c.ID, ""); err != nil {
		t.Fatalf("SetEnv to none = %v, want it to be selectable", err)
	}
	if c.ActiveEnv() != "" {
		t.Errorf("active = %q, want none", c.ActiveEnv())
	}
	if err := store.SetEnv(c.ID, "staging"); err != nil {
		t.Fatalf("SetEnv: %v", err)
	}
	if c.ActiveEnv() != "staging" {
		t.Errorf("active = %q", c.ActiveEnv())
	}
	if err := store.SetEnv(c.ID, "ghost"); err == nil {
		t.Error("expected an error for an environment the collection does not have")
	}
	if c.ActiveEnv() != "staging" {
		t.Errorf("active = %q, want a rejected switch to change nothing", c.ActiveEnv())
	}
}

func TestAcceptsEnvFiles(t *testing.T) {
	dir := writeAll(t, map[string]string{"demo.postman_collection.json": collectionJSON})
	c, err := collection.NewStore().Import(filepath.Join(dir, "demo.postman_collection.json"))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if !c.AcceptsEnvFiles() {
		t.Error("a Postman collection should take environment files")
	}

	bruno := &collection.Collection{Format: "bruno"}
	if bruno.AcceptsEnvFiles() {
		t.Error("Bruno keeps its environments in the collection folder")
	}
}

// Runs against the checked-in export rather than a fixture.
func TestImportSampleCollectionWithEnvironments(t *testing.T) {
	path := filepath.Join("..", "..", "sampledata", "postman", "col2_httpbin_envs", "httpbin-envs.postman_collection.json")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("sample data not present: %v", err)
	}

	c, err := collection.NewStore().Import(path)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if names := c.EnvNames(); len(names) != 2 || names[0] != "httpbinio" || names[1] != "httpbinorg" {
		t.Fatalf("environments = %v", names)
	}
	if got := c.Vars("httpbinorg").Expand("{{urlfromenv}}/anything"); got != "httpbin.org/anything" {
		t.Errorf("expanded = %q", got)
	}
	if got := c.Vars("httpbinio").Expand("{{urlfromenv}}/anything"); got != "httpbin.io/anything" {
		t.Errorf("expanded = %q", got)
	}
}
