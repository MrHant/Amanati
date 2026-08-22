// Package collection defines the format-neutral model that every importer
// produces, so the rest of the app never sees Postman- or Bruno-specific shapes.
package collection

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Collection is one imported API collection, whatever its source format.
//
// Everything but the environment selection is fixed once the importer hands it
// over. That selection changes while requests may be in flight, so it is
// guarded by envMu and reached through methods.
type Collection struct {
	ID        string
	Name      string
	Format    string // importer ID, e.g. "postman" / "bruno"
	Source    string // file or directory it was read from
	Variables []Variable
	Root      *Folder

	// Set directly by the importer; afterwards go through PutEnvironment.
	Environments []Environment

	envMu     sync.RWMutex
	activeEnv string

	index map[string]*Request
}

// Environment is a named set of variables that overrides the collection ones.
type Environment struct {
	Name      string
	Source    string // file it was read from, empty when inline
	Variables []Variable
}

// Variable is a single {{name}} substitution.
type Variable struct {
	Key      string
	Value    string
	Disabled bool
}

// Folder groups requests and nested folders. A collection has one implicit root.
type Folder struct {
	Name     string
	Seq      int
	Folders  []*Folder
	Requests []*Request
}

// Request is a single HTTP call.
type Request struct {
	ID     string
	Name   string
	Seq    int
	Method string
	URL    string

	Headers []Param
	Query   []Param
	Body    Body
	Auth    Auth
}

// Param is an enabled/disabled key-value pair (header, query param, form field).
type Param struct {
	Key      string
	Value    string
	Disabled bool
}

// Body modes understood by the client.
const (
	BodyNone   = "none"
	BodyRaw    = "raw"
	BodyForm   = "form-urlencoded"
	BodyMulti  = "multipart-form"
	BodyBinary = "binary"
)

// Body is the request payload.
type Body struct {
	Mode        string
	Raw         string
	ContentType string // suggested Content-Type for raw bodies
	Form        []Param
}

// Auth types understood by the client.
const (
	AuthNone    = "none"
	AuthInherit = "inherit"
	AuthBearer  = "bearer"
	AuthBasic   = "basic"
	AuthAPIKey  = "apikey"
)

// Auth is the (small) subset of auth schemes handled natively. Anything else
// an importer sees is left as AuthNone; the user can still add headers by hand.
type Auth struct {
	Type     string
	Token    string
	Username string
	Password string
	Key      string
	Value    string
	In       string // "header" or "query", for apikey
}

// Method returns the HTTP verb, upper-cased, defaulting to GET.
func (r *Request) Verb() string {
	if r.Method == "" {
		return "GET"
	}
	return strings.ToUpper(r.Method)
}

// Finalize sorts the tree, assigns stable request IDs and builds the lookup
// index. Every importer must call it before returning a collection.
func Finalize(c *Collection) *Collection {
	if c.Root == nil {
		c.Root = &Folder{}
	}
	if c.Name == "" {
		c.Name = "Untitled collection"
	}
	if c.activeEnv == "" && len(c.Environments) > 0 {
		c.activeEnv = c.Environments[0].Name
	}
	c.index = map[string]*Request{}
	finalizeFolder(c, c.Root, "")
	return c
}

func finalizeFolder(c *Collection, f *Folder, path string) {
	sort.SliceStable(f.Folders, func(i, j int) bool { return f.Folders[i].Seq < f.Folders[j].Seq })
	sort.SliceStable(f.Requests, func(i, j int) bool { return f.Requests[i].Seq < f.Requests[j].Seq })

	// IDs must stay free of "/" so they fit a single {id} route segment.
	for i, r := range f.Requests {
		r.ID = fmt.Sprintf("%s_%s%d", c.ID, path, i)
		c.index[r.ID] = r
	}
	for i, sub := range f.Folders {
		finalizeFolder(c, sub, fmt.Sprintf("%s%d.", path, i))
	}
}

// Lookup returns the request with the given ID, or nil.
func (c *Collection) Lookup(id string) *Request {
	if c.index == nil {
		return nil
	}
	return c.index[id]
}

// Vars flattens collection variables plus the named environment (which wins)
// into a resolver. An env of "" resolves collection variables only.
func (c *Collection) Vars(env string) Vars {
	v := Vars{}
	for _, item := range c.Variables {
		if !item.Disabled {
			v[item.Key] = item.Value
		}
	}
	if env == "" {
		return v
	}

	c.envMu.RLock()
	defer c.envMu.RUnlock()
	for _, e := range c.Environments {
		if !strings.EqualFold(e.Name, env) {
			continue
		}
		for _, item := range e.Variables {
			if !item.Disabled {
				v[item.Key] = item.Value
			}
		}
	}
	return v
}

// EnvNames lists the environments defined by the collection.
func (c *Collection) EnvNames() []string {
	c.envMu.RLock()
	defer c.envMu.RUnlock()
	names := make([]string, 0, len(c.Environments))
	for _, e := range c.Environments {
		names = append(names, e.Name)
	}
	return names
}

// ActiveEnv is the environment requests in this collection are sent with, or
// "" for none.
func (c *Collection) ActiveEnv() string {
	c.envMu.RLock()
	defer c.envMu.RUnlock()
	return c.activeEnv
}

// SetEnv selects the active environment, reporting false and changing nothing
// for a name the collection does not define. "" means no environment.
func (c *Collection) SetEnv(name string) bool {
	c.envMu.Lock()
	defer c.envMu.Unlock()
	if name == "" {
		c.activeEnv = ""
		return true
	}
	for _, e := range c.Environments {
		if strings.EqualFold(e.Name, name) {
			c.activeEnv = e.Name
			return true
		}
	}
	return false
}

// PutEnvironment adds an environment, replacing one of the same name so a
// re-import updates in place. The first one to arrive becomes active.
func (c *Collection) PutEnvironment(env Environment) {
	c.envMu.Lock()
	defer c.envMu.Unlock()
	for i, existing := range c.Environments {
		if strings.EqualFold(existing.Name, env.Name) {
			c.Environments[i] = env
			return
		}
	}
	c.Environments = append(c.Environments, env)
	if c.activeEnv == "" {
		c.activeEnv = env.Name
	}
}

// Vars resolves {{placeholders}}. Both Postman and Bruno use this syntax.
type Vars map[string]string

var varPattern = regexp.MustCompile(`\{\{\s*([^{}\s][^{}]*?)\s*\}\}`)

// Expand replaces every known {{key}}. Unknown keys are left untouched so the
// user can see what is missing in the sent request.
func (v Vars) Expand(s string) string {
	if len(v) == 0 || !strings.Contains(s, "{{") {
		return s
	}
	// Bounded passes so a variable referencing itself cannot loop forever.
	for i := 0; i < 5; i++ {
		out := varPattern.ReplaceAllStringFunc(s, func(m string) string {
			key := varPattern.FindStringSubmatch(m)[1]
			if val, ok := v[key]; ok {
				return val
			}
			return m
		})
		if out == s {
			break
		}
		s = out
	}
	return s
}

// ExpandParams applies Expand to a slice of params, dropping disabled ones.
func (v Vars) ExpandParams(in []Param) []Param {
	out := make([]Param, 0, len(in))
	for _, p := range in {
		if p.Disabled {
			continue
		}
		out = append(out, Param{Key: v.Expand(p.Key), Value: v.Expand(p.Value)})
	}
	return out
}
