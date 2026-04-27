package template

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecurityRenderFileRejectsPathTraversal(t *testing.T) {
	root := t.TempDir()
	viewsDir := filepath.Join(root, "views")
	if err := os.MkdirAll(viewsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "secret.html"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := New()
	e.BaseDir = viewsDir

	_, err := e.RenderFile("../secret.html", nil)
	if err == nil {
		t.Fatal("expected path traversal to be rejected")
	}
	if !strings.Contains(err.Error(), "escapes base directory") {
		t.Fatalf("expected path traversal error, got %v", err)
	}
}

func TestSecurityRenderFileRejectsAbsolutePath(t *testing.T) {
	root := t.TempDir()
	viewsDir := filepath.Join(root, "views")
	if err := os.MkdirAll(viewsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	secretPath := filepath.Join(root, "secret.html")
	if err := os.WriteFile(secretPath, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := New()
	e.BaseDir = viewsDir

	_, err := e.RenderFile(secretPath, nil)
	if err == nil {
		t.Fatal("expected absolute path to be rejected")
	}
	if !strings.Contains(err.Error(), "absolute template paths are not allowed") {
		t.Fatalf("expected absolute path error, got %v", err)
	}
}

func TestSecurityIncludeRejectsPathTraversal(t *testing.T) {
	root := t.TempDir()
	viewsDir := filepath.Join(root, "views")
	if err := os.MkdirAll(viewsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "secret.html"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(viewsDir, "index.html"), []byte(`@include("../secret.html")`), 0o644); err != nil {
		t.Fatal(err)
	}

	e := New()
	e.BaseDir = viewsDir

	_, err := e.RenderFile("index.html", nil)
	if err == nil {
		t.Fatal("expected include path traversal to be rejected")
	}
	if !strings.Contains(err.Error(), "escapes base directory") {
		t.Fatalf("expected include traversal error, got %v", err)
	}
}

func TestSecurityImportRejectsPathTraversal(t *testing.T) {
	root := t.TempDir()
	viewsDir := filepath.Join(root, "views")
	if err := os.MkdirAll(viewsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "component.html"), []byte(`@component("Secret"){secret}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(viewsDir, "index.html"), []byte(`@import("../component.html")@render("Secret")`), 0o644); err != nil {
		t.Fatal(err)
	}

	e := New()
	e.BaseDir = viewsDir

	_, err := e.RenderFile("index.html", nil)
	if err == nil {
		t.Fatal("expected import path traversal to be rejected")
	}
	if !strings.Contains(err.Error(), "escapes base directory") {
		t.Fatalf("expected import traversal error, got %v", err)
	}
}

func TestSecurityExtendsRejectsPathTraversal(t *testing.T) {
	root := t.TempDir()
	viewsDir := filepath.Join(root, "views")
	if err := os.MkdirAll(viewsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "layout.html"), []byte(`<html>${children}</html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(viewsDir, "page.html"), []byte(`@extends("../layout.html")<p>Hello</p>`), 0o644); err != nil {
		t.Fatal(err)
	}

	e := New()
	e.BaseDir = viewsDir

	_, err := e.RenderFile("page.html", nil)
	if err == nil {
		t.Fatal("expected layout path traversal to be rejected")
	}
	if !strings.Contains(err.Error(), "escapes base directory") {
		t.Fatalf("expected layout traversal error, got %v", err)
	}
}

func TestSecurityHydrationRuntimeURLEscapesAttribute(t *testing.T) {
	e := New()
	e.HydrationRuntimeURL = `/static/runtime.js" data-pwn="1`

	out, err := e.RenderSSR(`@signal(counter = 1)<div>${counter}</div>`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, `data-pwn="1"`) {
		t.Fatalf("expected hydration runtime URL to be escaped, got %q", out)
	}
	if !strings.Contains(out, `/static/runtime.js&#34; data-pwn=&#34;1`) {
		t.Fatalf("expected escaped quote entities in runtime URL, got %q", out)
	}
}

func TestSecuritySSRHydrationUsesJSONPayloadAndNonce(t *testing.T) {
	e := New()
	e.CSPNonce = "nonce-123"
	e.SecureMode = true

	out, err := e.RenderSSR(`@signal(counter = 1)<button on:click="counter += 1">Add</button>`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `<script type="application/json" data-spl-hydration>`) {
		t.Fatalf("expected inert JSON hydration payload, got %q", out)
	}
	if strings.Contains(out, `(function(){var payload=`) {
		t.Fatalf("expected no inline bootstrap wrapper, got %q", out)
	}
	if !strings.Contains(out, `nonce="nonce-123"`) {
		t.Fatalf("expected CSP nonce on executable hydration script, got %q", out)
	}
}

func TestSecurityRuntimeRemovesDynamicCodeExecution(t *testing.T) {
	e := New()
	e.SecureMode = true
	runtimeJS := e.RuntimeJSRaw()
	if strings.Contains(runtimeJS, "new Function") {
		t.Fatalf("expected runtime to avoid new Function, got %q", runtimeJS)
	}
	if strings.Contains(runtimeJS, "eval(") {
		t.Fatalf("expected runtime to avoid eval, got %q", runtimeJS)
	}
}

func TestSecurityRejectsActiveHTMLInSecureMode(t *testing.T) {
	e := New()
	e.SecureMode = true
	_, err := e.Render(`<div>${raw payload}</div>`, map[string]any{"payload": `<script>alert(1)</script>`})
	if err == nil {
		t.Fatal("expected active HTML to be rejected in secure mode")
	}
	if !strings.Contains(err.Error(), "script tags are not allowed") {
		t.Fatalf("expected secure mode script rejection, got %v", err)
	}
}

func TestCompatibilityLegacySSRHandlersRemainRenderable(t *testing.T) {
	e := New()
	out, err := e.RenderSSR(`@signal(open = false)@handler(openDialog) { var dlg = document.querySelector('#dlg'); if (dlg && typeof dlg.showModal === 'function') { dlg.showModal(); } }<button on:click="openDialog">Open</button><dialog id="dlg">Hi</dialog>`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `data-spl-on-click="openDialog"`) {
		t.Fatalf("expected legacy named handler event, got %q", out)
	}
	if !strings.Contains(out, `"openDialog":"var dlg = document.querySelector('#dlg');`) || !strings.Contains(out, `"secure":false`) {
		t.Fatalf("expected legacy handler source in hydration payload, got %q", out)
	}
}
