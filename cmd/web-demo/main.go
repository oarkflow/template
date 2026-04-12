// Command web-demo serves a small HTTP site powered by the SPL template
// engine, demonstrating SSR rendering and streaming.
//
// Usage:
//
//	go run ./cmd/web-demo
package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/oarkflow/template"
)

func main() {
	views := testdataDir("web")
	engine := template.New()
	engine.BaseDir = views

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		out, err := engine.RenderSSRFile("index.html", map[string]any{
			"title":   "SPL Web Demo",
			"counter": 1,
			"items": []any{
				map[string]any{"name": "Reactive templates", "ready": true},
				map[string]any{"name": "Streaming blocks", "ready": false},
				map[string]any{"name": "Hot reload hooks", "ready": true},
			},
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(out))
	})

	http.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := engine.RenderStreamFile(w, "stream.html", map[string]any{
			"title": "Stream Demo",
			"ready": strings.EqualFold(r.URL.Query().Get("ready"), "true"),
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	fmt.Println("web demo listening on :8091")
	if err := http.ListenAndServe(":8091", nil); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// testdataDir returns the absolute path to template/testdata/<sub>.
func testdataDir(sub string) string {
	_, src, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(src), "..", "..", "testdata", sub)
}
