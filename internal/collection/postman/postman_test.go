package postman

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mrhant/amanati/internal/collection"
)

const doc = `{
  "info": {
    "name": "Demo",
    "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"
  },
  "variable": [{ "key": "baseUrl", "value": "https://api.example.com" }],
  "item": [
    {
      "name": "List users",
      "request": {
        "method": "GET",
        "header": [
          { "key": "Accept", "value": "application/json" },
          { "key": "X-Retries", "value": 3 },
          { "key": "X-Off", "value": "1", "disabled": true }
        ],
        "url": {
          "raw": "{{baseUrl}}/users?page=1",
          "host": ["{{baseUrl}}"],
          "path": ["users"],
          "query": [
            { "key": "page", "value": 1 },
            { "key": "debug", "value": "yes", "disabled": true }
          ]
        }
      }
    },
    {
      "name": "Admin",
      "item": [
        {
          "name": "Create user",
          "request": {
            "method": "POST",
            "url": "{{baseUrl}}/users",
            "auth": {
              "type": "bearer",
              "bearer": [{ "key": "token", "value": "secret" }]
            },
            "body": {
              "mode": "raw",
              "raw": "{\"name\":\"Ada\"}",
              "options": { "raw": { "language": "json" } }
            }
          }
        },
        {
          "name": "Login",
          "request": {
            "method": "POST",
            "url": { "protocol": "https", "host": ["api", "example", "com"], "path": ["login"] },
            "body": {
              "mode": "urlencoded",
              "urlencoded": [{ "key": "user", "value": "ada" }]
            }
          }
        }
      ]
    }
  ]
}`

func writeDoc(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "demo.postman_collection.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestImport(t *testing.T) {
	c, err := Importer{}.Import(writeDoc(t, doc))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if c.Name != "Demo" || c.Format != "postman" {
		t.Errorf("collection = %q / %q", c.Name, c.Format)
	}
	if got := c.Vars("").Expand("{{baseUrl}}/x"); got != "https://api.example.com/x" {
		t.Errorf("variables: %q", got)
	}

	if len(c.Root.Requests) != 1 || len(c.Root.Folders) != 1 {
		t.Fatalf("root = %d requests / %d folders", len(c.Root.Requests), len(c.Root.Folders))
	}

	list := c.Root.Requests[0]
	if list.URL != "{{baseUrl}}/users?page=1" {
		t.Errorf("url = %q", list.URL)
	}
	if len(list.Headers) != 3 {
		t.Fatalf("headers = %+v", list.Headers)
	}
	if list.Headers[1].Value != "3" {
		t.Errorf("numeric header value = %q, want \"3\"", list.Headers[1].Value)
	}
	if !list.Headers[2].Disabled {
		t.Error("X-Off should be disabled")
	}
	if len(list.Query) != 2 || list.Query[0].Value != "1" || !list.Query[1].Disabled {
		t.Errorf("query = %+v", list.Query)
	}

	admin := c.Root.Folders[0]
	if admin.Name != "Admin" || len(admin.Requests) != 2 {
		t.Fatalf("admin = %q with %d requests", admin.Name, len(admin.Requests))
	}

	create := admin.Requests[0]
	if create.URL != "{{baseUrl}}/users" {
		t.Errorf("string-form url = %q", create.URL)
	}
	if create.Body.Mode != collection.BodyRaw || create.Body.ContentType != "application/json" {
		t.Errorf("body = %+v", create.Body)
	}
	if create.Auth.Type != collection.AuthBearer || create.Auth.Token != "secret" {
		t.Errorf("auth = %+v", create.Auth)
	}

	login := admin.Requests[1]
	if login.URL != "https://api.example.com/login" {
		t.Errorf("rebuilt url = %q, want it assembled from host/path", login.URL)
	}
	if login.Body.Mode != collection.BodyForm || len(login.Body.Form) != 1 {
		t.Errorf("urlencoded body = %+v", login.Body)
	}
}

func TestAccepts(t *testing.T) {
	if !(Importer{}).Accepts(writeDoc(t, doc)) {
		t.Error("a v2.1 collection should be accepted")
	}
	if (Importer{}).Accepts(writeDoc(t, `{"openapi":"3.0.0"}`)) {
		t.Error("an OpenAPI document should not be accepted")
	}
}

func TestImportRejectsBrokenJSON(t *testing.T) {
	if _, err := (Importer{}).Import(writeDoc(t, `{"info":`)); err == nil {
		t.Fatal("expected an error for truncated JSON")
	}
}
