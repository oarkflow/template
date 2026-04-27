package template

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTemplateSSRHydrationAndReactiveNodes(t *testing.T) {
	e := New()
	e.AutoEscape = false
	e.SecureMode = true
	out, err := e.RenderSSR(`@signal(count = 3)<div>@bind(count)</div>@click("Plus", count, "inc", "1")@effect(count) {<span>${count}</span>}@reactive(count) {@if(count) {<strong>${count}</strong>} @else {<em>zero</em>}}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `data-spl-bind="count"`) {
		t.Fatalf("expected hydration bind marker, got %q", out)
	}
	if !strings.Contains(out, `data-spl-hydration`) {
		t.Fatalf("expected hydration script, got %q", out)
	}
	if !strings.Contains(out, `__SPL_SIGNAL__count__`) {
		t.Fatalf("expected effect body output, got %q", out)
	}
	if !strings.Contains(out, `data-spl-on-click="[{&#34;kind&#34;:&#34;add&#34;,&#34;target&#34;:&#34;count&#34;,&#34;value&#34;:1}]"`) {
		t.Fatalf("expected click hydration action, got %q", out)
	}
	if !strings.Contains(out, `data-spl-view="1"`) {
		t.Fatalf("expected reactive view wrapper, got %q", out)
	}
	if !strings.Contains(out, `"Source"`) {
		t.Fatalf("expected hydration payload source field, got %q", out)
	}
	if !strings.Contains(out, `data-spl-else="count"`) {
		t.Fatalf("expected signal-aware conditional markers, got %q", out)
	}
	if !strings.Contains(out, `data-spl-runtime`) {
		t.Fatalf("expected runtime script tag, got %q", out)
	}
	if !strings.Contains(out, `SPL.debug`) {
		t.Fatalf("expected debug hooks in runtime, got %q", out)
	}
	if !strings.Contains(out, `captureFocus`) || !strings.Contains(out, `restoreFocus`) {
		// After obfuscation, check for mangled property names
		hasFocus := strings.Contains(out, `document.activeElement`) && strings.Contains(out, `setSelectionRange`)
		if !hasFocus {
			t.Fatalf("expected focus-preserving rerender hooks, got %q", out)
		}
	}
}

func TestCompleteReactiveShowcaseTemplate(t *testing.T) {
	e := New()
	e.BaseDir = filepath.Join("testdata", "templates")
	e.SecureMode = true
	out, err := e.RenderSSRFile("complete_reactive_showcase.html", map[string]any{
		"siteTitle":    "SPL UI",
		"pageTitle":    "Complete Reactive Showcase",
		"description":  "all major template features working together",
		"footerText":   "footer",
		"counter":      2,
		"panelOpen":    false,
		"apiBase":      "http://127.0.0.1:3020",
		"userName":     "sujit",
		"featureCount": 1,
		"searchQuery":  "spl reactive templates",
		"summary":      "summary text for truncation",
		"navLinks":     []any{map[string]any{"href": "/complete", "label": "Showcase"}},
		"features":     []any{map[string]any{"name": "signals", "status": "working"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, needle := range []string{"Complete Reactive Showcase", "Reactive Control Panel", `data-spl-on-click="[{&#34;kind&#34;:&#34;toggle&#34;,&#34;target&#34;:&#34;panelOpen&#34;}]"`, `data-spl-hydration`, `Hydrated with SSR`} {
		if !strings.Contains(out, needle) {
			t.Fatalf("expected %q in output, got %q", needle, out)
		}
	}
	if !strings.Contains(out, `data-spl-api-url="http://127.0.0.1:3020/api/todos"`) {
		t.Fatalf("expected template API hook, got %q", out)
	}
	if !strings.Contains(out, `data-spl-model="todoDraft"`) {
		t.Fatalf("expected attribute-driven bindings in output, got %q", out)
	}
	if !strings.Contains(out, `counter=2 draft=`) {
		t.Fatalf("expected imported component rendered counter text, got %q", out)
	}
	if !strings.Contains(out, `data-spl-api-method="POST"`) {
		t.Fatalf("expected template API method hook, got %q", out)
	}
	if !strings.Contains(out, `data-spl-api-form="closest"`) {
		t.Fatalf("expected template API form hook, got %q", out)
	}
}

func TestImportDirectiveRegistersComponents(t *testing.T) {
	e := New()
	e.BaseDir = filepath.Join("testdata", "templates")
	out, err := e.Render(`@import("components_ui.html")@render("TodoItem", {"title": "Imported", "completed": true}) {<span>slot</span>}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, needle := range []string{"Imported", "Completed: true", "slot"} {
		if !strings.Contains(out, needle) {
			t.Fatalf("expected %q in imported component output, got %q", needle, out)
		}
	}
}

func TestAttributeEventRewrite(t *testing.T) {
	e := New()
	e.SecureMode = true
	out, err := e.RenderSSR(`@signal(counter = 1)@signal(open = false)@reactive(counter, open) {<button on:click="counter += 1">Add</button><button on:click="toggle(open)">Toggle</button><span>${counter}</span>}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `data-spl-on-click="[{&#34;kind&#34;:&#34;add&#34;,&#34;target&#34;:&#34;counter&#34;,&#34;value&#34;:1}]"`) {
		t.Fatalf("expected increment attribute rewrite, got %q", out)
	}
	if !strings.Contains(out, `data-spl-on-click="[{&#34;kind&#34;:&#34;toggle&#34;,&#34;target&#34;:&#34;open&#34;}]"`) {
		t.Fatalf("expected function-style toggle rewrite, got %q", out)
	}
	if !strings.Contains(out, `type="application/json" data-spl-hydration`) {
		t.Fatalf("expected CSP-safe JSON hydration payload, got %q", out)
	}
}

func TestQuotedAttributeBracesInsideReactiveBlock(t *testing.T) {
	e := New()
	e.SecureMode = true
	_, err := e.RenderSSR(`@signal(counter = 1)@signal(lastAction = "none")@reactive(counter, lastAction) {<button on:click="(() => { counter += 2; lastAction = 'anonymous function'; })">Anon</button><button on:click="counter += 1; lastAction = 'inline'">Inline</button><p>${counter}</p>}`, nil)
	if err == nil {
		t.Fatal("expected unsafe anonymous function hydration to be rejected")
	}
	if !strings.Contains(err.Error(), "unsafe event expression") {
		t.Fatalf("expected unsafe event expression error, got %v", err)
	}
}

func TestHandlerDirectiveSerializedForHydration(t *testing.T) {
	e := New()
	e.SecureMode = true
	out, err := e.RenderSSR(`@signal(counter = 1)@signal(lastAction = "none")@handler(incrementByTwo) { counter += 2; lastAction = 'handler:incrementByTwo'; }@reactive(counter, lastAction) {<button on:click="incrementByTwo">Named</button><p>${counter}</p>}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"handlers"`) || !strings.Contains(out, `incrementByTwo`) {
		t.Fatalf("expected serialized template handler in hydration payload, got %q", out)
	}
	if !strings.Contains(out, `data-spl-on-click="incrementByTwo"`) {
		t.Fatalf("expected named handler reference in output, got %q", out)
	}
	if !strings.Contains(out, `"kind":"add"`) || !strings.Contains(out, `"kind":"set"`) {
		t.Fatalf("expected safe structured handler actions, got %q", out)
	}
}

func TestDebounceAttributeEventRewrite(t *testing.T) {
	e := New()
	e.SecureMode = true
	out, err := e.RenderSSR(`@signal(query = "")@reactive(query) {<input on:input="debounce(query = 'go', 250)" value="${query}" />}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `data-spl-on-input="{&#34;delay&#34;:250,&#34;actions&#34;:[{&#34;kind&#34;:&#34;set&#34;,&#34;target&#34;:&#34;query&#34;,&#34;value&#34;:&#34;go&#34;}]}"`) {
		t.Fatalf("expected debounced attribute rewrite, got %q", out)
	}
}

func TestDebounceNamedHandlerRewrite(t *testing.T) {
	e := New()
	e.SecureMode = true
	out, err := e.RenderSSR(`@signal(counter = 0)@handler(increment) { counter += 1; }@reactive(counter) {<button on:click="debounce(increment, 300)">Add</button>}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `data-spl-on-click="{&#34;delay&#34;:300,&#34;handler&#34;:&#34;increment&#34;}"`) {
		t.Fatalf("expected debounced handler rewrite, got %q", out)
	}
}

func TestTemplateStreamingDirectives(t *testing.T) {
	e := New()
	e.AutoEscape = false
	var buf strings.Builder
	e.BaseDir = filepath.Join("testdata", "web")
	err := e.RenderStreamFile(&buf, "stream.html", map[string]any{"title": "Stream", "ready": false})
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "Loading deferred chunk") {
		t.Fatalf("expected fallback content, got %q", out)
	}
	if !strings.Contains(out, "document.getElementById") {
		t.Fatalf("expected deferred patch script, got %q", out)
	}
	if !strings.Contains(out, "Not ready yet") {
		t.Fatalf("expected lazy fallback, got %q", out)
	}
}

func TestHotReloadInvalidatesTemplateCaches(t *testing.T) {
	dir := t.TempDir()
	tmplPath := filepath.Join(dir, "page.html")
	if err := os.WriteFile(tmplPath, []byte("before"), 0644); err != nil {
		t.Fatal(err)
	}
	e := New()
	e.BaseDir = dir
	first, err := e.RenderFile("page.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	if first != "before" {
		t.Fatalf("unexpected first render: %q", first)
	}
	if err := os.WriteFile(tmplPath, []byte("after"), 0644); err != nil {
		t.Fatal(err)
	}
	e.InvalidateCaches()
	second, err := e.RenderFile("page.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	if second != "after" {
		t.Fatalf("expected cache invalidation, got %q", second)
	}
}
