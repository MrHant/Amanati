// Package bruno imports Bruno collections. Unlike Postman, a Bruno collection
// is a directory tree: bruno.json at the root, one .bru file per request,
// folders mirroring the tree, and environments/ holding *.bru variable sets.
package bruno

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mrhant/amanati/internal/collection"
)

func init() { collection.Register(Importer{}) }

// Importer reads a Bruno collection directory.
type Importer struct{}

func (Importer) ID() string                  { return "bruno" }
func (Importer) Label() string               { return "Bruno" }
func (Importer) Kind() collection.SourceKind { return collection.SourceDirectory }
func (Importer) Patterns() []string          { return nil }

// Accepts reports whether path is a directory holding a bruno.json manifest.
// A .bru file or bruno.json itself is accepted too, resolving to its directory.
func (Importer) Accepts(path string) bool {
	_, err := os.Stat(filepath.Join(rootOf(path), "bruno.json"))
	return err == nil
}

func rootOf(path string) string {
	info, err := os.Stat(path)
	if err == nil && info.IsDir() {
		return path
	}
	return filepath.Dir(path)
}

func (Importer) Import(path string) (*collection.Collection, error) {
	root := rootOf(path)

	manifest, err := readManifest(filepath.Join(root, "bruno.json"))
	if err != nil {
		return nil, err
	}

	c := &collection.Collection{
		ID:     collection.SourceID(root),
		Name:   manifest.Name,
		Format: "bruno",
		Source: root,
	}
	if c.Name == "" {
		c.Name = filepath.Base(root)
	}

	// collection.bru carries collection-wide vars, headers and auth.
	if blocks, err := readBru(filepath.Join(root, "collection.bru")); err == nil {
		for _, b := range blocks {
			if b.name == "vars" {
				for _, e := range parseDict(b.body) {
					c.Variables = append(c.Variables, collection.Variable{Key: e.key, Value: e.value, Disabled: e.disabled})
				}
			}
		}
	}

	c.Environments = readEnvironments(filepath.Join(root, "environments"))

	folder, err := readFolder(root, c.Name)
	if err != nil {
		return nil, err
	}
	c.Root = folder
	return collection.Finalize(c), nil
}

type manifest struct {
	Version string `json:"version"`
	Name    string `json:"name"`
	Type    string `json:"type"`
}

func readManifest(path string) (manifest, error) {
	var m manifest
	raw, err := os.ReadFile(path)
	if err != nil {
		return m, fmt.Errorf("not a Bruno collection: %w", err)
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return m, fmt.Errorf("invalid bruno.json: %w", err)
	}
	return m, nil
}

func readBru(path string) ([]block, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseBlocks(string(raw))
}

// readFolder walks one directory level, recursing into subfolders.
func readFolder(dir, name string) (*collection.Folder, error) {
	f := &collection.Folder{Name: name}

	items, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		itemName := item.Name()
		if strings.HasPrefix(itemName, ".") || itemName == "node_modules" {
			continue
		}
		full := filepath.Join(dir, itemName)

		if item.IsDir() {
			if itemName == "environments" {
				continue
			}
			sub, err := readFolder(full, itemName)
			if err != nil {
				continue // an unreadable subfolder should not fail the import
			}
			f.Folders = append(f.Folders, sub)
			continue
		}
		if !strings.EqualFold(filepath.Ext(itemName), ".bru") {
			continue
		}
		switch itemName {
		case "collection.bru":
			continue
		case "folder.bru":
			if blocks, err := readBru(full); err == nil {
				applyFolderMeta(f, blocks)
			}
			continue
		}
		if r := readRequest(full); r != nil {
			f.Requests = append(f.Requests, r)
		}
	}
	return f, nil
}

func applyFolderMeta(f *collection.Folder, blocks []block) {
	for _, b := range blocks {
		if b.name != "meta" {
			continue
		}
		meta := parseDict(b.body)
		if name := lookup(meta, "name"); name != "" {
			f.Name = name
		}
		f.Seq = atoi(lookup(meta, "seq"))
	}
}

var methodBlocks = map[string]string{
	"get": "GET", "post": "POST", "put": "PUT", "delete": "DELETE",
	"patch": "PATCH", "options": "OPTIONS", "head": "HEAD", "trace": "TRACE",
}

