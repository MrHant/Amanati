package postman

import (
	"encoding/json"
	"strconv"
	"strings"
)

// The v2.1 schema spells several fields as a "oneOf": a request is an object or
// a bare URL string, a header list is an array or a raw header block, a host is
// a string or its dot-separated parts. Exporters other than Postman itself add
// further slop on top (numeric ports, "true" instead of true). Each of those
// shapes gets a small UnmarshalJSON here, so that a collection which is valid
// per the schema never fails to open.

type document struct {
	Info struct {
		Name   string `json:"name"`
		Schema string `json:"schema"`
	} `json:"info"`
	Item     []item `json:"item"`
	Variable []kv   `json:"variable"`
	Auth     *auth  `json:"auth"`
}

type item struct {
	Name    string   `json:"name"`
	Item    []item   `json:"item"`
	Request *request `json:"request"`
}

type request struct {
	Method string     `json:"method"`
	Header headerList `json:"header"`
	URL    url        `json:"url"`
	Body   *body      `json:"body"`
	Auth   *auth      `json:"auth"`
}

// UnmarshalJSON accepts the shorthand form, where the whole request is just the
// URL to GET.
func (r *request) UnmarshalJSON(data []byte) error {
	if isString(data) {
		var raw string
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		*r = request{Method: "GET", URL: url{raw: raw}}
		return nil
	}
	type alias request
	var tmp alias
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}
	*r = request(tmp)
	return nil
}

type body struct {
	Mode       string         `json:"mode"`
	Raw        string         `json:"raw"`
	URLEncoded []kv           `json:"urlencoded"`
	FormData   []kv           `json:"formdata"`
	GraphQL    map[string]any `json:"graphql"`
	Disabled   flexBool       `json:"disabled"`
	Options    options        `json:"options"`
}

// options is the free-form per-mode settings blob. Only the raw language is of
// any use here, and whatever else ends up in there must not fail the import.
type options struct {
	Raw struct {
		Language string `json:"language"`
	} `json:"raw"`
}

func (o *options) UnmarshalJSON(data []byte) error {
	type alias options
	var tmp alias
	if err := json.Unmarshal(data, &tmp); err == nil {
		*o = options(tmp)
	}
	return nil
}

type kv struct {
	Key      string   `json:"key"`
	Value    loose    `json:"value"`
	Disabled flexBool `json:"disabled"`
}

// headerList is either an array of key-value objects or the raw header block
// ("Accept: application/json\n"); the schema allows both.
type headerList []kv

func (h *headerList) UnmarshalJSON(data []byte) error {
	if isString(data) {
		var raw string
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		*h = parseHeaderBlock(raw)
		return nil
	}
	var list []kv
	if err := json.Unmarshal(data, &list); err != nil {
		return err
	}
	*h = list
	return nil
}

// parseHeaderBlock splits the raw spelling of a header list, in which a
// disabled header is commented out with "//".
func parseHeaderBlock(raw string) []kv {
	var out []kv
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		disabled := strings.HasPrefix(line, "//")
		if disabled {
			line = strings.TrimSpace(strings.TrimPrefix(line, "//"))
		}
		key, value, found := strings.Cut(line, ":")
		if !found || strings.TrimSpace(key) == "" {
			continue
		}
		out = append(out, kv{
			Key:      strings.TrimSpace(key),
			Value:    loose(strings.TrimSpace(value)),
			Disabled: flexBool(disabled),
		})
	}
	return out
}

// url is either a string or an object, depending on how the collection was
// exported; both spellings are valid v2.1.
type url struct {
	raw      string
	Protocol string   `json:"protocol"`
	Host     strList  `json:"host"`
	Path     segments `json:"path"`
	Port     loose    `json:"port"`
	Query    []kv     `json:"query"`
}

func (u *url) UnmarshalJSON(data []byte) error {
	if isString(data) {
		return json.Unmarshal(data, &u.raw)
	}
	type alias url
	var tmp struct {
		alias
		Raw string `json:"raw"`
	}
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}
	*u = url(tmp.alias)
	u.raw = tmp.Raw
	return nil
}

