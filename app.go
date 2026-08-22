package main

import (
	"context"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/mrhant/amanati/internal/collection"
)

// App holds the bindings callable from JavaScript. Everything else in the UI
// goes over HTTP to the fragment server; these methods exist only for the
// things the browser cannot do — native file dialogs.
type App struct {
	ctx   context.Context
	store *collection.Store
}

// NewApp wires the bindings to a store.
func NewApp(store *collection.Store) *App {
	return &App{store: store}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// Result is what the frontend gets back from a binding.
type Result struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// ImportFile opens a native picker for single-file collection formats
// (currently Postman) and imports the chosen file.
func (a *App) ImportFile() Result {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Import collection",
		Filters: fileFilters(),
	})
	if err != nil {
		return Result{Message: err.Error()}
	}
	return a.importPath(path)
}

// ImportDirectory opens a native folder picker for directory-based formats
// (currently Bruno).
func (a *App) ImportDirectory() Result {
	path, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Open collection folder",
	})
	if err != nil {
		return Result{Message: err.Error()}
	}
	return a.importPath(path)
}

// ImportEnvironment opens a picker for a standalone environment file and
// attaches it to a collection.
func (a *App) ImportEnvironment(collectionID string) Result {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Import environment",
		Filters: envFilters(),
	})
	if err != nil {
		return Result{Message: err.Error()}
	}
	if path == "" {
		return Result{Message: ""} // dialog cancelled
	}
	c, name, err := a.store.ImportEnv(collectionID, path)
	if err != nil {
		return Result{Message: err.Error()}
	}
	return Result{OK: true, Message: name + " into " + c.Name}
}

func (a *App) importPath(path string) Result {
	if path == "" {
		return Result{Message: ""} // dialog cancelled
	}
	c, err := a.store.Import(path)
	if err != nil {
		return Result{Message: err.Error()}
	}
	return Result{OK: true, Message: c.Name}
}

// fileFilters builds the dialog filters from the importer registry, so a new
// file-based format shows up without touching this file.
func fileFilters() []runtime.FileFilter {
	var filters []runtime.FileFilter
	var all []string

	for _, imp := range collection.ImportersFor(collection.SourceFile) {
		patterns := imp.Patterns()
		if len(patterns) == 0 {
			continue
		}
		filters = append(filters, runtime.FileFilter{
			DisplayName: imp.Label() + " (" + strings.Join(patterns, " ") + ")",
			Pattern:     strings.Join(patterns, ";"),
		})
		all = append(all, patterns...)
	}
	if len(filters) > 1 {
		filters = append([]runtime.FileFilter{{
			DisplayName: "All collections",
			Pattern:     strings.Join(all, ";"),
		}}, filters...)
	}
	return withAllFiles(filters)
}

// envFilters does the same for the formats that keep environments in files of
// their own.
func envFilters() []runtime.FileFilter {
	var filters []runtime.FileFilter

	for _, imp := range collection.EnvImporters() {
		patterns := imp.EnvPatterns()
		if len(patterns) == 0 {
			continue
		}
		filters = append(filters, runtime.FileFilter{
			DisplayName: imp.Label() + " environment (" + strings.Join(patterns, " ") + ")",
			Pattern:     strings.Join(patterns, ";"),
		})
	}
	return withAllFiles(filters)
}

func withAllFiles(filters []runtime.FileFilter) []runtime.FileFilter {
	return append(filters, runtime.FileFilter{DisplayName: "All files (*.*)", Pattern: "*.*"})
}
