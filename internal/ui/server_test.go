package ui

import (
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mrhant/amanati/internal/collection"
	_ "github.com/mrhant/amanati/internal/collection/postman"
)

const collectionJSON = `{
  "info": { "name": "Demo", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json" },
  "variable": [{ "key": "baseUrl", "value": "https://api.example.com" }],
  "item": [{
    "name": "List users",
    "request": {
      "method": "GET",
      "header": [{ "key": "Accept", "value": "application/json" }],
      "url": { "raw": "{{baseUrl}}/users?page=1", "query": [{ "key": "page", "value": "1" }] }
    }
  }]
}`

// newServer returns a fragment server with one collection already imported.
func newServer(t *testing.T) (*Server, *collection.Collection) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "demo.postman_collection.json")
	if err := os.WriteFile(path, []byte(collectionJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	store := collection.NewStore()
	c, err := store.Import(path)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	return New(store), c
}

func get(t *testing.T, s *Server, target string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d", target, rec.Code)
	}
	return rec.Body.String()
}

func post(t *testing.T, s *Server, target string, form url.Values) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST %s = %d", target, rec.Code)
	}
	return rec.Body.String()
}

func TestSidebarListsRequests(t *testing.T) {
	s, _ := newServer(t)

	body := get(t, s, "/api/sidebar")
	for _, want := range []string{"Demo", "List users", "GET", "postman"} {
		if !strings.Contains(body, want) {
			t.Errorf("sidebar is missing %q:\n%s", want, body)
		}
	}
}

func TestSidebarEmpty(t *testing.T) {
	s := New(collection.NewStore())
	if body := get(t, s, "/api/sidebar"); !strings.Contains(body, "No collections open") {
		t.Errorf("empty sidebar = %s", body)
	}
}

var stateAttr = regexp.MustCompile(`data-state="([^"]*)"`)

// editorState extracts and decodes the JSON that the editor fragment hands to
// Alpine, which is the contract between Go and the frontend.
func editorState(t *testing.T, fragment string) map[string]any {
	t.Helper()

	m := stateAttr.FindStringSubmatch(fragment)
	if m == nil {
		t.Fatalf("no data-state attribute in fragment:\n%s", fragment)
	}
	var state map[string]any
	if err := json.Unmarshal([]byte(html.UnescapeString(m[1])), &state); err != nil {
		t.Fatalf("data-state is not valid JSON after unescaping: %v", err)
	}
	return state
}

func TestBlankEditorState(t *testing.T) {
	s, _ := newServer(t)
	state := editorState(t, get(t, s, "/api/blank"))

	if state["method"] != "GET" {
		t.Errorf("method = %v", state["method"])
	}
	// Alpine calls .filter and .push on these, so they must never be null.
	for _, key := range []string{"headers", "params", "envs"} {
		if _, ok := state[key].([]any); !ok {
			t.Errorf("%s = %#v, want an array", key, state[key])
		}
	}
	if form, ok := state["body"].(map[string]any)["form"].([]any); !ok {
		t.Errorf("body.form = %#v, want an array", form)
	}
}

func TestRequestEditorState(t *testing.T) {
	s, c := newServer(t)
	request := c.Root.Requests[0]

	state := editorState(t, get(t, s, "/api/request/"+request.ID))

	if state["name"] != "List users" {
		t.Errorf("name = %v", state["name"])
	}
	if state["url"] != "{{baseUrl}}/users" {
		t.Errorf("url = %v, want the query string split out", state["url"])
	}
	if state["collectionId"] != c.ID {
		t.Errorf("collectionId = %v, want %v", state["collectionId"], c.ID)
	}

	params, _ := state["params"].([]any)
	if len(params) != 1 {
		t.Fatalf("params = %#v", state["params"])
	}
	if first := params[0].(map[string]any); first["key"] != "page" {
		t.Errorf("param = %#v", first)
	}
}

func TestRequestEditorUnknownID(t *testing.T) {
	s, _ := newServer(t)
	if body := get(t, s, "/api/request/nope"); !strings.Contains(body, "no longer open") {
		t.Errorf("unknown request = %s", body)
	}
}

func TestSendRendersResponse(t *testing.T) {
	s, c := newServer(t)

	var gotPath, gotAuth, gotAccept string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"users":[{"id":1}]}`))
	}))
	defer upstream.Close()

	body := post(t, s, "/api/send", url.Values{
		"collection-id": {c.ID},
		"method":        {"GET"},
		"url":           {upstream.URL + "/users"},
		"header-key":    {"Accept", "X-Skip"},
		"header-value":  {"application/json", "no"},
		"header-on":     {"1", "0"},
		"param-key":     {"page", "hidden"},
		"param-value":   {"2", "no"},
		"param-on":      {"1", "0"},
		"auth-type":     {"bearer"},
		"auth-token":    {"tok"},
		"body-mode":     {"none"},
	})

	if gotPath != "/users?page=2" {
		t.Errorf("upstream path = %q, want disabled params dropped", gotPath)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q", gotAccept)
	}

	if !strings.Contains(body, "200 OK") {
		t.Errorf("fragment is missing the status:\n%s", body)
	}
	// JSON responses are indented for display.
	if !strings.Contains(body, "&#34;users&#34;: [") {
		t.Errorf("fragment is missing the pretty-printed body:\n%s", body)
	}
}

func TestSendExpandsVariables(t *testing.T) {
	s, c := newServer(t)

	var gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
	}))
	defer upstream.Close()

	// Point the collection variable at the test server, then use it by name.
	c.Variables = []collection.Variable{{Key: "baseUrl", Value: upstream.URL}}

	post(t, s, "/api/send", url.Values{
		"collection-id": {c.ID},
		"method":        {"GET"},
		"url":           {"{{baseUrl}}/users"},
		"body-mode":     {"none"},
		"auth-type":     {"none"},
	})

	if gotPath != "/users" {
		t.Errorf("path = %q, want {{baseUrl}} expanded", gotPath)
	}
}

func TestSendPostsFormBody(t *testing.T) {
	s, _ := newServer(t)

	var gotType, gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotType = r.Header.Get("Content-Type")
		raw := make([]byte, r.ContentLength)
		r.Body.Read(raw)
		gotBody = string(raw)
		w.WriteHeader(http.StatusCreated)
	}))
	defer upstream.Close()

	body := post(t, s, "/api/send", url.Values{
		"method":     {"POST"},
		"url":        {upstream.URL},
		"body-mode":  {"form-urlencoded"},
		"form-key":   {"name"},
		"form-value": {"Ada"},
		"form-on":    {"1"},
		"auth-type":  {"none"},
	})

	if gotType != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type = %q", gotType)
	}
	if gotBody != "name=Ada" {
		t.Errorf("body = %q", gotBody)
	}
	if !strings.Contains(body, "201 Created") {
		t.Errorf("fragment is missing the status:\n%s", body)
	}
}

func TestSendReportsTransportFailure(t *testing.T) {
	s, _ := newServer(t)

	body := post(t, s, "/api/send", url.Values{
		"method":    {"GET"},
		"url":       {""},
		"body-mode": {"none"},
		"auth-type": {"none"},
	})

	if !strings.Contains(body, "Failed") || !strings.Contains(body, "URL is empty") {
		t.Errorf("error fragment = %s", body)
	}
}

func TestCloseCollection(t *testing.T) {
	s, c := newServer(t)

	body := post(t, s, "/api/collections/"+c.ID+"/close", url.Values{})
	if !strings.Contains(body, "No collections open") {
		t.Errorf("sidebar after close = %s", body)
	}
}
