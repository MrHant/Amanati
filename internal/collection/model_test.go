package collection

import "testing"

func TestVarsExpand(t *testing.T) {
	vars := Vars{"host": "example.com", "path": "users", "self": "{{self}}"}

	cases := []struct{ in, want string }{
		{"https://{{host}}/{{path}}", "https://example.com/users"},
		{"https://{{ host }}/x", "https://example.com/x"},
		{"{{missing}}/x", "{{missing}}/x"},
		{"no placeholders", "no placeholders"},
		{"{{self}}", "{{self}}"}, // must terminate rather than recurse
	}
	for _, c := range cases {
		if got := vars.Expand(c.in); got != c.want {
			t.Errorf("Expand(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestVarsExpandNested(t *testing.T) {
	vars := Vars{"base": "https://{{host}}", "host": "example.com"}
	if got := vars.Expand("{{base}}/v1"); got != "https://example.com/v1" {
		t.Errorf("nested expansion = %q", got)
	}
}

func TestExpandParamsDropsDisabled(t *testing.T) {
	vars := Vars{"v": "1"}
	in := []Param{
		{Key: "a", Value: "{{v}}"},
		{Key: "b", Value: "2", Disabled: true},
	}
	out := vars.ExpandParams(in)

	if len(out) != 1 {
		t.Fatalf("got %d params, want 1", len(out))
	}
	if out[0].Key != "a" || out[0].Value != "1" {
		t.Errorf("param = %+v", out[0])
	}
}

func TestFinalizeSortsBySeq(t *testing.T) {
	c := Finalize(&Collection{
		ID: "cid",
		Root: &Folder{
			Requests: []*Request{{Name: "second", Seq: 2}, {Name: "first", Seq: 1}},
			Folders:  []*Folder{{Name: "b", Seq: 2}, {Name: "a", Seq: 1}},
		},
	})

	if c.Root.Requests[0].Name != "first" {
		t.Errorf("requests not sorted: %q first", c.Root.Requests[0].Name)
	}
	if c.Root.Folders[0].Name != "a" {
		t.Errorf("folders not sorted: %q first", c.Root.Folders[0].Name)
	}
	if c.Name != "Untitled collection" {
		t.Errorf("missing name not defaulted: %q", c.Name)
	}
}

func TestVerbDefaultsToGet(t *testing.T) {
	if got := (&Request{}).Verb(); got != "GET" {
		t.Errorf("Verb() = %q, want GET", got)
	}
	if got := (&Request{Method: "post"}).Verb(); got != "POST" {
		t.Errorf("Verb() = %q, want POST", got)
	}
}
