package ui

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mrhant/amanati/internal/collection"
)

const envCollectionJSON = `{
  "info": { "name": "Demo", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json" },
  "item": [{ "name": "Anything", "request": { "method": "GET", "url": "{{host}}/anything" } }]
}`

// newServerWithEnvs imports a collection exported alongside two environments.
func newServerWithEnvs(t *testing.T) (*Server, *collection.Collection) {
	t.Helper()

	dir := t.TempDir()
	files := map[string]string{
		"demo.postman_collection.json":     envCollectionJSON,
		"prod.postman_environment.json":    `{"name":"prod","values":[{"key":"host","value":"https://prod.example.com"}],"_postman_variable_scope":"environment"}`,
		"staging.postman_environment.json": `{"name":"staging","values":[{"key":"host","value":"https://staging.example.com"}],"_postman_variable_scope":"environment"}`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	store := collection.NewStore()
	c, err := store.Import(filepath.Join(dir, "demo.postman_collection.json"))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	return New(store), c
}

// postStatus posts without asserting 200, unlike post.
func postStatus(t *testing.T, s *Server, target string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func TestEditorRendersEnvironmentPicker(t *testing.T) {
	s, c := newServerWithEnvs(t)

	fragment := get(t, s, "/api/request/"+c.Root.Requests[0].ID)

	// Asserted as markup because the picker is server-rendered, not built by
	// Alpine from the state below.
	for _, want := range []string{
		`hx-post="/api/collections/` + c.ID + `/env"`,
		`<option value="prod" selected>prod</option>`,
		`<option value="staging">staging</option>`,
		`>No environment</option>`,
	} {
		if !strings.Contains(fragment, want) {
			t.Errorf("editor is missing %s:\n%s", want, fragment)
		}
	}

	state := editorState(t, fragment)
	if state["env"] != "prod" {
		t.Errorf("env = %v, want the collection's active environment", state["env"])
	}
	if envs, _ := state["envs"].([]any); len(envs) != 2 {
		t.Errorf("envs = %#v", state["envs"])
	}
}

func TestEditorWithoutEnvironmentsHasNoPicker(t *testing.T) {
	s, c := newServer(t)

	fragment := get(t, s, "/api/request/"+c.Root.Requests[0].ID)
	if strings.Contains(fragment, "/env\"") {
		t.Errorf("a collection with no environments should render no picker:\n%s", fragment)
	}
}

func TestSetEnvSticksAcrossRequests(t *testing.T) {
	s, c := newServerWithEnvs(t)

	rec := postStatus(t, s, "/api/collections/"+c.ID+"/env", url.Values{"env": {"staging"}})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST env = %d, want 204", rec.Code)
	}

	fragment := get(t, s, "/api/request/"+c.Root.Requests[0].ID)
	if !strings.Contains(fragment, `<option value="staging" selected>`) {
		t.Errorf("reopened editor did not keep the choice:\n%s", fragment)
	}
	if c.ActiveEnv() != "staging" {
		t.Errorf("active = %q", c.ActiveEnv())
	}
}

func TestSetEnvToNone(t *testing.T) {
	s, c := newServerWithEnvs(t)

	if rec := postStatus(t, s, "/api/collections/"+c.ID+"/env", url.Values{"env": {""}}); rec.Code != http.StatusNoContent {
		t.Fatalf("POST env = %d, want 204", rec.Code)
	}
	if c.ActiveEnv() != "" {
		t.Errorf("active = %q, want none", c.ActiveEnv())
	}
	if fragment := get(t, s, "/api/request/"+c.Root.Requests[0].ID); !strings.Contains(fragment, `<option value="" selected>`) {
		t.Errorf(`"No environment" is not selected:\n%s`, fragment)
	}
}

func TestSetEnvRejectsUnknown(t *testing.T) {
	s, c := newServerWithEnvs(t)

	if rec := postStatus(t, s, "/api/collections/"+c.ID+"/env", url.Values{"env": {"ghost"}}); rec.Code != http.StatusNotFound {
		t.Errorf("unknown environment = %d, want 404", rec.Code)
	}
	if rec := postStatus(t, s, "/api/collections/nope/env", url.Values{"env": {"prod"}}); rec.Code != http.StatusNotFound {
		t.Errorf("unknown collection = %d, want 404", rec.Code)
	}
	if c.ActiveEnv() != "prod" {
		t.Errorf("active = %q, want a rejected switch to change nothing", c.ActiveEnv())
	}
}

func TestSendUsesEnvironmentFromForm(t *testing.T) {
	s, c := newServerWithEnvs(t)

	var gotHost string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
	}))
	defer upstream.Close()
	c.PutEnvironment(collection.Environment{
		Name:      "local",
		Variables: []collection.Variable{{Key: "host", Value: upstream.URL}},
	})

	post(t, s, "/api/send", url.Values{
		"collection-id": {c.ID},
		"env":           {"local"},
		"method":        {"GET"},
		"url":           {"{{host}}/anything"},
		"body-mode":     {"none"},
		"auth-type":     {"none"},
	})

	if want := strings.TrimPrefix(upstream.URL, "http://"); gotHost != want {
		t.Errorf("host = %q, want the posted environment to resolve {{host}} to %q", gotHost, want)
	}
}

// A form with no picker at all, as the blank editor posts.
func TestSendFallsBackToActiveEnvironment(t *testing.T) {
	s, c := newServerWithEnvs(t)

	var gotHost string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
	}))
	defer upstream.Close()
	c.PutEnvironment(collection.Environment{
		Name:      "local",
		Variables: []collection.Variable{{Key: "host", Value: upstream.URL}},
	})
	if !c.SetEnv("local") {
		t.Fatal("SetEnv")
	}

	post(t, s, "/api/send", url.Values{
		"collection-id": {c.ID},
		"method":        {"GET"},
		"url":           {"{{host}}/anything"},
		"body-mode":     {"none"},
		"auth-type":     {"none"},
	})

	if want := strings.TrimPrefix(upstream.URL, "http://"); gotHost != want {
		t.Errorf("host = %q, want the active environment used, %q", gotHost, want)
	}
}

func TestSidebarShowsEnvironmentImport(t *testing.T) {
	s, c := newServerWithEnvs(t)

	body := get(t, s, "/api/sidebar")
	if !strings.Contains(body, "importEnvironment('"+c.ID+"')") {
		t.Errorf("sidebar has no environment import for the collection:\n%s", body)
	}
}
