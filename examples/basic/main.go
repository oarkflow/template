// Command basic demonstrates core SPL template features: variables, filters,
// conditionals, loops, switch statements, and auto-escaping.
//
// Usage:
//
//	go run ./examples/basic
package main

import (
	"fmt"
	"os"

	template "github.com/oarkflow/template"
)

func main() {
	engine := template.New()
	engine.AutoEscape = true

	// ── Variables and filters ──
	tmpl := `<h1>${title | upper}</h1>
<p>Hello, ${name | title}!</p>
<p>Slug: ${title | slug}</p>
<p>Truncated: ${bio | truncate 30 "..."}</p>
<p>URL-safe: ${query | urlencode}</p>`

	out, err := engine.Render(tmpl, map[string]any{
		"title": "getting started with spl templates",
		"name":  "alice",
		"bio":   "A software engineer who loves building template engines and web tools",
		"query": "spl template engine",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "render error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("=== Variables & Filters ===")
	fmt.Println(out)

	// ── Conditionals ──
	tmpl = `@if(isAdmin) {
  <p>Welcome, administrator!</p>
} @elseif(isModerator) {
  <p>Welcome, moderator ${name}!</p>
} @else {
  <p>Welcome, ${name}!</p>
}`
	out, err = engine.Render(tmpl, map[string]any{
		"isAdmin":     false,
		"isModerator": true,
		"name":        "Bob",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "render error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("\n=== Conditionals ===")
	fmt.Println(out)

	// ── Loops ──
	tmpl = `<h2>Shopping List</h2>
<ul>
@for(i, item in items) {
  <li>#${i + 1}: ${item | title} (first=${loop.first}, last=${loop.last})</li>
}
</ul>

@for(x in empty) {
  <li>${x}</li>
} @empty {
  <p><em>This list is empty.</em></p>
}`
	out, err = engine.Render(tmpl, map[string]any{
		"items": []any{"apples", "bread", "cheese", "milk"},
		"empty": []any{},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "render error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("\n=== Loops ===")
	fmt.Println(out)

	// ── Switch ──
	tmpl = `@switch(status) {
  @case("pending") {
    <span style="color:orange">Pending</span>
  }
  @case("shipped", "in_transit") {
    <span style="color:blue">In Transit</span>
  }
  @case("delivered") {
    <span style="color:green">Delivered</span>
  }
  @default {
    <span>Unknown: ${status}</span>
  }
}`
	out, err = engine.Render(tmpl, map[string]any{
		"status": "shipped",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "render error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("\n=== Switch ===")
	fmt.Println(out)

	// ── Auto-escaping ──
	tmpl = `<p>Escaped (safe): ${userInput}</p>
<p>Raw (opt-in dangerous): ${raw userInput}</p>`
	out, err = engine.Render(tmpl, map[string]any{
		"userInput": `<script>alert("xss")</script>`,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "render error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("\n=== Auto-Escaping ===")
	fmt.Println(out)
}
