package bruno

import (
	"fmt"
	"strings"
)

// block is one top-level `name { ... }` section of a .bru file. The name may
// carry a qualifier after a colon, e.g. `body:json` or `auth:bearer`.
type block struct {
	name string
	arg  string
	body string
}

// entry is one `key: value` line of a dictionary block. A leading `~` marks the
// entry disabled.
type entry struct {
	key      string
	value    string
	disabled bool
}

// parseBlocks splits a .bru document into its top-level blocks.
func parseBlocks(src string) ([]block, error) {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	var blocks []block

	i := 0
	for i < len(src) {
		// Skip whitespace and // comments between blocks.
		for i < len(src) {
			if isSpace(src[i]) {
				i++
				continue
			}
			if strings.HasPrefix(src[i:], "//") {
				nl := strings.IndexByte(src[i:], '\n')
				if nl < 0 {
					i = len(src)
				} else {
					i += nl + 1
				}
				continue
			}
			break
		}
		if i >= len(src) {
			break
		}

		open := strings.IndexByte(src[i:], '{')
		if open < 0 {
			break // trailing junk, nothing more to read
		}
		name := strings.TrimSpace(src[i : i+open])
		if name == "" {
			return nil, fmt.Errorf("unnamed block at offset %d", i)
		}

		start := i + open + 1
		end, err := matchBrace(src, start)
		if err != nil {
			return nil, fmt.Errorf("block %q: %w", name, err)
		}

		head, arg, _ := strings.Cut(name, ":")
		blocks = append(blocks, block{
			name: strings.TrimSpace(head),
			arg:  strings.TrimSpace(arg),
			body: dedent(src[start:end]),
		})
		i = end + 1
	}
	return blocks, nil
}

// matchBrace returns the index of the `}` closing the block that starts at
// from, skipping over double-quoted strings so JSON bodies survive.
func matchBrace(src string, from int) (int, error) {
	depth := 1
	for i := from; i < len(src); i++ {
		switch src[i] {
		case '\\':
			i++ // escape outside a string is harmless to skip
		case '"':
			i = skipString(src, i)
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i, nil
			}
		}
	}
	return 0, fmt.Errorf("unclosed block")
}

// skipString returns the index of the quote closing the string that opens at
// from. An unterminated string is treated as a literal quote character, so a
// stray `"` in a text body cannot swallow the rest of the file.
func skipString(src string, from int) int {
	for i := from + 1; i < len(src); i++ {
		switch src[i] {
		case '\\':
			i++
		case '"':
			return i
		case '\n':
			return from
		}
	}
	return from
}

// dedent strips the common leading indentation that Bruno writes inside blocks
// and trims the blank first/last lines.
func dedent(s string) string {
	lines := strings.Split(strings.Trim(s, "\n"), "\n")

	indent := -1
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		n := len(line) - len(strings.TrimLeft(line, " \t"))
		if indent < 0 || n < indent {
			indent = n
		}
	}
	if indent <= 0 {
		return strings.Join(lines, "\n")
	}
	for i, line := range lines {
		if len(line) >= indent {
			lines[i] = line[indent:]
		} else {
			lines[i] = strings.TrimLeft(line, " \t")
		}
	}
	return strings.Join(lines, "\n")
}

// parseDict reads the `key: value` lines of a dictionary block. A value may be
// wrapped in triple single-quotes to span several lines.
func parseDict(body string) []entry {
	var entries []entry
	lines := strings.Split(body, "\n")

	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		disabled := strings.HasPrefix(key, "~")
		key = strings.TrimSpace(strings.TrimPrefix(key, "~"))
		if key == "" {
			continue
		}

		if strings.HasPrefix(value, "'''") {
			var buf []string
			rest := strings.TrimPrefix(value, "'''")
			if trailing, closed := strings.CutSuffix(rest, "'''"); closed && rest != "" {
				value = trailing
			} else {
				buf = append(buf, rest)
				for i+1 < len(lines) {
					i++
					if strings.TrimSpace(lines[i]) == "'''" {
						break
					}
					buf = append(buf, lines[i])
				}
				value = dedent(strings.Join(buf, "\n"))
			}
		}
		entries = append(entries, entry{key: key, value: value, disabled: disabled})
	}
	return entries
}

func lookup(entries []entry, key string) string {
	for _, e := range entries {
		if e.key == key {
			return e.value
		}
	}
	return ""
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}
