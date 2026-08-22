package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mrhant/amanati/internal/collection"
	"github.com/mrhant/amanati/internal/httpclient"
)

// sidebarVM backs the collection tree.
type sidebarVM struct {
	Collections []*collection.Collection
}

// rowVM is one key-value row in the editor. Alpine owns these client-side, so
// they are serialised to JSON rather than rendered as markup.
type rowVM struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Enabled bool   `json:"enabled"`
}

type bodyVM struct {
	Mode string  `json:"mode"`
	Type string  `json:"type"`
	Raw  string  `json:"raw"`
	Form []rowVM `json:"form"`
}

type authVM struct {
	Type     string `json:"type"`
	Token    string `json:"token"`
	Username string `json:"username"`
	Password string `json:"password"`
	Key      string `json:"key"`
	Value    string `json:"value"`
	In       string `json:"in"`
}

// editorVM is the whole request editor state, handed to Alpine as JSON.
type editorVM struct {
	CollectionID   string   `json:"collectionId"`
	CollectionName string   `json:"collectionName"`
	RequestID      string   `json:"requestId"`
	Name           string   `json:"name"`
	Method         string   `json:"method"`
	URL            string   `json:"url"`
	Headers        []rowVM  `json:"headers"`
	Params         []rowVM  `json:"params"`
	Body           bodyVM   `json:"body"`
	Auth           authVM   `json:"auth"`
	Envs           []string `json:"envs"` // environments of the owning collection
	Env            string   `json:"env"`  // the one requests are sent with, "" for none
}

// blankEditor is the starting state for a new request. Every slice is non-nil
// so the JSON handed to Alpine never contains null where it expects an array.
func blankEditor() editorVM {
	return editorVM{
		Name:    "Untitled request",
		Method:  http.MethodGet,
		Headers: []rowVM{},
		Params:  []rowVM{},
		Body:    bodyVM{Mode: collection.BodyNone, Type: "application/json", Form: []rowVM{}},
		Auth:    authVM{Type: collection.AuthNone, In: "header"},
		Envs:    []string{},
	}
}

func editorFor(c *collection.Collection, r *collection.Request) editorVM {
	vm := blankEditor()
	vm.RequestID = r.ID
	vm.Name = r.Name
	vm.Method = r.Verb()
	vm.URL = stripQuery(r.URL)
	vm.Headers = toRows(r.Headers)
	vm.Params = mergeQuery(r.URL, r.Query)
	vm.Body = bodyVM{
		Mode: orDefault(r.Body.Mode, collection.BodyNone),
		Type: orDefault(r.Body.ContentType, "application/json"),
		Raw:  r.Body.Raw,
		Form: toRows(r.Body.Form),
	}
	vm.Auth = authVM{
		Type:     orDefault(r.Auth.Type, collection.AuthNone),
		Token:    r.Auth.Token,
		Username: r.Auth.Username,
		Password: r.Auth.Password,
		Key:      r.Auth.Key,
		Value:    r.Auth.Value,
		In:       orDefault(r.Auth.In, "header"),
	}
	if c != nil {
		vm.CollectionID = c.ID
		vm.CollectionName = c.Name
		vm.Envs = c.EnvNames()
		vm.Env = c.ActiveEnv()
	}
	return vm
}

// mergeQuery lifts params already encoded in the URL string into editable rows,
// so the query table shows everything in one place.
func mergeQuery(rawURL string, declared []collection.Param) []rowVM {
	rows := toRows(declared)
	seen := map[string]bool{}
	for _, r := range rows {
		seen[r.Key] = true
	}

	_, query, found := strings.Cut(rawURL, "?")
	if !found {
		return rows
	}
	for _, pair := range strings.Split(query, "&") {
		if pair == "" {
			continue
		}
		key, value, _ := strings.Cut(pair, "=")
		if key == "" || seen[key] {
			continue
		}
		rows = append(rows, rowVM{Key: key, Value: value, Enabled: true})
	}
	return rows
}

func stripQuery(rawURL string) string {
	base, _, _ := strings.Cut(rawURL, "?")
	return base
}

func toRows(in []collection.Param) []rowVM {
	out := make([]rowVM, 0, len(in))
	for _, p := range in {
		out = append(out, rowVM{Key: p.Key, Value: p.Value, Enabled: !p.Disabled})
	}
	return out
}

func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// headerVM is one response header, flattened for display.
type headerVM struct {
	Name  string
	Value string
}

// responseVM backs the response pane.
type responseVM struct {
	Error string

	Status      string
	StatusTone  string
	Proto       string
	Duration    string
	Size        string
	ContentType string
	Body        string
	Truncated   bool
	Printable   bool
	Headers     []headerVM
	SentURL     string
	SentMethod  string
}

func newResponseVM(r *httpclient.Response) responseVM {
	contentType := r.Headers.Get("Content-Type")
	body, printable := renderBody(r.Body, contentType)

	return responseVM{
		Status:      r.Status,
		StatusTone:  statusTone(r.StatusCode),
		Proto:       r.Proto,
		Duration:    humanDuration(r.Duration),
		Size:        humanBytes(r.Size),
		ContentType: contentType,
		Body:        body,
		Truncated:   r.Truncated,
		Printable:   printable,
		Headers:     flattenHeaders(r.Headers),
		SentURL:     r.SentURL,
		SentMethod:  r.SentMethod,
	}
}

// renderBody pretty-prints JSON and refuses to dump binary into the DOM.
func renderBody(raw []byte, contentType string) (string, bool) {
	if len(raw) == 0 {
		return "", true
	}
	if !utf8.Valid(raw) || bytes.IndexByte(raw, 0) >= 0 {
		return "", false
	}
	if strings.Contains(strings.ToLower(contentType), "json") {
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, raw, "", "  "); err == nil {
			return pretty.String(), true
		}
	}
	return string(raw), true
}

func flattenHeaders(h http.Header) []headerVM {
	out := make([]headerVM, 0, len(h))
	for name, values := range h {
		for _, v := range values {
			out = append(out, headerVM{Name: name, Value: v})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func statusTone(code int) string {
	switch {
	case code >= 200 && code < 300:
		return "ok"
	case code >= 300 && code < 400:
		return "redirect"
	case code >= 400 && code < 500:
		return "client"
	case code >= 500:
		return "server"
	default:
		return "other"
	}
}

func humanDuration(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return fmt.Sprintf("%d µs", d.Microseconds())
	case d < time.Second:
		return fmt.Sprintf("%d ms", d.Milliseconds())
	default:
		return fmt.Sprintf("%.2f s", d.Seconds())
	}
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for size := n / unit; size >= unit; size /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}