func readRequest(path string) *collection.Request {
	blocks, err := readBru(path)
	if err != nil {
		return nil
	}

	r := &collection.Request{
		Name: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
		Body: collection.Body{Mode: collection.BodyNone},
		Auth: collection.Auth{Type: collection.AuthNone},
	}

	// A .bru file keeps every body and auth variant the user has ever typed;
	// the method block names the one that is actually active. Collect them all
	// and pick afterwards, so block order cannot change the result.
	bodies := map[string]collection.Body{}
	auths := map[string]collection.Auth{}
	bodyMode, authMode := "none", "none"

	for _, b := range blocks {
		switch b.name {
		case "meta":
			meta := parseDict(b.body)
			if name := lookup(meta, "name"); name != "" {
				r.Name = name
			}
			r.Seq = atoi(lookup(meta, "seq"))

		case "headers":
			for _, e := range parseDict(b.body) {
				r.Headers = append(r.Headers, collection.Param{Key: e.key, Value: e.value, Disabled: e.disabled})
			}

		case "params", "query":
			// params blocks are tagged `params:query` or `params:path`; only
			// query params belong in the URL.
			if b.name == "params" && b.arg != "" && b.arg != "query" {
				continue
			}
			for _, e := range parseDict(b.body) {
				r.Query = append(r.Query, collection.Param{Key: e.key, Value: e.value, Disabled: e.disabled})
			}

		case "body":
			variant := b.arg
			if variant == "" {
				variant = "text"
			}
			bodies[variant] = readBody(b)

		case "auth":
			if b.arg != "" {
				auths[b.arg] = readAuth(b)
			}

		default:
			if verb, ok := methodBlocks[b.name]; ok {
				fields := parseDict(b.body)
				r.Method = verb
				r.URL = lookup(fields, "url")
				bodyMode = lookup(fields, "body")
				authMode = lookup(fields, "auth")
			}
		}
	}

	if body, ok := bodies[bodyMode]; ok {
		r.Body = body
	}

	switch authMode {
	case "inherit":
		r.Auth = collection.Auth{Type: collection.AuthInherit}
	case "", "none":
		// already none
	default:
		if a, ok := auths[authMode]; ok {
			r.Auth = a
		}
	}
	return r
}

func readBody(b block) collection.Body {
	switch b.arg {
	case "json":
		return collection.Body{Mode: collection.BodyRaw, Raw: b.body, ContentType: "application/json"}
	case "xml":
		return collection.Body{Mode: collection.BodyRaw, Raw: b.body, ContentType: "application/xml"}
	case "text", "":
		return collection.Body{Mode: collection.BodyRaw, Raw: b.body, ContentType: "text/plain"}
	case "sparql", "graphql":
		return collection.Body{Mode: collection.BodyRaw, Raw: b.body, ContentType: "application/json"}
	case "form-urlencoded":
		return collection.Body{Mode: collection.BodyForm, Form: toParams(parseDict(b.body))}
	case "multipart-form":
		return collection.Body{Mode: collection.BodyMulti, Form: toParams(parseDict(b.body))}
	default:
		return collection.Body{Mode: collection.BodyNone}
	}
}

func readAuth(b block) collection.Auth {
	fields := parseDict(b.body)
	switch b.arg {
	case "bearer":
		return collection.Auth{Type: collection.AuthBearer, Token: lookup(fields, "token")}
	case "basic":
		return collection.Auth{
			Type:     collection.AuthBasic,
			Username: lookup(fields, "username"),
			Password: lookup(fields, "password"),
		}
	case "apikey":
		in := "header"
		if lookup(fields, "placement") == "queryparams" {
			in = "query"
		}
		return collection.Auth{
			Type:  collection.AuthAPIKey,
			Key:   lookup(fields, "key"),
			Value: lookup(fields, "value"),
			In:    in,
		}
	default:
		return collection.Auth{Type: collection.AuthNone}
	}
}

func readEnvironments(dir string) []collection.Environment {
	items, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var envs []collection.Environment
	for _, item := range items {
		if item.IsDir() || !strings.EqualFold(filepath.Ext(item.Name()), ".bru") {
			continue
		}
		blocks, err := readBru(filepath.Join(dir, item.Name()))
		if err != nil {
			continue
		}
		env := collection.Environment{Name: strings.TrimSuffix(item.Name(), filepath.Ext(item.Name()))}
		for _, b := range blocks {
			if b.name != "vars" {
				continue
			}
			for _, e := range parseDict(b.body) {
				env.Variables = append(env.Variables, collection.Variable{Key: e.key, Value: e.value, Disabled: e.disabled})
			}
		}
		envs = append(envs, env)
	}
	return envs
}

func toParams(entries []entry) []collection.Param {
	out := make([]collection.Param, 0, len(entries))
	for _, e := range entries {
		out = append(out, collection.Param{Key: e.key, Value: e.value, Disabled: e.disabled})
	}
	return out
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}
