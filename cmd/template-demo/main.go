// Command template-demo renders the template showcase to demonstrate all
// SPL template engine features: expressions, filters, conditionals, loops,
// switch, includes, layout inheritance, raw blocks, auto-escaping, and more.
//
// Usage:
//
//	go run ./cmd/template-demo
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/oarkflow/template"
)

func main() {
	engine := template.New()
	engine.BaseDir = testdataDir("templates")
	engine.Globals["siteTitle"] = "SPL Demo"
	engine.Globals["footerHTML"] = `&copy; 2026 SPL Project &mdash; <a href="/about">About</a>`
	engine.Globals["navLinks"] = []any{
		map[string]any{"href": "/docs", "label": "Docs"},
		map[string]any{"href": "/examples", "label": "Examples"},
		map[string]any{"href": "/playground", "label": "Playground"},
	}

	data := map[string]any{
		"pageTitle":     "Template Engine Showcase",
		"userName":      "alice",
		"userId":        "42",
		"longText":      "This is a really long piece of text that should get truncated by the filter",
		"searchQuery":   "spl template engine features",
		"lowercaseText": "hello world from spl",
		"isAdmin":       false,
		"isModerator":   true,
		"products": []any{
			map[string]any{
				"name":   "Widget Pro",
				"price":  29.99,
				"onSale": true,
				"tags":   []any{"TOOLS", "Featured", "NEW"},
			},
			map[string]any{
				"name":   "Gadget Mini",
				"price":  9.49,
				"onSale": false,
				"tags":   []any{"ELECTRONICS", "Compact"},
			},
		},
		"colors":      []any{"red", "green", "blue", "yellow"},
		"settings":    map[string]any{"host": "localhost", "port": "8080", "debug": "true"},
		"orderStatus": "shipped",
		"cards": []any{
			map[string]any{"title": "Getting Started", "body": "Install SPL and run your first script.", "badge": "New"},
			map[string]any{"title": "Templates", "body": "Build dynamic HTML with SPL expressions.", "badge": nil},
			map[string]any{"title": "Filters", "body": "Transform output with 25+ built-in filters.", "badge": "Popular"},
		},
		"dangerousInput": `<script>alert("xss")</script>`,
		"itemCount":      5,
		"price":          200,
		"discount":       15,
	}

	out, err := engine.RenderFile("showcase.html", data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "render error: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(out)
}

// testdataDir returns the absolute path to template/testdata/<sub>.
func testdataDir(sub string) string {
	_, src, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(src), "..", "..", "testdata", sub)
}
