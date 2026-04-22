package template

import (
	"strings"
	"testing"
)

func TestReproExactTemplate1(t *testing.T) {
	e := New()
	// Exact template from user's issue - components with CSS braces
	tmpl := `@component("Badge", label, color) {
  <style>.badge { display: inline-block; padding: 2px 8px; border-radius: 12px; font-size: 12px; color: white; background: ${color | default "#666"}; }</style>
  <span class="badge">${label}</span>
}

@component("Card", title, body, tag, tagColor) {
  <style>.card { border: 1px solid #ddd; border-radius: 8px; padding: 16px; margin: 8px 0; }</style>
  <div class="card">
    <h3>${title} @render("Badge", {"label": tag, "color": tagColor})</h3>
    <p>${body}</p>
  </div>
}

<h1>Component Demo</h1>
@render("Card", {"title": "Getting Started", "body": "Install SPL and run your first script.", "tag": "New", "tagColor": "#22c55e"})
@render("Card", {"title": "Templates", "body": "Build dynamic HTML with SPL expressions.", "tag": "Guide", "tagColor": "#3b82f6"})
@render("Card", {"title": "Filters", "body": "Transform output with 25+ built-in filters.", "tag": "Popular", "tagColor": "#ef4444"})`

	out, err := e.Render(tmpl, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Getting Started") {
		t.Fatalf("expected 'Getting Started' in output, got %q", out)
	}
	if !strings.Contains(out, `<span class="badge">New</span>`) {
		t.Fatalf("expected Badge rendered, got %q", out)
	}
	if !strings.Contains(out, `<span class="badge">Guide</span>`) {
		t.Fatalf("expected Badge 'Guide' rendered, got %q", out)
	}
	if !strings.Contains(out, `<span class="badge">Popular</span>`) {
		t.Fatalf("expected Badge 'Popular' rendered, got %q", out)
	}
}

func TestReproExactTemplate2(t *testing.T) {
	e := New()
	// Named slots with CSS braces in component
	tmpl := `@component("Panel") {
  <style>.panel { border: 1px solid #ccc; border-radius: 8px; overflow: hidden; margin: 12px 0; } .panel-header { background: #f0f0f0; padding: 8px 16px; font-weight: bold; border-bottom: 1px solid #ccc; } .panel-body { padding: 16px; } .panel-footer { background: #fafafa; padding: 8px 16px; font-size: 12px; color: #666; border-top: 1px solid #ccc; } .status-green { color: green; }</style>
  <div class="panel">
    <div class="panel-header">
      @slot("header")
    </div>
    <div class="panel-body">
      @slot
    </div>
    <div class="panel-footer">
      @slot("footer")
    </div>
  </div>
}

<h1>Named Slots Demo</h1>

@let(userName = "John Doe")
@let(role = "Developer")
@let(lastLogin = "2023-10-01")

@render("Panel") {
  @fill("header") { User Profile }
  <p>Name: ${userName}</p>
  <p>Role: ${role | title}</p>
  @fill("footer") { Last login: ${lastLogin} }
}

@render("Panel") {
  @fill("header") { System Status }
  <p class="status-green">All systems operational.</p>
  @fill("footer") { Updated just now }
}`

	out, err := e.Render(tmpl, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "User Profile") {
		t.Fatalf("expected 'User Profile' in header slot, got %q", out)
	}
	if !strings.Contains(out, "John Doe") {
		t.Fatalf("expected 'John Doe' in body, got %q", out)
	}
	if !strings.Contains(out, "System Status") {
		t.Fatalf("expected 'System Status' in second panel, got %q", out)
	}
}

func TestReproExactTemplate3(t *testing.T) {
	e := New()
	// @let, @computed, @for with CSS braces in style block
	tmpl := `<style>
table { border-collapse: collapse; width: 100%; }
th, td { padding: 8px; }
th { background: #f0f0f0; text-align: left; }
td:nth-child(2) { text-align: center; }
td:nth-child(3) { text-align: right; }
td:nth-child(4) { text-align: right; font-weight: bold; }
</style>
@let(name = "World")
@let(greeting = "Hello, " + name + "!")
<h1>${greeting}</h1>

<h2>Order Summary</h2>
@let(items = [{"name": "Widget", "price": 19.99, "qty": 3}, {"name": "Gadget", "price": 9.99, "qty": 1}])
<table>
  <tr><th>Item</th><th>Qty</th><th>Price</th><th>Total</th></tr>
@for(item in items) {
  @computed(lineTotal = item.price * item.qty)
  <tr><td>${item.name}</td><td>${item.qty}</td><td>$${item.price}</td><td>$${lineTotal}</td></tr>
}
</table>`

	out, err := e.Render(tmpl, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Hello, World!") {
		t.Fatalf("expected 'Hello, World!' in output, got %q", out)
	}
	if !strings.Contains(out, "Widget") {
		t.Fatalf("expected 'Widget' in output, got %q", out)
	}
	if !strings.Contains(out, "59.97") {
		t.Fatalf("expected lineTotal 59.97 in output, got %q", out)
	}
}
