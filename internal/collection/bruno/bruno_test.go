package bruno

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mrhant/amanati/internal/collection"
)

// writeCollection lays out a minimal Bruno collection on disk.
func writeCollection(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	files := map[string]string{
		"bruno.json":             `{"version":"1","name":"Demo","type":"collection"}`,
		"collection.bru":         "vars {\n  baseUrl: https://api.example.com\n}\n",
		"environments/local.bru": "vars {\n  baseUrl: http://localhost:8080\n}\n",
		"Get users.bru":          "meta {\n  name: List users\n  seq: 1\n}\n\nget {\n  url: {{baseUrl}}/users\n  body: none\n  auth: none\n}\n\nparams:query {\n  page: 2\n}\n",
		"Admin/folder.bru":       "meta {\n  name: Admin area\n  seq: 1\n}\n",
		"Admin/Create user.bru":  "meta {\n  name: Create user\n  seq: 1\n}\n\npost {\n  url: {{baseUrl}}/users\n  body: json\n  auth: bearer\n}\n\nauth:bearer {\n  token: secret\n}\n\nbody:json {\n  {\"name\": \"Ada\"}\n}\n",
		"Admin/Draft.bru":        "meta {\n  name: Draft\n  seq: 2\n}\n\nget {\n  url: {{baseUrl}}/draft\n  body: none\n  auth: none\n}\n\nbody:json {\n  {\"ignored\": true}\n}\n",
	}
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestImport(t *testing.T) {
	root := writeCollection(t)

	c, err := Importer{}.Import(root)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if c.Name != "Demo" || c.Format != "bruno" {
		t.Errorf("collection = %q / %q", c.Name, c.Format)
	}
	if got := c.Vars("").Expand("{{baseUrl}}/x"); got != "https://api.example.com/x" {
		t.Errorf("collection vars: %q", got)
	}
	if got := c.Vars("local").Expand("{{baseUrl}}/x"); got != "http://localhost:8080/x" {
		t.Errorf("environment override: %q", got)
	}

	if len(c.Root.Requests) != 1 {
		t.Fatalf("root has %d requests, want 1", len(c.Root.Requests))
	}
	list := c.Root.Requests[0]
	if list.Name != "List users" || list.Verb() != "GET" {
		t.Errorf("root request = %+v", list)
	}
	if len(list.Query) != 1 || list.Query[0].Key != "page" || list.Query[0].Value != "2" {
		t.Errorf("query params = %+v", list.Query)
	}

	if len(c.Root.Folders) != 1 {
		t.Fatalf("root has %d folders, want 1", len(c.Root.Folders))
	}
	admin := c.Root.Folders[0]
	if admin.Name != "Admin area" {
		t.Errorf("folder name = %q, want the name from folder.bru", admin.Name)
	}
	if len(admin.Requests) != 2 {
		t.Fatalf("Admin has %d requests, want 2", len(admin.Requests))
	}

	create := admin.Requests[0]
	if create.Verb() != "POST" || create.Body.Mode != collection.BodyRaw {
		t.Errorf("create = %s / %s", create.Verb(), create.Body.Mode)
	}
	if create.Auth.Type != collection.AuthBearer || create.Auth.Token != "secret" {
		t.Errorf("auth = %+v", create.Auth)
	}

	// Draft declares `body: none`, so its leftover body:json block is ignored.
	if draft := admin.Requests[1]; draft.Body.Mode != collection.BodyNone {
		t.Errorf("draft body mode = %q, want none", draft.Body.Mode)
	}
}

func TestImportAssignsUniqueRequestIDs(t *testing.T) {
	c, err := Importer{}.Import(writeCollection(t))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	seen := map[string]bool{}
	var walk func(*collection.Folder)
	walk = func(f *collection.Folder) {
		for _, r := range f.Requests {
			if r.ID == "" {
				t.Error("request has an empty ID")
			}
			if seen[r.ID] {
				t.Errorf("duplicate request ID %q", r.ID)
			}
			seen[r.ID] = true
			if c.Lookup(r.ID) != r {
				t.Errorf("Lookup(%q) did not return the request", r.ID)
			}
		}
		for _, sub := range f.Folders {
			walk(sub)
		}
	}
	walk(c.Root)

	if len(seen) != 3 {
		t.Errorf("indexed %d requests, want 3", len(seen))
	}
}

func TestAcceptsRejectsPlainDirectory(t *testing.T) {
	if (Importer{}).Accepts(t.TempDir()) {
		t.Error("a directory without bruno.json should not be accepted")
	}
}
