package main

import (
	"strings"
	"testing"
)

func demoRenderData() map[string]any {
	return map[string]any{
		"title": "SPL Template Engine - Interactive Demo",
		"countries": []Country{
			{Code: "us", Name: "United States", Region: "Americas"},
			{Code: "uk", Name: "United Kingdom", Region: "Europe"},
			{Code: "ca", Name: "Canada", Region: "Americas"},
		},
		"roles": []Role{
			{Value: "developer", Label: "Developer", Permissions: []string{"read", "write", "deploy"}},
			{Value: "designer", Label: "Designer", Permissions: []string{"read", "write"}},
		},
		"priorities": []Priority{PriorityLow, PriorityMedium, PriorityHigh},
		"config": FormConfig{
			MaxBioLength: 280,
			MinAge:       0,
			MaxAge:       150,
			AllowSignup:  true,
		},
		"regionColors": map[string]string{
			"Americas": "#3b82f6",
			"Europe":   "#22c55e",
		},
	}
}

func TestFiberDemoRendersContent(t *testing.T) {
	engine := New("./views")
	engine.SSR(true)
	engine.engine.Globals["siteName"] = "SPL Fiber Demo"
	if err := engine.Load(); err != nil {
		t.Fatalf("load views: %v", err)
	}

	var out strings.Builder
	if err := engine.Render(&out, "index", demoRenderData()); err != nil {
		t.Fatalf("render index: %v", err)
	}

	html := out.String()
	if !strings.Contains(html, "Personal Information") {
		t.Fatalf("expected rendered tab content, got %q", html)
	}
	if !strings.Contains(html, "Inspirational Quote") {
		t.Fatalf("expected demo section content, got %q", html)
	}
	if !strings.Contains(html, "data-spl-hydration") {
		t.Fatalf("expected hydration payload, got %q", html)
	}
}

func TestFiberDemoRendersContentInSecureMode(t *testing.T) {
	engine := New("./views")
	engine.SSR(true)
	engine.engine.Globals["siteName"] = "SPL Fiber Demo"
	engine.engine.SecureMode = true
	if err := engine.Load(); err != nil {
		t.Fatalf("load views: %v", err)
	}

	var out strings.Builder
	if err := engine.Render(&out, "index", demoRenderData()); err != nil {
		t.Fatalf("render index in secure mode: %v", err)
	}

	html := out.String()
	if !strings.Contains(html, "data-spl-hydration") {
		t.Fatalf("expected hydration payload in secure mode, got %q", html)
	}
}
