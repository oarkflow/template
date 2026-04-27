package template

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestRenderCachesCompiledTemplate(t *testing.T) {
	e := New()
	tmpl := `<h1>${title}</h1>`
	first, err := e.Render(tmpl, map[string]any{"title": "One"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := e.Render(tmpl, map[string]any{"title": "Two"})
	if err != nil {
		t.Fatal(err)
	}
	if first != `<h1>One</h1>` || second != `<h1>Two</h1>` {
		t.Fatalf("unexpected outputs: %q %q", first, second)
	}
	if len(e.compiledTextCache) != 1 {
		t.Fatalf("expected one compiled text template, got %d", len(e.compiledTextCache))
	}
}

func TestRenderFileCachesCompiledTemplate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.html")
	if err := os.WriteFile(path, []byte(`<p>${msg}</p>`), 0644); err != nil {
		t.Fatal(err)
	}
	e := New()
	e.BaseDir = dir
	first, err := e.RenderFile("page.html", map[string]any{"msg": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := e.RenderFile("page.html", map[string]any{"msg": "world"})
	if err != nil {
		t.Fatal(err)
	}
	if first != `<p>hello</p>` || second != `<p>world</p>` {
		t.Fatalf("unexpected outputs: %q %q", first, second)
	}
	if len(e.compiledFileCache) != 1 {
		t.Fatalf("expected one compiled file template, got %d", len(e.compiledFileCache))
	}
}

func TestCacheStatsReportsTemplateAndExpressionCaches(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.html")
	if err := os.WriteFile(path, []byte(`<p>${msg}</p>`), 0644); err != nil {
		t.Fatal(err)
	}

	e := New()
	e.BaseDir = dir

	if _, err := e.Render(`<h1>${title}</h1>`, map[string]any{"title": "Hello"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.RenderFile("page.html", map[string]any{"msg": "World"}); err != nil {
		t.Fatal(err)
	}

	stats := e.CacheStats()
	if stats.ParsedFiles != 1 || stats.CompiledFiles != 1 {
		t.Fatalf("expected file caches to contain one entry, got %+v", stats)
	}
	if stats.ParsedTemplates != 1 || stats.CompiledTemplates != 1 {
		t.Fatalf("expected string template caches to contain one entry, got %+v", stats)
	}
	if stats.ExprPrograms == 0 || stats.ExprFastPaths == 0 {
		t.Fatalf("expected expression caches to be populated, got %+v", stats)
	}
	if !stats.GlobalEnvReady {
		t.Fatalf("expected global environment to be initialized, got %+v", stats)
	}
}

func TestCacheStatsAfterInvalidateCaches(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.html")
	if err := os.WriteFile(path, []byte(`<p>${msg}</p>`), 0644); err != nil {
		t.Fatal(err)
	}

	e := New()
	e.BaseDir = dir

	if _, err := e.Render(`<h1>${title}</h1>`, map[string]any{"title": "Hello"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.RenderFile("page.html", map[string]any{"msg": "World"}); err != nil {
		t.Fatal(err)
	}

	e.InvalidateCaches()
	stats := e.CacheStats()
	if stats.ParsedFiles != 0 || stats.ParsedTemplates != 0 || stats.CompiledFiles != 0 || stats.CompiledTemplates != 0 {
		t.Fatalf("expected template caches to be cleared, got %+v", stats)
	}
	if stats.ExprPrograms == 0 || stats.ExprFastPaths == 0 {
		t.Fatalf("expected expression caches to remain warm after template cache invalidation, got %+v", stats)
	}
}

func TestConcurrentRenderIsolation(t *testing.T) {
	e := New()
	tmpl := `@signal(counter = value)@reactive(counter) {<span>${counter}</span>}`
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(v int) {
			defer wg.Done()
			out, err := e.RenderSSR(tmpl, map[string]any{"value": v})
			if err != nil {
				errs <- err
				return
			}
			needle := fmt.Sprintf(`<span>%d</span>`, v)
			if !strings.Contains(out, needle) {
				errs <- fmt.Errorf("missing %s in %q", needle, out)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestConcurrentCompiledTemplateReuse(t *testing.T) {
	e := New()
	tmpl := `<h1>${title}</h1>`
	var wg sync.WaitGroup
	errs := make(chan error, 50)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(v int) {
			defer wg.Done()
			out, err := e.Render(tmpl, map[string]any{"title": fmt.Sprintf("T%d", v)})
			if err != nil {
				errs <- err
				return
			}
			if !strings.Contains(out, fmt.Sprintf("T%d", v)) {
				errs <- fmt.Errorf("unexpected output %q", out)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(e.compiledTextCache) != 1 {
		t.Fatalf("expected one compiled template after concurrent renders, got %d", len(e.compiledTextCache))
	}
}

func TestExprSimple(t *testing.T) {
	e := New()
	out, err := e.Render(`<h1>${title}</h1>`, map[string]any{"title": "Hello"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "<h1>Hello</h1>" {
		t.Fatalf("expected <h1>Hello</h1>, got %q", out)
	}
}

func TestExprAutoEscape(t *testing.T) {
	e := New()
	out, err := e.Render(`<p>${val}</p>`, map[string]any{"val": "<b>bold</b>"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "&lt;b&gt;bold&lt;/b&gt;") {
		t.Fatalf("expected HTML-escaped output, got %q", out)
	}
}

func TestExprRaw(t *testing.T) {
	e := New()
	out, err := e.Render(`<p>${raw val}</p>`, map[string]any{"val": "<b>bold</b>"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "<p><b>bold</b></p>" {
		t.Fatalf("expected raw output, got %q", out)
	}
}

func TestExprNoEscape(t *testing.T) {
	e := New()
	e.AutoEscape = false
	out, err := e.Render(`<p>${val}</p>`, map[string]any{"val": "<b>bold</b>"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "<p><b>bold</b></p>" {
		t.Fatalf("expected unescaped output, got %q", out)
	}
}

func TestExprFilter(t *testing.T) {
	e := New()
	e.AutoEscape = false
	out, err := e.Render(`${name | upper}`, map[string]any{"name": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "HELLO" {
		t.Fatalf("expected HELLO, got %q", out)
	}
}

func TestExprFilterChain(t *testing.T) {
	e := New()
	e.AutoEscape = false
	out, err := e.Render(`${name | upper | trim}`, map[string]any{"name": "  hello  "})
	if err != nil {
		t.Fatal(err)
	}
	if out != "HELLO" {
		t.Fatalf("expected HELLO, got %q", out)
	}
}

func TestExprFilterWithArg(t *testing.T) {
	e := New()
	e.AutoEscape = false
	out, err := e.Render(`${val | truncate 5 ".."}`, map[string]any{"val": "Hello World"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "Hello.." {
		t.Fatalf("expected Hello.., got %q", out)
	}
}

func TestExprSupportsSingleQuotedStrings(t *testing.T) {
	e := New()
	e.AutoEscape = false
	out, err := e.Render(`<h1>${hash('sha256', 'secret')}</h1>`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "<h1>2bb80d537b1da3e38bd30361aa855686") || !strings.HasSuffix(out, "</h1>") {
		t.Fatalf("expected sha256 hash output, got %q", out)
	}
}

func TestIfTrue(t *testing.T) {
	e := New()
	e.AutoEscape = false
	out, err := e.Render(`@if(show) {<p>yes</p>}`, map[string]any{"show": true})
	if err != nil {
		t.Fatal(err)
	}
	if out != "<p>yes</p>" {
		t.Fatalf("expected <p>yes</p>, got %q", out)
	}
}

func TestIfFalse(t *testing.T) {
	e := New()
	out, err := e.Render(`@if(show) {<p>yes</p>}`, map[string]any{"show": false})
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Fatalf("expected empty, got %q", out)
	}
}

func TestIfElse(t *testing.T) {
	e := New()
	e.AutoEscape = false
	out, err := e.Render(`@if(show) {<p>yes</p>} @else {<p>no</p>}`, map[string]any{"show": false})
	if err != nil {
		t.Fatal(err)
	}
	if out != "<p>no</p>" {
		t.Fatalf("expected <p>no</p>, got %q", out)
	}
}

func TestIfElseif(t *testing.T) {
	e := New()
	e.AutoEscape = false
	out, err := e.Render(`@if(x == 1) {A} @elseif(x == 2) {B} @else {C}`, map[string]any{"x": 2})
	if err != nil {
		t.Fatal(err)
	}
	if out != "B" {
		t.Fatalf("expected B, got %q", out)
	}
}

func TestForArray(t *testing.T) {
	e := New()
	e.AutoEscape = false
	out, err := e.Render(`@for(item in items) {<li>${item}</li>}`, map[string]any{
		"items": []any{"a", "b", "c"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out != "<li>a</li><li>b</li><li>c</li>" {
		t.Fatalf("unexpected: %q", out)
	}
}

func TestForWithIndex(t *testing.T) {
	e := New()
	e.AutoEscape = false
	out, err := e.Render(`@for(i, item in items) {${i}:${item} }`, map[string]any{
		"items": []any{"x", "y"},
	})
	if err != nil {
		t.Fatal(err)
	}
	expected := "0:x 1:y "
	if out != expected {
		t.Fatalf("expected %q, got %q", expected, out)
	}
}

func TestForEmpty(t *testing.T) {
	e := New()
	e.AutoEscape = false
	out, err := e.Render(`@for(item in items) {<li>${item}</li>} @empty {<p>none</p>}`, map[string]any{
		"items": []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out != "<p>none</p>" {
		t.Fatalf("expected <p>none</p>, got %q", out)
	}
}

func TestForLoopMeta(t *testing.T) {
	e := New()
	e.AutoEscape = false
	tmpl := `@for(item in items) {${loop.index}:${loop.first}:${loop.last} }`
	out, err := e.Render(tmpl, map[string]any{"items": []any{"a", "b", "c"}})
	if err != nil {
		t.Fatal(err)
	}
	expected := "0:true:false 1:false:false 2:false:true "
	if out != expected {
		t.Fatalf("expected %q, got %q", expected, out)
	}
}

func TestSwitch(t *testing.T) {
	e := New()
	e.AutoEscape = false
	tmpl := `@switch(status) { @case("active") {ON} @case("inactive") {OFF} @default {?} }`
	out, err := e.Render(tmpl, map[string]any{"status": "active"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "ON" {
		t.Fatalf("expected ON, got %q", out)
	}
}

func TestSwitchDefault(t *testing.T) {
	e := New()
	e.AutoEscape = false
	tmpl := `@switch(status) { @case("active") {ON} @default {?} }`
	out, err := e.Render(tmpl, map[string]any{"status": "unknown"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "?" {
		t.Fatalf("expected ?, got %q", out)
	}
}

func TestSwitchMultipleValues(t *testing.T) {
	e := New()
	e.AutoEscape = false
	tmpl := `@switch(x) { @case("a", "b") {AB} @default {other} }`
	out, err := e.Render(tmpl, map[string]any{"x": "b"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "AB" {
		t.Fatalf("expected AB, got %q", out)
	}
}

func TestRawBlock(t *testing.T) {
	e := New()
	tmpl := `@raw {${not parsed} @if(true) {nope}}`
	out, err := e.Render(tmpl, nil)
	if err != nil {
		t.Fatal(err)
	}
	expected := `${not parsed} @if(true) {nope}`
	if out != expected {
		t.Fatalf("expected %q, got %q", expected, out)
	}
}

func TestComment(t *testing.T) {
	e := New()
	e.AutoEscape = false
	tmpl := "@// This is a comment\n<p>hello</p>"
	out, err := e.Render(tmpl, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "<p>hello</p>" {
		t.Fatalf("expected <p>hello</p>, got %q", out)
	}
}

func TestInclude(t *testing.T) {
	dir := t.TempDir()
	partial := filepath.Join(dir, "partial.html")
	os.WriteFile(partial, []byte(`<b>${name}</b>`), 0644)

	e := New()
	e.BaseDir = dir
	e.AutoEscape = false
	out, err := e.Render(`<div>@include("partial.html")</div>`, map[string]any{"name": "World"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "<div><b>World</b></div>" {
		t.Fatalf("unexpected: %q", out)
	}
}

func TestIncludeWithData(t *testing.T) {
	dir := t.TempDir()
	card := filepath.Join(dir, "card.html")
	os.WriteFile(card, []byte(`<div class="card">${cardTitle}</div>`), 0644)

	e := New()
	e.BaseDir = dir
	e.AutoEscape = false
	out, err := e.Render(`@include("card.html", {"cardTitle": "Test"})`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != `<div class="card">Test</div>` {
		t.Fatalf("unexpected: %q", out)
	}
}

func TestLayout(t *testing.T) {
	dir := t.TempDir()
	layout := filepath.Join(dir, "layout.html")
	os.WriteFile(layout, []byte(`<html><body>@block("content") {default}</body></html>`), 0644)

	page := `@extends("layout.html")
@define("content") {<h1>${title}</h1>}`

	e := New()
	e.BaseDir = dir
	e.AutoEscape = false
	out, err := e.Render(page, map[string]any{"title": "My Page"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "<html><body><h1>My Page</h1></body></html>" {
		t.Fatalf("unexpected: %q", out)
	}
}

func TestLayoutDefaultBlock(t *testing.T) {
	dir := t.TempDir()
	layout := filepath.Join(dir, "layout.html")
	os.WriteFile(layout, []byte(`<html>@block("content") {fallback}</html>`), 0644)

	page := `@extends("layout.html")`

	e := New()
	e.BaseDir = dir
	e.AutoEscape = false
	out, err := e.Render(page, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "<html>fallback</html>" {
		t.Fatalf("unexpected: %q", out)
	}
}

func TestGlobals(t *testing.T) {
	e := New()
	e.AutoEscape = false
	e.Globals["siteName"] = "My Site"
	out, err := e.Render(`<title>${siteName}</title>`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "<title>My Site</title>" {
		t.Fatalf("unexpected: %q", out)
	}
}

func TestCustomFilter(t *testing.T) {
	e := New()
	e.AutoEscape = false
	e.RegisterFilter("shout", func(val any, args ...string) string {
		return strings.ToUpper(str(val)) + "!!!"
	})
	out, err := e.Render(`${name | shout}`, map[string]any{"name": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "HELLO!!!" {
		t.Fatalf("expected HELLO!!!, got %q", out)
	}
}

func TestNestedIf(t *testing.T) {
	e := New()
	e.AutoEscape = false
	tmpl := `@if(a) {@if(b) {both} @else {only a}} @else {none}`
	out, err := e.Render(tmpl, map[string]any{"a": true, "b": true})
	if err != nil {
		t.Fatal(err)
	}
	if out != "both" {
		t.Fatalf("expected both, got %q", out)
	}
}

func TestNestedFor(t *testing.T) {
	e := New()
	e.AutoEscape = false
	tmpl := `@for(row in rows) {@for(col in row) {[${col}]}|}`
	out, err := e.Render(tmpl, map[string]any{
		"rows": []any{
			[]any{1, 2},
			[]any{3, 4},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	expected := "[1][2]|[3][4]|"
	if out != expected {
		t.Fatalf("expected %q, got %q", expected, out)
	}
}

func TestExprArithmetic(t *testing.T) {
	e := New()
	e.AutoEscape = false
	out, err := e.Render(`${a + b}`, map[string]any{"a": 10, "b": 20})
	if err != nil {
		t.Fatal(err)
	}
	if out != "30" {
		t.Fatalf("expected 30, got %q", out)
	}
}

func TestExprDotAccess(t *testing.T) {
	e := New()
	e.AutoEscape = false
	out, err := e.Render(`${user.name}`, map[string]any{
		"user": map[string]any{"name": "Alice"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out != "Alice" {
		t.Fatalf("expected Alice, got %q", out)
	}
}

func TestRenderFile(t *testing.T) {
	dir := t.TempDir()
	tmplFile := filepath.Join(dir, "page.html")
	os.WriteFile(tmplFile, []byte(`<h1>${title}</h1>`), 0644)

	e := New()
	e.BaseDir = dir
	e.AutoEscape = false
	out, err := e.RenderFile("page.html", map[string]any{"title": "Test"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "<h1>Test</h1>" {
		t.Fatalf("unexpected: %q", out)
	}
}

func TestFilterSlug(t *testing.T) {
	e := New()
	e.AutoEscape = false
	out, err := e.Render(`${title | slug}`, map[string]any{"title": "Hello World! This is a Test"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "hello-world-this-is-a-test" {
		t.Fatalf("unexpected: %q", out)
	}
}

func TestFilterDefault(t *testing.T) {
	e := New()
	e.AutoEscape = false
	// Use null instead of undefined variable to test default filter
	out, err := e.Render(`${missing | default "N/A"}`, map[string]any{"missing": nil})
	if err != nil {
		t.Fatal(err)
	}
	if out != "N/A" {
		t.Fatalf("expected N/A, got %q", out)
	}
}

func TestFilterNl2br(t *testing.T) {
	e := New()
	e.AutoEscape = false
	out, err := e.Render(`${text | nl2br}`, map[string]any{"text": "line1\nline2"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "line1<br>line2" {
		t.Fatalf("unexpected: %q", out)
	}
}

func TestComplexTemplate(t *testing.T) {
	e := New()
	e.AutoEscape = false
	tmpl := `<html>
<body>
  <h1>${title}</h1>
  @if(items) {
  <ul>
    @for(item in items) {
    <li>${item.name}: ${item.price}</li>
    }
  </ul>
  } @else {
  <p>No items</p>
  }
</body>
</html>`
	out, err := e.Render(tmpl, map[string]any{
		"title": "Shop",
		"items": []any{
			map[string]any{"name": "Widget", "price": 9.99},
			map[string]any{"name": "Gadget", "price": 24.50},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Widget: 9.99") {
		t.Fatalf("missing Widget in output: %q", out)
	}
	if !strings.Contains(out, "Gadget: 24.5") {
		t.Fatalf("missing Gadget in output: %q", out)
	}
}

func TestParseErrors(t *testing.T) {
	e := New()
	_, err := e.Render(`${unclosed`, nil)
	if err == nil {
		t.Fatal("expected error for unclosed expression")
	}
}

func TestEmptyTemplate(t *testing.T) {
	e := New()
	out, err := e.Render(``, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Fatalf("expected empty, got %q", out)
	}
}

func TestPlainText(t *testing.T) {
	e := New()
	out, err := e.Render(`Hello World`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "Hello World" {
		t.Fatalf("expected Hello World, got %q", out)
	}
}

func TestFilterReverse(t *testing.T) {
	e := New()
	e.AutoEscape = false
	out, err := e.Render(`${val | reverse}`, map[string]any{"val": "abc"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "cba" {
		t.Fatalf("expected cba, got %q", out)
	}
}

func TestFilterUrlencode(t *testing.T) {
	e := New()
	e.AutoEscape = false
	out, err := e.Render(`${val | urlencode}`, map[string]any{"val": "hello world"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "hello+world" {
		t.Fatalf("expected hello+world, got %q", out)
	}
}

func TestLogicalOrInExpr(t *testing.T) {
	e := New()
	e.AutoEscape = false
	// SPL's || returns boolean true/false, not the truthy value
	out, err := e.Render(`${a || b}`, map[string]any{"a": false, "b": "fallback"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "true" {
		t.Fatalf("expected true, got %q", out)
	}
}

func TestForHash(t *testing.T) {
	e := New()
	e.AutoEscape = false
	tmpl := `@for(k, v in config) {${k}=${v} }`
	out, err := e.Render(tmpl, map[string]any{
		"config": map[string]any{"host": "localhost", "port": "8080"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Hash iteration order is sorted by key in our renderer
	if !strings.Contains(out, "host=localhost") || !strings.Contains(out, "port=8080") {
		t.Fatalf("unexpected: %q", out)
	}
}

// --- Component tests ---

func TestComponentBasic(t *testing.T) {
	e := New()
	tmpl := `@component("Greeting") {<p>Hello, ${name}!</p>}@render("Greeting", {"name": "Alice"})`
	out, err := e.Render(tmpl, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "<p>Hello, Alice!</p>" {
		t.Fatalf("expected <p>Hello, Alice!</p>, got %q", out)
	}
}

func TestComponentWithDefaultSlot(t *testing.T) {
	e := New()
	tmpl := `@component("Box") {<div>@slot</div>}@render("Box") {content here}`
	out, err := e.Render(tmpl, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "<div>content here</div>" {
		t.Fatalf("expected <div>content here</div>, got %q", out)
	}
}

func TestComponentWithNamedSlots(t *testing.T) {
	e := New()
	tmpl := `@component("Card") {<div><header>@slot("header")</header><main>@slot</main><footer>@slot("footer")</footer></div>}@render("Card") {@fill("header") {Title}Body@fill("footer") {Footer}}`
	out, err := e.Render(tmpl, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "<header>Title</header>") {
		t.Fatalf("missing header slot, got %q", out)
	}
	if !strings.Contains(out, "<footer>Footer</footer>") {
		t.Fatalf("missing footer slot, got %q", out)
	}
	if !strings.Contains(out, "<main>Body</main>") {
		t.Fatalf("missing default slot, got %q", out)
	}
}

func TestComponentWithProps(t *testing.T) {
	e := New()
	tmpl := `@component("Btn") {<button class="${variant}">@slot</button>}@render("Btn", {"variant": "primary"}) {Click me}`
	out, err := e.Render(tmpl, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != `<button class="primary">Click me</button>` {
		t.Fatalf("expected button with primary class, got %q", out)
	}
}

func TestComponentReuse(t *testing.T) {
	e := New()
	tmpl := `@component("Tag") {<span>${label}</span>}@render("Tag", {"label": "Go"})@render("Tag", {"label": "SPL"})`
	out, err := e.Render(tmpl, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "<span>Go</span><span>SPL</span>" {
		t.Fatalf("expected two tags, got %q", out)
	}
}

func TestComponentWithConditional(t *testing.T) {
	e := New()
	tmpl := `@component("Alert") {@if(show) {<div class="alert">${msg}</div>}}@render("Alert", {"show": true, "msg": "Warning!"})`
	out, err := e.Render(tmpl, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != `<div class="alert">Warning!</div>` {
		t.Fatalf("expected alert div, got %q", out)
	}
}

func TestComponentWithLoop(t *testing.T) {
	e := New()
	tmpl := `@component("List") {<ul>@for(item in items) {<li>${item}</li>}</ul>}@render("List", {"items": ["a", "b", "c"]})`
	out, err := e.Render(tmpl, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "<ul><li>a</li><li>b</li><li>c</li></ul>" {
		t.Fatalf("expected list, got %q", out)
	}
}

func TestComponentWithFilter(t *testing.T) {
	e := New()
	tmpl := `@component("Title") {<h1>${text | upper}</h1>}@render("Title", {"text": "hello"})`
	out, err := e.Render(tmpl, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "<h1>HELLO</h1>" {
		t.Fatalf("expected <h1>HELLO</h1>, got %q", out)
	}
}

func TestNestedComponents(t *testing.T) {
	e := New()
	tmpl := `@component("Inner") {<b>@slot</b>}@component("Outer") {<div>@render("Inner") {${text}}</div>}@render("Outer", {"text": "hi"})`
	out, err := e.Render(tmpl, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "<div><b>hi</b></div>" {
		t.Fatalf("expected nested components, got %q", out)
	}
}

func TestComponentUndefined(t *testing.T) {
	e := New()
	_, err := e.Render(`@render("Missing")`, nil)
	if err == nil {
		t.Fatal("expected error for undefined component")
	}
	if !strings.Contains(err.Error(), "undefined component") {
		t.Fatalf("expected 'undefined component' error, got: %v", err)
	}
}

func TestComponentRegisteredViaAPI(t *testing.T) {
	e := New()
	err := e.RegisterComponent("Badge", `<span class="badge">${label}</span>`)
	if err != nil {
		t.Fatal(err)
	}
	out, err := e.Render(`@render("Badge", {"label": "New"})`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != `<span class="badge">New</span>` {
		t.Fatalf("expected badge, got %q", out)
	}
}

func TestComponentEmptySlot(t *testing.T) {
	e := New()
	tmpl := `@component("Wrap") {<div>@slot("missing")|@slot</div>}@render("Wrap") {ok}`
	out, err := e.Render(tmpl, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "<div>|ok</div>" {
		t.Fatalf("expected empty named slot, got %q", out)
	}
}

func TestComponentInheritsParentData(t *testing.T) {
	e := New()
	tmpl := `@component("Show") {<p>${globalVar}</p>}@render("Show")`
	out, err := e.Render(tmpl, map[string]any{"globalVar": "from-parent"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "<p>from-parent</p>" {
		t.Fatalf("expected parent data, got %q", out)
	}
}

func TestComponentPropsObject(t *testing.T) {
	e := New()
	// Without declared props, all props are spread AND available via props.xxx
	tmpl := `@component("Card") {<div>${name} ${props.name}</div>}@render("Card", {"name": "Alice"})`
	out, err := e.Render(tmpl, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "<div>Alice Alice</div>" {
		t.Fatalf("expected props object access, got %q", out)
	}
}

func TestComponentDeclaredProps(t *testing.T) {
	e := New()
	// With declared props: declared names are shorthand, all props via props.xxx
	tmpl := `@component("ProductCard", name, price) {<div>${name}: $${price} (${props.name})</div>}@render("ProductCard", {"name": "Laptop", "price": 999})`
	out, err := e.Render(tmpl, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "<div>Laptop: $999 (Laptop)</div>" {
		t.Fatalf("expected declared props + props object, got %q", out)
	}
}

func TestComponentDeclaredPropsOnlyDeclaredAsShorthand(t *testing.T) {
	e := New()
	// Only declared prop "name" is a shorthand; "extra" is NOT a top-level shorthand but IS in props
	tmpl := `@component("Item", name) {<div>${name} ${props.extra}</div>}@render("Item", {"name": "Foo", "extra": "bar"})`
	out, err := e.Render(tmpl, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "<div>Foo bar</div>" {
		t.Fatalf("expected only declared props as shorthand, got %q", out)
	}
}

func TestComponentDeclaredPropsWithConditional(t *testing.T) {
	e := New()
	tmpl := `@component("ProductCard", name, category, price, onSale) {<div>${name} - ${category | upper}@if(onSale) { <span>SALE</span>}</div>}@render("ProductCard", {"name": "Laptop", "category": "electronics", "price": 999, "onSale": true})`
	out, err := e.Render(tmpl, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Laptop - ELECTRONICS") {
		t.Fatalf("expected declared props in output, got %q", out)
	}
	if !strings.Contains(out, "<span>SALE</span>") {
		t.Fatalf("expected conditional from declared prop, got %q", out)
	}
}

// --- Prop alias + default tests ---

func TestComponentPropAlias(t *testing.T) {
	e := New()
	tmpl := `@component("Card", title as heading) {<h3>${heading}</h3>}@render("Card", {"title": "Hello"})`
	out, err := e.Render(tmpl, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "<h3>Hello</h3>" {
		t.Fatalf("expected alias prop, got %q", out)
	}
}

func TestComponentPropDefault(t *testing.T) {
	e := New()
	tmpl := `@component("Card", price = 0) {<p>$${price}</p>}@render("Card")`
	out, err := e.Render(tmpl, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "<p>$0</p>" {
		t.Fatalf("expected default prop, got %q", out)
	}
}

func TestComponentPropAliasAndDefault(t *testing.T) {
	e := New()
	tmpl := `@component("Card", title as heading = "Untitled") {<h3>${heading}</h3>}@render("Card")`
	out, err := e.Render(tmpl, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "<h3>Untitled</h3>" {
		t.Fatalf("expected alias+default, got %q", out)
	}
}

func TestComponentPropAliasOverridden(t *testing.T) {
	e := New()
	tmpl := `@component("Card", title as heading = "Untitled") {<h3>${heading}</h3>}@render("Card", {"title": "Custom"})`
	out, err := e.Render(tmpl, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "<h3>Custom</h3>" {
		t.Fatalf("expected overridden prop, got %q", out)
	}
}

func TestComponentPropMixed(t *testing.T) {
	e := New()
	tmpl := `@component("Item", name, title as heading = "Default", price = 10) {${name}:${heading}:$${price}}@render("Item", {"name": "Widget", "title": "Custom"})`
	out, err := e.Render(tmpl, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "Widget:Custom:$10" {
		t.Fatalf("expected mixed props, got %q", out)
	}
}

func TestComponentPropDefaultInPropsObject(t *testing.T) {
	e := New()
	// Default values should also be reflected in props.xxx
	tmpl := `@component("Card", color = "blue") {${color}:${props.color}}@render("Card")`
	out, err := e.Render(tmpl, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "blue:blue" {
		t.Fatalf("expected default in props object, got %q", out)
	}
}

// --- @let tests ---

func TestLetBasic(t *testing.T) {
	e := New()
	tmpl := `@let(x = 1 + 2)${x}`
	out, err := e.Render(tmpl, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "3" {
		t.Fatalf("expected 3, got %q", out)
	}
}

func TestLetStringConcat(t *testing.T) {
	e := New()
	tmpl := `@let(full = first + " " + last)${full}`
	out, err := e.Render(tmpl, map[string]any{"first": "John", "last": "Doe"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "John Doe" {
		t.Fatalf("expected John Doe, got %q", out)
	}
}

func TestLetInForLoop(t *testing.T) {
	e := New()
	e.AutoEscape = false
	tmpl := `@for(item in items) {@let(total = item.price * item.qty)${item.name}=$${total} }`
	out, err := e.Render(tmpl, map[string]any{
		"items": []any{
			map[string]any{"name": "A", "price": 10, "qty": 3},
			map[string]any{"name": "B", "price": 5, "qty": 2},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out != "A=$30 B=$10 " {
		t.Fatalf("expected let in loop, got %q", out)
	}
}

func TestLetOverwrite(t *testing.T) {
	e := New()
	tmpl := `@let(x = 1)@let(x = x + 10)${x}`
	out, err := e.Render(tmpl, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "11" {
		t.Fatalf("expected 11, got %q", out)
	}
}

// --- @computed tests ---

func TestComputedBasic(t *testing.T) {
	e := New()
	tmpl := `@computed(total = price * qty)Total: ${total}`
	out, err := e.Render(tmpl, map[string]any{"price": 25, "qty": 4})
	if err != nil {
		t.Fatal(err)
	}
	if out != "Total: 100" {
		t.Fatalf("expected Total: 100, got %q", out)
	}
}

func TestComputedInLoop(t *testing.T) {
	e := New()
	e.AutoEscape = false
	tmpl := `@for(item in items) {@computed(label = item.name + "!")${label} }`
	out, err := e.Render(tmpl, map[string]any{
		"items": []any{
			map[string]any{"name": "Foo"},
			map[string]any{"name": "Bar"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out != "Foo! Bar! " {
		t.Fatalf("expected computed in loop, got %q", out)
	}
}

// --- @watch tests ---

func TestWatchFirstRender(t *testing.T) {
	e := New()
	e.AutoEscape = false
	tmpl := `@watch(x) {RENDERED}rest`
	out, err := e.Render(tmpl, map[string]any{"x": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "RENDEREDrest" {
		t.Fatalf("expected first render, got %q", out)
	}
}

func TestWatchNoChange(t *testing.T) {
	e := New()
	e.AutoEscape = false
	// Same category for all items — header should render only once
	tmpl := `@for(item in items) {@watch(item.cat) {<h2>${item.cat}</h2>}<p>${item.name}</p>}`
	out, err := e.Render(tmpl, map[string]any{
		"items": []any{
			map[string]any{"name": "A", "cat": "fruit"},
			map[string]any{"name": "B", "cat": "fruit"},
			map[string]any{"name": "C", "cat": "fruit"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, "<h2>fruit</h2>") != 1 {
		t.Fatalf("expected 1 header, got %q", out)
	}
	if strings.Count(out, "<p>") != 3 {
		t.Fatalf("expected 3 items, got %q", out)
	}
}

func TestWatchWithChange(t *testing.T) {
	e := New()
	e.AutoEscape = false
	// Category changes — header should render at each change
	tmpl := `@for(item in items) {@watch(item.cat) {<h2>${item.cat}</h2>}<p>${item.name}</p>}`
	out, err := e.Render(tmpl, map[string]any{
		"items": []any{
			map[string]any{"name": "Laptop", "cat": "electronics"},
			map[string]any{"name": "Phone", "cat": "electronics"},
			map[string]any{"name": "Desk", "cat": "furniture"},
			map[string]any{"name": "Chair", "cat": "furniture"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, "<h2>") != 2 {
		t.Fatalf("expected 2 headers, got %q", out)
	}
	if !strings.Contains(out, "<h2>electronics</h2>") {
		t.Fatalf("missing electronics header, got %q", out)
	}
	if !strings.Contains(out, "<h2>furniture</h2>") {
		t.Fatalf("missing furniture header, got %q", out)
	}
}

func TestWatchGroupedList(t *testing.T) {
	e := New()
	e.AutoEscape = false
	tmpl := `@for(item in items) {@watch(item.category) {<h3>${item.category | upper}</h3>}<div>${item.name}: $${item.price}</div>}`
	out, err := e.Render(tmpl, map[string]any{
		"items": []any{
			map[string]any{"name": "Apple", "category": "fruit", "price": 1},
			map[string]any{"name": "Banana", "category": "fruit", "price": 2},
			map[string]any{"name": "Carrot", "category": "vegetable", "price": 3},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, "<h3>") != 2 {
		t.Fatalf("expected 2 group headers, got %q", out)
	}
	if !strings.Contains(out, "<h3>FRUIT</h3>") || !strings.Contains(out, "<h3>VEGETABLE</h3>") {
		t.Fatalf("expected group headers, got %q", out)
	}
}
