# Amanati

Opinionated API client.

Built with Wails (Go + WebView2), HTMX, Alpine.js and
Tailwind CSS.

## Prerequisites

- Go 1.23+
- Node 18+
- Wails CLI: `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- WebView2 runtime (preinstalled on Windows 11)

Check the environment with `wails doctor`.

## Running

Common commands are in the [Taskfile](Taskfile.yml); run `task` to list them.

```sh
task dev             # hot-reloading dev build
task build           # produces build/bin/Amanati.exe
task check           # format check, vet and tests — what CI would run
task clean           # drop build output and the compiled frontend
task test            # run tests
task test:cover      # show test coverage report
```
