// Package ui serves the HTML fragments that HTMX swaps into the window. It is
// a normal http.Handler, mounted inside the Wails asset server, so the frontend
// stays plain HTML with no JS build step or client-side router.
package ui

import (
	"net/http"
	"strings"
	"time"

	"github.com/mrhant/amanati/internal/collection"
	"github.com/mrhant/amanati/internal/httpclient"
)

// Prefix is the path all UI endpoints live under.
const Prefix = "/api/"

// Server renders fragments for one window.
type Server struct {
	store  *collection.Store
	client *httpclient.Client
	mux    *http.ServeMux
}

// New builds the handler around a store.
func New(store *collection.Store) *Server {
	s := &Server{
		store:  store,
		client: httpclient.New(httpclient.Options{Timeout: 60 * time.Second}),
		mux:    http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/sidebar", s.handleSidebar)
	s.mux.HandleFunc("GET /api/request/{id}", s.handleRequest)
	s.mux.HandleFunc("GET /api/blank", s.handleBlank)
	s.mux.HandleFunc("POST /api/send", s.handleSend)
	s.mux.HandleFunc("POST /api/collections/{id}/close", s.handleClose)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	s.mux.ServeHTTP(w, r)
}

func (s *Server) handleSidebar(w http.ResponseWriter, r *http.Request) {
	s.render(w, "sidebar.gohtml", sidebarVM{Collections: s.store.List()})
}

func (s *Server) handleClose(w http.ResponseWriter, r *http.Request) {
	s.store.Close(r.PathValue("id"))
	s.render(w, "sidebar.gohtml", sidebarVM{Collections: s.store.List()})
}

func (s *Server) handleBlank(w http.ResponseWriter, r *http.Request) {
	s.render(w, "editor.gohtml", blankEditor())
}

func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	c, req := s.store.FindRequest(r.PathValue("id"))
	if req == nil {
		s.renderError(w, "That request is no longer open.")
		return
	}
	s.render(w, "editor.gohtml", editorFor(c, req))
}

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderError(w, "Could not read the request form: "+err.Error())
		return
	}

	vars := collection.Vars{}
	if c := s.store.Get(r.FormValue("collection-id")); c != nil {
		vars = c.Vars(r.FormValue("env"))
	}

	out := httpclient.Request{
		Method:  r.FormValue("method"),
		URL:     vars.Expand(strings.TrimSpace(r.FormValue("url"))),
		Headers: vars.ExpandParams(rows(r.Form, "header")),
		Query:   vars.ExpandParams(rows(r.Form, "param")),
		Body:    formBody(r, vars),
		Auth:    formAuth(r, vars),
	}

	resp, err := s.client.Send(r.Context(), out)
	if err != nil {
		s.render(w, "response.gohtml", responseVM{Error: err.Error()})
		return
	}
	s.render(w, "response.gohtml", newResponseVM(resp))
}

// rows reassembles the `<prefix>-key` / `-value` / `-on` inputs that Alpine
// renders for each key-value row back into params, keeping disabled ones so the
// caller decides what to drop.
func rows(form map[string][]string, prefix string) []collection.Param {
	keys := form[prefix+"-key"]
	values := form[prefix+"-value"]
	enabled := form[prefix+"-on"]

	out := make([]collection.Param, 0, len(keys))
	for i, key := range keys {
		if strings.TrimSpace(key) == "" {
			continue
		}
		p := collection.Param{Key: key}
		if i < len(values) {
			p.Value = values[i]
		}
		if i < len(enabled) {
			p.Disabled = enabled[i] != "1"
		}
		out = append(out, p)
	}
	return out
}

func formBody(r *http.Request, vars collection.Vars) collection.Body {
	switch r.FormValue("body-mode") {
	case collection.BodyRaw:
		return collection.Body{
			Mode:        collection.BodyRaw,
			Raw:         vars.Expand(r.FormValue("body-raw")),
			ContentType: r.FormValue("body-type"),
		}
	case collection.BodyForm:
		return collection.Body{Mode: collection.BodyForm, Form: vars.ExpandParams(rows(r.Form, "form"))}
	case collection.BodyMulti:
		return collection.Body{Mode: collection.BodyMulti, Form: vars.ExpandParams(rows(r.Form, "form"))}
	default:
		return collection.Body{Mode: collection.BodyNone}
	}
}

func formAuth(r *http.Request, vars collection.Vars) collection.Auth {
	switch r.FormValue("auth-type") {
	case collection.AuthBearer:
		return collection.Auth{Type: collection.AuthBearer, Token: vars.Expand(r.FormValue("auth-token"))}
	case collection.AuthBasic:
		return collection.Auth{
			Type:     collection.AuthBasic,
			Username: vars.Expand(r.FormValue("auth-username")),
			Password: vars.Expand(r.FormValue("auth-password")),
		}
	case collection.AuthAPIKey:
		return collection.Auth{
			Type:  collection.AuthAPIKey,
			Key:   vars.Expand(r.FormValue("auth-key")),
			Value: vars.Expand(r.FormValue("auth-value")),
			In:    r.FormValue("auth-in"),
		}
	default:
		return collection.Auth{Type: collection.AuthNone}
	}
}
