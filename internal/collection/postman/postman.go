// Package postman imports Postman Collection v2.0/v2.1 files.
package postman

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mrhant/amanati/internal/collection"
)

func init() { collection.Register(Importer{}) }

// Importer reads a single exported .json collection file.
type Importer struct{}

func (Importer) ID() string                  { return "postman" }
func (Importer) Label() string               { return "Postman v2" }
func (Importer) Kind() collection.SourceKind { return collection.SourceFile }
func (Importer) Patterns() []string          { return []string{"*.json"} }

// Accepts does a cheap peek at the file header rather than parsing the whole
// document, so scanning a folder of unrelated JSON stays fast.
func (Importer) Accepts(path string) bool {
	if !strings.EqualFold(filepath.Ext(path), ".json") {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	head := make([]byte, 4096)
	n, _ := f.Read(head)
	return strings.Contains(string(head[:n]), "schema.getpostman.com/json/collection")
}

func (Importer) Import(path string) (*collection.Collection, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc document
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	c := &collection.Collection{
		ID:     collection.SourceID(path),
		Name:   doc.Info.Name,
		Format: "postman",
		Source: path,
		Root:   &collection.Folder{Name: doc.Info.Name},
	}
	for _, v := range doc.Variable {
		c.Variables = append(c.Variables, collection.Variable{Key: v.Key, Value: v.Value.String(), Disabled: bool(v.Disabled)})
	}
	convertItems(doc.Item, c.Root)
	return collection.Finalize(c), nil
}

func convertItems(items []item, into *collection.Folder) {
	for i, it := range items {
		if it.Request == nil {
			sub := &collection.Folder{Name: it.Name, Seq: i}
			convertItems(it.Item, sub)
			into.Folders = append(into.Folders, sub)
			continue
		}
		into.Requests = append(into.Requests, convertRequest(it, i))
	}
}

func convertRequest(it item, seq int) *collection.Request {
	src := it.Request
	r := &collection.Request{
		Name:   it.Name,
		Seq:    seq,
		Method: src.Method,
		URL:    src.URL.Raw(),
		Auth:   convertAuth(src.Auth),
		Body:   convertBody(src.Body),
	}
	for _, h := range src.Header {
		r.Headers = append(r.Headers, collection.Param{Key: h.Key, Value: h.Value.String(), Disabled: bool(h.Disabled)})
	}
	for _, q := range src.URL.Query {
		r.Query = append(r.Query, collection.Param{Key: q.Key, Value: q.Value.String(), Disabled: bool(q.Disabled)})
	}
	return r
}

func convertBody(b *body) collection.Body {
	if b == nil || b.Mode == "" || bool(b.Disabled) {
		return collection.Body{Mode: collection.BodyNone}
	}
	switch b.Mode {
	case "raw":
		return collection.Body{
			Mode:        collection.BodyRaw,
			Raw:         b.Raw,
			ContentType: contentTypeFor(b.Options.Raw.Language),
		}
	case "urlencoded":
		return collection.Body{Mode: collection.BodyForm, Form: toParams(b.URLEncoded)}
	case "formdata":
		return collection.Body{Mode: collection.BodyMulti, Form: toParams(b.FormData)}
	case "graphql":
		payload, err := json.Marshal(b.GraphQL)
		if err != nil {
			return collection.Body{Mode: collection.BodyNone}
		}
		return collection.Body{Mode: collection.BodyRaw, Raw: string(payload), ContentType: "application/json"}
	default:
		return collection.Body{Mode: collection.BodyNone}
	}
}

func contentTypeFor(language string) string {
	switch language {
	case "json":
		return "application/json"
	case "xml":
		return "application/xml"
	case "html":
		return "text/html"
	case "javascript":
		return "application/javascript"
	default:
		return "text/plain"
	}
}

func toParams(in []kv) []collection.Param {
	out := make([]collection.Param, 0, len(in))
	for _, p := range in {
		out = append(out, collection.Param{Key: p.Key, Value: p.Value.String(), Disabled: bool(p.Disabled)})
	}
	return out
}

func convertAuth(a *auth) collection.Auth {
	if a == nil {
		return collection.Auth{Type: collection.AuthNone}
	}
	switch a.Type {
	case "bearer":
		return collection.Auth{Type: collection.AuthBearer, Token: a.lookup("bearer", "token")}
	case "basic":
		return collection.Auth{
			Type:     collection.AuthBasic,
			Username: a.lookup("basic", "username"),
			Password: a.lookup("basic", "password"),
		}
	case "apikey":
		in := a.lookup("apikey", "in")
		if in != "query" {
			in = "header"
		}
		return collection.Auth{
			Type:  collection.AuthAPIKey,
			Key:   a.lookup("apikey", "key"),
			Value: a.lookup("apikey", "value"),
			In:    in,
		}
	case "noauth", "":
		return collection.Auth{Type: collection.AuthNone}
	default:
		// Schemes we do not implement yet (oauth2, awsv4, ...) fall back to
		// none; the request still opens and can be sent with manual headers.
		return collection.Auth{Type: collection.AuthNone}
	}
}
