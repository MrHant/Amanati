package collection

import (
	"fmt"
	"sort"
	"sync"
)

// Importer turns an on-disk collection of some format into the neutral model.
// New formats (Insomnia, OpenAPI, HAR, ...) only need to implement this and
// call Register from an init function.
type Importer interface {
	// ID is the stable short name of the format, e.g. "postman".
	ID() string
	// Label is shown in the UI, e.g. "Postman v2.1".
	Label() string
	// Kind reports whether this format lives in a single file or a directory.
	Kind() SourceKind
	// Patterns lists file globs for the open dialog, e.g. "*.json". Directory
	// formats return nil.
	Patterns() []string
	// Accepts reports whether path looks like this format. It should be cheap
	// and must not error out on unrelated files.
	Accepts(path string) bool
	// Import reads path and returns a finalized collection.
	Import(path string) (*Collection, error)
}

// EnvImporter is implemented by formats whose environments are exported as
// files of their own, separate from the collection they belong to. Formats
// that keep them inside the collection read them during Import instead.
type EnvImporter interface {
	Importer
	// AcceptsEnv reports whether path looks like an environment of this
	// format. Like Accepts it is run over every file next to an imported
	// collection, so it must stay cheap and must not error on unrelated ones.
	AcceptsEnv(path string) bool
	// EnvPatterns lists file globs for the environment open dialog.
	EnvPatterns() []string
	// ImportEnv reads path into a single environment.
	ImportEnv(path string) (Environment, error)
}

// EnvImporters returns the importers that can read a standalone environment
// file.
func EnvImporters() []EnvImporter {
	out := []EnvImporter{}
	for _, imp := range Importers() {
		if env, ok := imp.(EnvImporter); ok {
			out = append(out, env)
		}
	}
	return out
}

// SourceKind tells the UI which native dialog to open for a format.
type SourceKind int

const (
	SourceFile SourceKind = iota
	SourceDirectory
)

var (
	mu        sync.RWMutex
	importers []Importer
)

// Register adds an importer. Panics on a duplicate ID, which can only be a
// programming error.
func Register(imp Importer) {
	mu.Lock()
	defer mu.Unlock()
	for _, existing := range importers {
		if existing.ID() == imp.ID() {
			panic(fmt.Sprintf("collection: duplicate importer %q", imp.ID()))
		}
	}
	importers = append(importers, imp)
	sort.Slice(importers, func(i, j int) bool { return importers[i].ID() < importers[j].ID() })
}

// Importers returns every registered importer.
func Importers() []Importer {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Importer, len(importers))
	copy(out, importers)
	return out
}

// ImportersFor returns the importers that handle the given source kind.
func ImportersFor(kind SourceKind) []Importer {
	out := []Importer{}
	for _, imp := range Importers() {
		if imp.Kind() == kind {
			out = append(out, imp)
		}
	}
	return out
}
