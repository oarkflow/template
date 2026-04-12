// Command ssr demonstrates server-side rendering with JavaScript hydration.
// The SPL template engine renders reactive templates on the server, then
// emits JavaScript that "hydrates" the HTML for interactivity in the browser.
//
// Usage:
//
//	go run ./examples/ssr
//
// Then visit http://localhost:8092
package main

import (
	"fmt"
	"net/http"
	"os"

	template "github.com/oarkflow/template"
)

func main() {
	engine := template.New()

	tmpl := `<!DOCTYPE html>
<html>
<head>
  <title>${title}</title>
  <style>
    body { font-family: sans-serif; max-width: 600px; margin: 2rem auto; padding: 0 1rem; }
    button { padding: 0.5rem 1rem; margin: 0.25rem; border-radius: 6px; border: 1px solid #ccc; cursor: pointer; }
    .panel { padding: 1rem; background: #f6f8fa; border-radius: 8px; margin-top: 1rem; }
  </style>
</head>
<body>
  <h1>${title}</h1>
  <p>This page was rendered on the server with SSR hydration.</p>

  @signal(counter = start)
  @signal(panelOpen = false)

  <p>Counter: @bind(counter)</p>

  <div>
    <button on:click="counter += 1">Increment</button>
    <button on:click="counter -= 1">Decrement</button>
    <button on:click="toggle(panelOpen)">Toggle Panel</button>
  </div>

  @effect(counter) {
    <p>Effect: counter is now ${counter}</p>
  }

  @reactive(counter, panelOpen) {
    <div class="panel">
      <strong>Reactive view — count:</strong> ${counter}
      @if(panelOpen) {
        <div style="margin-top:0.5rem">
          <h3>Panel Content</h3>
          <ul>
          @for(item in items) {
            <li>${item.name} — ${item.status | title}</li>
          }
          </ul>
        </div>
      } @else {
        <p>Panel is closed. Click "Toggle Panel" to open.</p>
      }
    </div>
  }
</body>
</html>`

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		out, err := engine.RenderSSR(tmpl, map[string]any{
			"title": "SPL SSR Demo",
			"start": 0,
			"items": []any{
				map[string]any{"name": "Signals", "status": "working"},
				map[string]any{"name": "Effects", "status": "working"},
				map[string]any{"name": "Reactive views", "status": "working"},
			},
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(out))
	})

	addr := ":8092"
	fmt.Printf("SSR demo listening on http://localhost%s\n", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
