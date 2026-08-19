package postman

import (
	"testing"

	"github.com/mrhant/amanati/internal/collection"
)

// info is the header every fixture below needs for Accepts and Import to agree
// that the document is a collection.
const info = `"info":{"name":"X","schema":"https://schema.getpostman.com/json/collection/v2.1.0/collection.json"}`

func importOne(t *testing.T, doc string) *collection.Request {
	t.Helper()
	c, err := Importer{}.Import(writeDoc(t, doc))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(c.Root.Requests) != 1 {
		t.Fatalf("want 1 request, got %d", len(c.Root.Requests))
	}
	return c.Root.Requests[0]
}

// The v2.1 schema lets a field be one of several types, and exporters other
// than Postman are looser still. None of these may fail the import.
func TestImportAcceptsSchemaVariants(t *testing.T) {
	t.Run("request as a bare URL string", func(t *testing.T) {
		r := importOne(t, `{`+info+`,"item":[{"name":"a","request":"https://x.io/a"}]}`)
		if r.Verb() != "GET" || r.URL != "https://x.io/a" {
			t.Errorf("got %s %q", r.Verb(), r.URL)
		}
	})

	t.Run("host as a string", func(t *testing.T) {
		r := importOne(t, `{`+info+`,"item":[{"name":"a","request":{"method":"GET","url":{"host":"x.io","path":["a"]}}}]}`)
		if r.URL != "x.io/a" {
			t.Errorf("url = %q", r.URL)
		}
	})

	t.Run("path as a string", func(t *testing.T) {
		r := importOne(t, `{`+info+`,"item":[{"name":"a","request":{"method":"GET","url":{"host":["x","io"],"path":"a/b"}}}]}`)
		if r.URL != "x.io/a/b" {
			t.Errorf("url = %q", r.URL)
		}
	})

	t.Run("path variables as objects", func(t *testing.T) {
		r := importOne(t, `{`+info+`,"item":[{"name":"a","request":{"method":"GET","url":{"host":["x","io"],"path":["u",{"type":"string","value":":id"}]}}}]}`)
		if r.URL != "x.io/u/:id" {
			t.Errorf("url = %q", r.URL)
		}
	})

	t.Run("port as a number", func(t *testing.T) {
		r := importOne(t, `{`+info+`,"item":[{"name":"a","request":{"method":"GET","url":{"host":["x","io"],"port":8080,"path":["a"]}}}]}`)
		if r.URL != "x.io:8080/a" {
			t.Errorf("url = %q", r.URL)
		}
	})

	t.Run("headers as a raw block", func(t *testing.T) {
		r := importOne(t, `{`+info+`,"item":[{"name":"a","request":{"method":"GET","header":"Accept: application/json\n//X-Off: 1\n","url":"https://x.io/a"}}]}`)
		if len(r.Headers) != 2 {
			t.Fatalf("headers = %+v", r.Headers)
		}
		if r.Headers[0].Key != "Accept" || r.Headers[0].Value != "application/json" {
			t.Errorf("first header = %+v", r.Headers[0])
		}
		if !r.Headers[1].Disabled {
			t.Error("a // commented header should come back disabled")
		}
	})

	t.Run("auth attributes as an object", func(t *testing.T) {
		r := importOne(t, `{`+info+`,"item":[{"name":"a","request":{"method":"GET","auth":{"type":"bearer","bearer":{"token":"secret"}},"url":"https://x.io/a"}}]}`)
		if r.Auth.Type != collection.AuthBearer || r.Auth.Token != "secret" {
			t.Errorf("auth = %+v", r.Auth)
		}
	})

	t.Run("disabled as a string", func(t *testing.T) {
		r := importOne(t, `{`+info+`,"item":[{"name":"a","request":{"method":"GET","header":[{"key":"k","value":"v","disabled":"true"}],"url":"https://x.io/a"}}]}`)
		if len(r.Headers) != 1 || !r.Headers[0].Disabled {
			t.Errorf("headers = %+v", r.Headers)
		}
	})

	t.Run("null url", func(t *testing.T) {
		r := importOne(t, `{`+info+`,"item":[{"name":"a","request":{"method":"GET","url":null}}]}`)
		if r.URL != "" {
			t.Errorf("url = %q", r.URL)
		}
	})

	t.Run("body options that are not an object", func(t *testing.T) {
		r := importOne(t, `{`+info+`,"item":[{"name":"a","request":{"method":"POST","body":{"mode":"raw","raw":"hi","options":"json"},"url":"https://x.io/a"}}]}`)
		if r.Body.Mode != collection.BodyRaw || r.Body.Raw != "hi" {
			t.Errorf("body = %+v", r.Body)
		}
	})
}
