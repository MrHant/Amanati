# Repository Guidelines

## Project Structure & Module Organization

Amanati is a Wails desktop API client. The Go entry points are `main.go` and `app.go`. Domain code lives under `internal/`: `collection/` handles collection models and storage, its `postman/` and `bruno/` packages implement import formats, `httpclient/` sends requests, and `ui/` serves views and Go HTML templates. Frontend source is in `frontend/src/`; `frontend/build.mjs` assembles HTMX/Alpine assets and Tailwind CSS into generated `frontend/dist/`. Platform packaging files belong in `build/`, while reusable import fixtures belong in `sampledata/`. Keep tests beside the Go package they cover.

## Build, Test, and Development Commands

Install Go 1.23+, Node 18+, Task, the Wails CLI, and WebView2 on Windows. Run `task` to list all supported workflows.

- `task dev`: launch the app with Wails hot reload.
- `task build`: produce the desktop binary in `build/bin/`.
- `task check`: run formatting verification, `go vet`, and all tests; use this before submitting changes.
- `task test`: execute `go test ./...`.
- `task test:cover`: generate `coverage.out` and open the HTML coverage report.
- `task frontend:build`: rebuild HTML/JavaScript and Tailwind assets without rebuilding Go.
- `task doctor`: validate the local Wails toolchain.

## Coding Style & Naming Conventions

Format Go code with `task fmt` (`gofmt`) and use standard Go naming: short lowercase package names, `PascalCase` exports, and `camelCase` internals. Keep package responsibilities narrow and return errors with useful context. Name templates descriptively with the `.gohtml` suffix. Follow the existing two-space indentation in JavaScript, JSON, CSS, and templates; do not hand-edit generated `frontend/dist/` content.

## Commit Guidelines

Recent commits use concise, imperative, area-prefixed subjects such as `samples - add postman environment files samples`. Follow that pattern (`ui - refine response layout`) and keep each commit focused. 

## Security & Configuration Tips

Do not commit API secrets, tokens, or private Postman environment values.


## Architecture

**The UI is server-rendered HTML fragments, not a JS app.** `main.go` installs
`apiMiddleware` in the Wails asset server: anything under `ui.Prefix` (`/api/`) is handed to
`internal/ui.Server` (a plain `http.ServeMux`), everything else falls through to the embedded
`frontend/dist`. HTMX therefore talks to a real HTTP endpoint inside the desktop app, and
there is no bundler or client-side router.

Consequences worth knowing before changing UI code:

- Wails bindings (`app.go`) exist **only** for native file dialogs — `ImportFile`,
  `ImportDirectory`, `ImportEnvironment`. Everything else belongs in `internal/ui`, not in a
  new binding. The dialog filters are derived from the importer registry, so a new format
  needs no edits to `app.go`.
- `internal/ui/view.go` holds the view models; `editorVM` is serialised to JSON into a
  `data-state` attribute and read back by `requestEditor()` in `frontend/src/index.html`.
  Alpine owns the editor's key-value rows client-side; the server reassembles them on POST
  from `<prefix>-key` / `-value` / `-on` form fields (`rows()` in `server.go`). Adding a field
  to the editor means touching the VM, the template, the Alpine component, and `rows`/`form*`
  parsing together.
- Templates are `.gohtml` in `internal/ui/templates/`, parsed once into a package-level
  `template.Must` in `templates.go` with a small FuncMap (`json`, `methodTone`, `rows`, …).
- Tailwind v4 scans both `frontend/src/index.html` and `internal/ui/templates/` (declared via
  `@source` in `frontend/src/input.css`). Class names produced in Go — `methodTone` — are not
  in any markup, so they are kept alive by an `@source inline(...)` line that must stay in
  sync with that function. daisyUI supplies components and the nord/forest themes.

**Importers are a registry, not a switch.** `internal/collection` defines the format-neutral
model (`Collection` / `Folder` / `Request` / `Environment` / `Vars`) that every importer
produces, so nothing downstream sees Postman- or Bruno-specific shapes.

- A format implements `collection.Importer` and calls `collection.Register` from `init()`;
  `main.go` blank-imports the package. Adding Insomnia/OpenAPI/HAR is one package plus one
  blank import — `Store.Import` picks the importer by `Accepts(path)`, and `Accepts` must be
  cheap and must never error on unrelated files (it is run over whole directories).
- `SourceKind` (`SourceFile` vs `SourceDirectory`) decides which native dialog opens.
  Postman is a single JSON file; Bruno is a directory of `.bru` files with `bruno.json` at
  the root (`internal/collection/bruno/bru.go` is the hand-written `.bru` parser).
- Formats whose environments are separate files additionally implement `EnvImporter`
  (Postman does; Bruno reads its environments during `Import`). Importing a Postman
  collection also pulls in environment files sitting next to it (`attachSiblingEnvs`).
- Every importer must call `collection.Finalize` before returning: it sorts by `Seq`, assigns
  request IDs (which must contain no `/`, since they are a single route segment) and builds
  the lookup index.
- Collection IDs come from `SourceID(path)` — a hash of the path — so re-importing the same
  file updates in place rather than duplicating.

**Mutable state.** `Store` holds the open collections for the window; nothing is persisted.
A `Collection` is built single-handed by its importer and then treated as immutable *except*
for `Environments`/`activeEnv`, which the UI changes while requests may be in flight — reach
those through `PutEnvironment` / `SetEnv` / `ActiveEnv` / `Vars`, never the fields directly.
The active environment belongs to the collection, not to the open editor, so it survives
moving between requests.

**Sending.** `internal/httpclient` takes a *fully resolved* request: `ui.handleSend` expands
`{{vars}}` and drops disabled rows before calling it, so the client never sees a placeholder.
The `Response` echoes `SentURL`/`SentHeaders` so the user can debug variable expansion
without a proxy, and bodies are capped at 8 MiB.

## Testing Guidelines

Standard `testing`, table-driven for parsers, tests beside the package. `internal/ui` tests
drive the real mux with `httptest`; collection tests write fixtures into `t.TempDir()`.
`sampledata/` holds real Postman exports (collections plus paired environment files) usable
as fixtures.
