package bruno

import "testing"

const sample = `meta {
  name: Create user
  type: http
  seq: 2
}

post {
  url: {{baseUrl}}/users?trace=1
  body: json
  auth: bearer
}

headers {
  Content-Type: application/json
  ~X-Debug: 1
}

auth:bearer {
  token: {{token}}
}

body:json {
  {
    "name": "Ada",
    "note": "a } brace inside a string"
  }
}
`

func TestParseBlocks(t *testing.T) {
	blocks, err := parseBlocks(sample)
	if err != nil {
		t.Fatalf("parseBlocks: %v", err)
	}

	want := []struct{ name, arg string }{
		{"meta", ""},
		{"post", ""},
		{"headers", ""},
		{"auth", "bearer"},
		{"body", "json"},
	}
	if len(blocks) != len(want) {
		t.Fatalf("got %d blocks, want %d: %+v", len(blocks), len(want), blocks)
	}
	for i, w := range want {
		if blocks[i].name != w.name || blocks[i].arg != w.arg {
			t.Errorf("block %d = %q:%q, want %q:%q", i, blocks[i].name, blocks[i].arg, w.name, w.arg)
		}
	}

	body := blocks[4].body
	const wantBody = "{\n  \"name\": \"Ada\",\n  \"note\": \"a } brace inside a string\"\n}"
	if body != wantBody {
		t.Errorf("json body =\n%q\nwant\n%q", body, wantBody)
	}
}

func TestParseDictDisabledEntries(t *testing.T) {
	blocks, err := parseBlocks(sample)
	if err != nil {
		t.Fatalf("parseBlocks: %v", err)
	}
	entries := parseDict(blocks[2].body)

	if len(entries) != 2 {
		t.Fatalf("got %d header entries, want 2", len(entries))
	}
	if entries[0].key != "Content-Type" || entries[0].value != "application/json" || entries[0].disabled {
		t.Errorf("entry 0 = %+v", entries[0])
	}
	if entries[1].key != "X-Debug" || !entries[1].disabled {
		t.Errorf("entry 1 = %+v, want disabled X-Debug", entries[1])
	}
}

func TestParseDictKeepsColonsInValues(t *testing.T) {
	entries := parseDict("url: https://example.com:8443/v1")
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].value != "https://example.com:8443/v1" {
		t.Errorf("value = %q", entries[0].value)
	}
}

func TestParseDictMultilineValue(t *testing.T) {
	entries := parseDict("query: '''\n  first\n  second\n'''\nother: 1")
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2: %+v", len(entries), entries)
	}
	if entries[0].value != "first\nsecond" {
		t.Errorf("multiline value = %q", entries[0].value)
	}
	if entries[1].key != "other" {
		t.Errorf("second entry = %+v", entries[1])
	}
}

func TestParseBlocksUnclosed(t *testing.T) {
	if _, err := parseBlocks("meta {\n  name: x\n"); err == nil {
		t.Fatal("expected an error for an unclosed block")
	}
}
