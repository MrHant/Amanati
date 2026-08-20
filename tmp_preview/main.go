package main

import (
	"log"
	"net/http"
	"strings"

	"github.com/mrhant/amanati/internal/collection"
	"github.com/mrhant/amanati/internal/ui"

	_ "github.com/mrhant/amanati/internal/collection/bruno"
	_ "github.com/mrhant/amanati/internal/collection/postman"
)

func main() {
	store := collection.NewStore()
	if _, err := store.Import("sampledata/postman/col1_httpbin/httpbin.postman_collection.json"); err != nil {
		log.Printf("sample import: %v", err)
	}
	fragments := ui.New(store)
	fs := http.FileServer(http.Dir("frontend/dist"))
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, ui.Prefix) {
			fragments.ServeHTTP(w, r)
			return
		}
		fs.ServeHTTP(w, r)
	})
	log.Println("preview on http://127.0.0.1:8791")
	log.Fatal(http.ListenAndServe("127.0.0.1:8791", nil))
}
