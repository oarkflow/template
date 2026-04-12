// Command components demonstrates the SPL template component system:
// reusable components with declared props, aliases, defaults, and named slots.
//
// Usage:
//
//	go run ./examples/components
package main

import (
	"fmt"
	"os"

	template "github.com/oarkflow/template"
)

func main() {
	engine := template.New()
	engine.AutoEscape = true

	// ── Basic component with props ──
	tmpl := `@component("Badge", label, color) {
  <span style="display:inline-block;padding:2px 8px;border-radius:12px;font-size:12px;color:white;background:${color | default "#666"}">${label}</span>
}

@component("Card", title, body, tag, tagColor) {
  <div style="border:1px solid #ddd;border-radius:8px;padding:16px;margin:8px 0">
    <h3>${title} @render("Badge", {"label": tag, "color": tagColor})</h3>
    <p>${body}</p>
  </div>
}

<h1>Component Demo</h1>
@render("Card", {"title": "Getting Started", "body": "Install SPL and run your first script.", "tag": "New", "tagColor": "#22c55e"})
@render("Card", {"title": "Templates", "body": "Build dynamic HTML with SPL expressions.", "tag": "Guide", "tagColor": "#3b82f6"})
@render("Card", {"title": "Filters", "body": "Transform output with 25+ built-in filters.", "tag": "Popular", "tagColor": "#ef4444"})`

	out, err := engine.Render(tmpl, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "render error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("=== Basic Components ===")
	fmt.Println(out)

	// ── Named slots ──
	tmpl = `@component("Panel") {
  <div style="border:1px solid #ccc;border-radius:8px;overflow:hidden;margin:12px 0">
    <div style="background:#f0f0f0;padding:8px 16px;font-weight:bold;border-bottom:1px solid #ccc">
      @slot("header")
    </div>
    <div style="padding:16px">
      @slot
    </div>
    <div style="background:#fafafa;padding:8px 16px;font-size:12px;color:#666;border-top:1px solid #ccc">
      @slot("footer")
    </div>
  </div>
}

<h1>Named Slots Demo</h1>

@render("Panel") {
  @fill("header") { User Profile }
  <p>Name: ${userName}</p>
  <p>Role: ${role | title}</p>
  @fill("footer") { Last login: ${lastLogin} }
}

@render("Panel") {
  @fill("header") { System Status }
  <p style="color:green">All systems operational.</p>
  @fill("footer") { Updated just now }
}`

	out, err = engine.Render(tmpl, map[string]any{
		"userName":  "Alice",
		"role":      "administrator",
		"lastLogin": "2 hours ago",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "render error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("\n=== Named Slots ===")
	fmt.Println(out)

	// ── Props with aliases and defaults ──
	tmpl = `@component("InfoCard", title as heading = "Untitled", subtitle = "No description", color = "#3b82f6") {
  <div style="border:2px solid ${color};border-radius:8px;padding:16px;margin:8px 0">
    <h3 style="color:${color}">${heading}</h3>
    <p style="color:#666">${subtitle}</p>
  </div>
}

<h1>Prop Aliases & Defaults</h1>

@render("InfoCard", {"title": "Custom Card", "subtitle": "With all props set", "color": "#ef4444"})
@render("InfoCard", {"title": "Partial Props"})
@render("InfoCard")`

	out, err = engine.Render(tmpl, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "render error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("\n=== Prop Aliases & Defaults ===")
	fmt.Println(out)
}