// Raw returns the URL text, rebuilding it from parts when the export omitted
// the "raw" field.
func (u url) Raw() string {
	if u.raw != "" {
		return u.raw
	}
	var b strings.Builder
	if u.Protocol != "" {
		b.WriteString(u.Protocol + "://")
	}
	b.WriteString(strings.Join(u.Host, "."))
	if port := u.Port.String(); port != "" {
		b.WriteString(":" + port)
	}
	if len(u.Path) > 0 {
		b.WriteString("/" + strings.Join(u.Path, "/"))
	}
	return b.String()
}

// strList accepts a single string or an array of them, the two ways a host is
// written.
type strList []string

func (s *strList) UnmarshalJSON(data []byte) error {
	if isString(data) {
		var one string
		if err := json.Unmarshal(data, &one); err != nil {
			return err
		}
		*s = strList{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(data, &many); err != nil {
		return err
	}
	*s = many
	return nil
}

// segments is a URL path: the whole thing as one string, or an array whose
// entries are either plain strings or {"value": ":id"} path-variable objects.
type segments []string

func (s *segments) UnmarshalJSON(data []byte) error {
	if isString(data) {
		var one string
		if err := json.Unmarshal(data, &one); err != nil {
			return err
		}
		*s = segments{one}
		return nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(data, &items); err != nil {
		return err
	}
	out := make(segments, 0, len(items))
	for _, raw := range items {
		if isString(raw) {
			var part string
			if err := json.Unmarshal(raw, &part); err != nil {
				return err
			}
			out = append(out, part)
			continue
		}
		var v struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		}
		// A segment we cannot read is skipped rather than failed: the rest of
		// the path is still worth showing, and "raw" usually carries the URL.
		if err := json.Unmarshal(raw, &v); err != nil {
			continue
		}
		switch {
		case v.Value != "":
			out = append(out, v.Value)
		case v.Key != "":
			out = append(out, ":"+v.Key)
		}
	}
	*s = out
	return nil
}

// loose accepts a JSON string, number or bool, since exports are inconsistent
// about the type of header and variable values.
type loose string

func (l *loose) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "null" {
		*l = ""
		return nil
	}
	if isString(data) {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*l = loose(s)
		return nil
	}
	*l = loose(trimmed)
	return nil
}

func (l loose) String() string { return string(l) }

// flexBool tolerates the string spelling of "disabled" that some converters
// emit instead of a JSON bool.
type flexBool bool

func (b *flexBool) UnmarshalJSON(data []byte) error {
	if isString(data) {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		v, err := strconv.ParseBool(strings.TrimSpace(s))
		*b = flexBool(err == nil && v)
		return nil
	}
	var v bool
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*b = flexBool(v)
	return nil
}

type auth struct {
	Type   string    `json:"type"`
	Bearer authAttrs `json:"bearer"`
	Basic  authAttrs `json:"basic"`
	APIKey authAttrs `json:"apikey"`
}

// authAttrs holds the {key, value} pairs of one scheme. Postman writes them as
// an array; other tools write a plain {"token": "..."} object.
type authAttrs []kv

func (a *authAttrs) UnmarshalJSON(data []byte) error {
	if strings.HasPrefix(strings.TrimSpace(string(data)), "{") {
		var obj map[string]loose
		if err := json.Unmarshal(data, &obj); err != nil {
			return err
		}
		out := make(authAttrs, 0, len(obj))
		for key, value := range obj {
			out = append(out, kv{Key: key, Value: value})
		}
		*a = out
		return nil
	}
	var list []kv
	if err := json.Unmarshal(data, &list); err != nil {
		return err
	}
	*a = list
	return nil
}

func (a *auth) lookup(scheme, key string) string {
	var list []kv
	switch scheme {
	case "bearer":
		list = a.Bearer
	case "basic":
		list = a.Basic
	case "apikey":
		list = a.APIKey
	}
	for _, item := range list {
		if item.Key == key {
			return item.Value.String()
		}
	}
	return ""
}

// isString reports whether a JSON value is a string literal.
func isString(data []byte) bool {
	return strings.HasPrefix(strings.TrimSpace(string(data)), `"`)
}
