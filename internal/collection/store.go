package collection

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Store holds the collections open in the current window.
type Store struct {
	mu     sync.RWMutex
	order  []string
	byID   map[string]*Collection
	active string
}

// NewStore returns an empty store.
func NewStore() *Store {
	return &Store{byID: map[string]*Collection{}}
}

// Import detects the format of path and loads it, replacing any collection
// previously imported from the same source.
func (s *Store) Import(path string) (*Collection, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(abs); err != nil {
		return nil, fmt.Errorf("cannot open %s: %w", filepath.Base(abs), err)
	}

	var firstErr error
	for _, imp := range Importers() {
		if !imp.Accepts(abs) {
			continue
		}
		c, err := imp.Import(abs)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", imp.Label(), err)
			}
			continue
		}
		if env, ok := imp.(EnvImporter); ok && imp.Kind() == SourceFile {
			attachSiblingEnvs(c, abs, env)
		}
		s.put(c)
		return c, nil
	}
	if firstErr != nil {
		return nil, firstErr
	}
	for _, imp := range EnvImporters() {
		if imp.AcceptsEnv(abs) {
			return nil, fmt.Errorf("%s is an environment, not a collection: import it from the collection it belongs to", filepath.Base(abs))
		}
	}
	return nil, errors.New("unrecognised collection format")
}

// ImportEnv reads a standalone environment file and attaches it to an open
// collection, making it the active one.
func (s *Store) ImportEnv(collectionID, path string) (*Collection, string, error) {
	c := s.Get(collectionID)
	if c == nil {
		return nil, "", errors.New("that collection is no longer open")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, "", err
	}

	imp := envImporterFor(c.Format)
	if imp == nil {
		return nil, "", fmt.Errorf("%s collections do not take separate environment files", c.Format)
	}
	if !imp.AcceptsEnv(abs) {
		return nil, "", fmt.Errorf("%s is not a %s environment", filepath.Base(abs), imp.Label())
	}
	env, err := imp.ImportEnv(abs)
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", filepath.Base(abs), err)
	}

	c.PutEnvironment(env)
	c.SetEnv(env.Name)
	return c, env.Name, nil
}

// attachSiblingEnvs pulls in the environment files sitting next to a
// collection. Nothing in an export links the two, so their folder is the only
// hint available that they belong together.
func attachSiblingEnvs(c *Collection, path string, imp EnvImporter) {
	dir := filepath.Dir(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		sibling := filepath.Join(dir, entry.Name())
		if sibling == path || !imp.AcceptsEnv(sibling) {
			continue
		}
		env, err := imp.ImportEnv(sibling)
		if err != nil {
			continue // a file that only looked like an environment
		}
		c.PutEnvironment(env)
	}
}

// envImporterFor returns the environment reader for a collection format.
func envImporterFor(format string) EnvImporter {
	for _, imp := range EnvImporters() {
		if imp.ID() == format {
			return imp
		}
	}
	return nil
}

// AcceptsEnvFiles reports whether this collection's format keeps environments
// in files of their own, and so has anything to import.
func (c *Collection) AcceptsEnvFiles() bool {
	return envImporterFor(c.Format) != nil
}

// SetEnv switches the active environment of an open collection.
func (s *Store) SetEnv(collectionID, name string) error {
	c := s.Get(collectionID)
	if c == nil {
		return errors.New("that collection is no longer open")
	}
	if !c.SetEnv(name) {
		return fmt.Errorf("%s has no environment named %q", c.Name, name)
	}
	return nil
}

func (s *Store) put(c *Collection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.byID[c.ID]; !exists {
		s.order = append(s.order, c.ID)
	}
	s.byID[c.ID] = c
	s.active = c.ID
}

// Close removes a collection from the store.
func (s *Store) Close(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byID, id)
	for i, existing := range s.order {
		if existing == id {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	if s.active == id {
		s.active = ""
		if len(s.order) > 0 {
			s.active = s.order[len(s.order)-1]
		}
	}
}

// List returns the open collections in import order.
func (s *Store) List() []*Collection {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Collection, 0, len(s.order))
	for _, id := range s.order {
		if c, ok := s.byID[id]; ok {
			out = append(out, c)
		}
	}
	return out
}

// Get returns a collection by ID, or nil.
func (s *Store) Get(id string) *Collection {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.byID[id]
}

// FindRequest locates a request across every open collection.
func (s *Store) FindRequest(id string) (*Collection, *Request) {
	for _, c := range s.List() {
		if r := c.Lookup(id); r != nil {
			return c, r
		}
	}
	return nil, nil
}

// SourceID derives a stable collection ID from its path, so re-importing the
// same file updates it in place instead of duplicating it.
func SourceID(path string) string {
	sum := sha1.Sum([]byte(filepath.Clean(path)))
	return hex.EncodeToString(sum[:8])
}
