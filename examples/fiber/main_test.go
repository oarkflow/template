package main

import (
	"strings"
	"testing"
)

func demoRenderData() map[string]any {
	return demoPageData()
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

func TestAdditionalDemoPagesRender(t *testing.T) {
	engine := New("./views")
	engine.SSR(true)
	engine.engine.Globals["siteName"] = "SPL Fiber Demo"
	if err := engine.Load(); err != nil {
		t.Fatalf("load views: %v", err)
	}

	tests := []struct {
		name     string
		template string
		data     map[string]any
		want     string
	}{
		{name: "catalog", template: "demos", data: demoCatalogData(), want: "Different pages, complete UI"},
		{name: "launch", template: "demo_product_launch", data: productLaunchPageData(), want: "NovaFlow helps cross-functional teams"},
		{name: "ops", template: "demo_ops_control", data: opsControlPageData(), want: "Shipment visibility board"},
		{name: "commerce", template: "demo_commerce_admin", data: commerceAdminPageData(), want: "Order risk board"},
		{name: "support", template: "demo_support_hub", data: supportHubPageData(), want: "Atlas Support keeps SLAs visible"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var out strings.Builder
			if err := engine.Render(&out, tc.template, tc.data); err != nil {
				t.Fatalf("render %s: %v", tc.template, err)
			}
			if !strings.Contains(out.String(), tc.want) {
				t.Fatalf("expected %q in rendered %s", tc.want, tc.template)
			}
		})
	}
}

func TestBrowserShellEmbedsBundle(t *testing.T) {
	html := browserShellHTML("Demo", "interactive", "index.html", `{"entry":"index.html"}`)
	if strings.Contains(html, "/assets/spl-bundle.json") {
		t.Fatalf("expected inline bundle shell to avoid bundle fetch endpoint")
	}
	if !strings.Contains(html, `data-spl-bundle-id="spl-browser-bundle"`) {
		t.Fatalf("expected inline bundle marker, got %q", html)
	}
	if !strings.Contains(html, `/api/browser/page-data/interactive`) {
		t.Fatalf("expected page data endpoint, got %q", html)
	}
}
