package main

import (
	"embed"
	"log"
	"net/http"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"github.com/mrhant/amanati/internal/collection"
	"github.com/mrhant/amanati/internal/ui"

	// Importers register themselves; adding a format is one blank import.
	_ "github.com/mrhant/amanati/internal/collection/bruno"
	_ "github.com/mrhant/amanati/internal/collection/postman"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	store := collection.NewStore()
	app := NewApp(store)
	fragments := ui.New(store)

	err := wails.Run(&options.App{
		Title:     "Amanati",
		Width:     1280,
		Height:    820,
		MinWidth:  900,
		MinHeight: 600,
		AssetServer: &assetserver.Options{
			Assets:     assets,
			Middleware: apiMiddleware(fragments),
		},
		OnStartup: app.startup,
		Bind:      []any{app},
	})
	if err != nil {
		log.Fatalf("amanati: %v", err)
	}
}

// apiMiddleware routes /api/* to the fragment server and lets everything else
// fall through to the embedded static assets. This is what allows HTMX to work
// against a real HTTP endpoint inside a desktop app.
func apiMiddleware(fragments http.Handler) assetserver.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, ui.Prefix) {
				fragments.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
