package postman

import (
	"os"
	"path/filepath"
	"testing"
)

const envDocJSON = `{
  "id": "5249a740-e496-49d2-824b-e152a89c15d9",
  "name": "staging",
  "values": [
    { "key": "host", "value": "staging.example.com", "enabled": true },
    { "key": "port", "value": 8443 },
    { "key": "legacy", "value": "off", "enabled": false },
    { "key": "also-off", "value": "x", "disabled": true },
    { "key": "", "value": "nameless" }
  ],
  "_postman_variable_scope": "environment"
}`

// writeEnv writes one file into a temp directory, under the given name.
func writeEnv(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestImportEnv(t *testing.T) {
	env, err := Importer{}.ImportEnv(writeEnv(t, "staging.postman_environment.json", envDocJSON))
	if err != nil {
		t.Fatalf("ImportEnv: %v", err)
	}
	if env.Name != "staging" {
		t.Errorf("name = %q", env.Name)
	}
	if len(env.Variables) != 4 {
		t.Fatalf("variables = %+v, want the four named ones", env.Variables)
	}
	if v := env.Variables[0]; v.Key != "host" || v.Value != "staging.example.com" || v.Disabled {
		t.Errorf("enabled value = %+v", v)
	}
	if v := env.Variables[1]; v.Value != "8443" || v.Disabled {
		t.Errorf("numeric value with no flag = %+v, want enabled and stringified", v)
	}
	if !env.Variables[2].Disabled {
		t.Error(`"enabled": false should disable the variable`)
	}
	if !env.Variables[3].Disabled {
		t.Error(`"disabled": true should disable the variable too`)
	}
	if env.Source == "" {
		t.Error("Source should record the file the environment came from")
	}
}

func TestImportEnvNameFallsBackToFile(t *testing.T) {
	path := writeEnv(t, "prod.postman_environment.json", `{"values":[{"key":"a","value":"b"}]}`)
	env, err := Importer{}.ImportEnv(path)
	if err != nil {
		t.Fatalf("ImportEnv: %v", err)
	}
	if env.Name != "prod" {
		t.Errorf("name = %q, want the file name without Postman's tag", env.Name)
	}
}

func TestImportEnvRejectsGlobals(t *testing.T) {
	path := writeEnv(t, "globals.json", `{"name":"Globals","values":[],"_postman_variable_scope":"globals"}`)
	if _, err := (Importer{}).ImportEnv(path); err == nil {
		t.Fatal("expected an error for a globals export")
	}
}

func TestAcceptsEnv(t *testing.T) {
	cases := []struct {
		name    string
		file    string
		content string
		want    bool
	}{
		{"exported environment", "staging.postman_environment.json", envDocJSON, true},
		{"no scope marker", "old.json", `{"name":"old","values":[]}`, true},
		{"globals export", "globals.json", `{"values":[],"_postman_variable_scope":"globals"}`, false},
		{"a collection", "demo.postman_collection.json", doc, false},
		{"unrelated JSON", "tsconfig.json", `{"compilerOptions":{}}`, false},
		{"broken JSON", "broken.json", `{"values":`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := (Importer{}).AcceptsEnv(writeEnv(t, tc.file, tc.content)); got != tc.want {
				t.Errorf("AcceptsEnv = %v, want %v", got, tc.want)
			}
		})
	}
}

// Both are .json and both may sit in the same folder, so the two checks have
// to disagree on every file.
func TestAcceptsAndAcceptsEnvDoNotOverlap(t *testing.T) {
	env := writeEnv(t, "staging.postman_environment.json", envDocJSON)
	if (Importer{}).Accepts(env) {
		t.Error("an environment should not be accepted as a collection")
	}
	if (Importer{}).AcceptsEnv(writeDoc(t, doc)) {
		t.Error("a collection should not be accepted as an environment")
	}
}
