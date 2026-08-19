package ui

import (
	"embed"
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"strings"
)

//go:embed templates/*.gohtml
var templateFS embed.FS

var templates = template.Must(
	template.New("ui").Funcs(template.FuncMap{
		"json":       toJSON,
		"methodTone": methodTone,
		"lower":      strings.ToLower,
		"rows":       newRowSpec,
	}).ParseFS(templateFS, "templates/*.gohtml"),
)

// rowSpec parameterises the shared key-value table template.
type rowSpec struct {
	Prefix string // form field prefix, e.g. "header" -> header-key/-value/-on
	Model  string // Alpine expression holding the array, e.g. "body.form"
	Label  string // placeholder shown in the key column
}

func newRowSpec(prefix, model, label string) rowSpec {
	return rowSpec{Prefix: prefix, Model: model, Label: label}
}

// toJSON serialises a view model for a data-* attribute. It returns a plain
// string on purpose: html/template escapes it for the attribute and the browser
// hands the original text back through dataset, which is safer than trying to
// reason about script-tag escaping contexts.
func toJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		log.Printf("ui: encoding view model: %v", err)
		return "{}"
	}
	return string(data)
}

// methodTone maps an HTTP verb to a colour token used by the stylesheet.
func methodTone(method string) string {
	switch strings.ToUpper(method) {
	case "GET":
		return "text-emerald-400"
	case "POST":
		return "text-amber-400"
	case "PUT", "PATCH":
		return "text-sky-400"
	case "DELETE":
		return "text-rose-400"
	default:
		return "text-zinc-400"
	}
}

func (s *Server) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.ExecuteTemplate(w, name, data); err != nil {
		// The response is already partly written at this point, so all we can
		// usefully do is leave a trace for the dev console.
		log.Printf("ui: rendering %s: %v", name, err)
	}
}

func (s *Server) renderError(w http.ResponseWriter, message string) {
	s.render(w, "error.gohtml", message)
}
