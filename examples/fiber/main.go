// Package main demonstrates using the SPL template engine with GoFiber
// by implementing the fiber.Views interface.
//
// Run:
//
//	go run main.go
//
// Then visit http://localhost:3000
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/oarkflow/template"
)

// ─── Data types for template demonstration ───
// These demonstrate that SPL templates work with Go structs, typed slices,
// typed maps, and custom types — not just map[string]any.

type Country struct {
	Code   string
	Name   string
	Region string
}

type Role struct {
	Value       string
	Label       string
	Permissions []string
}

type Priority string

const (
	PriorityLow      Priority = "low"
	PriorityMedium   Priority = "medium"
	PriorityHigh     Priority = "high"
	PriorityCritical Priority = "critical"
)

type FormConfig struct {
	MaxBioLength int
	MinAge       int
	MaxAge       int
	AllowSignup  bool
}

// SPLViews implements fiber.Views using the SPL template engine.
type SPLViews struct {
	engine    *template.Engine
	directory string
	extension string
	reload    bool // re-read templates on every render (dev mode)
	ssr       bool // use SSR rendering with hydration for reactive features
}

// New creates an SPLViews engine rooted at directory.
// Extension defaults to ".html".
func New(directory string, extension ...string) *SPLViews {
	ext := ".html"
	if len(extension) > 0 && extension[0] != "" {
		ext = extension[0]
	}
	return &SPLViews{
		engine:    template.New(),
		directory: directory,
		extension: ext,
	}
}

// Reload enables reloading templates from disk on every render.
// Useful during development.
func (v *SPLViews) Reload(enabled bool) *SPLViews {
	v.reload = enabled
	return v
}

// SSR enables server-side rendering with hydration for reactive features
// (@signal, @reactive, @bind, @effect, on:click, etc.).
func (v *SPLViews) SSR(enabled bool) *SPLViews {
	v.ssr = enabled
	return v
}

// HydrationRuntimeURL configures the engine to load the SPL hydration runtime
// from an external URL instead of inlining it on every page.
func (v *SPLViews) HydrationRuntimeURL(url string) *SPLViews {
	v.engine.HydrationRuntimeURL = url
	return v
}

// Load walks the views directory and pre-parses all template files
// so the engine caches them for fast rendering.
// Called once by Fiber at startup.
func (v *SPLViews) Load() error {
	v.engine.BaseDir = v.directory
	v.engine.AutoEscape = true

	return filepath.Walk(v.directory, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if !strings.HasSuffix(path, v.extension) {
			return nil
		}
		// Warm the engine's file cache by rendering with nil data.
		// Errors are expected for templates that use @extends (they need
		// data at render time), so we just ignore them here.
		rel, _ := filepath.Rel(v.directory, path)
		_, _ = v.engine.RenderFile(rel, nil)
		return nil
	})
}

// Render renders the named template into the writer.
// binding is the template data (typically fiber.Map).
// layout args are joined as the layout path (SPL uses @extends in templates,
// so this is provided as a convenience override).
//
// This method satisfies the fiber.Views interface:
//
//	Render(io.Writer, string, interface{}, ...string) error
func (v *SPLViews) Render(w io.Writer, name string, binding any, layout ...string) error {
	if v.reload {
		v.engine.InvalidateCaches()
	}

	// Convert binding to map[string]any.
	data, ok := binding.(map[string]any)
	if !ok {
		if binding == nil {
			data = make(map[string]any)
		} else if fm, ok := binding.(fiber.Map); ok {
			data = fm
		} else {
			return fmt.Errorf("spl: binding must be map[string]any or fiber.Map, got %T", binding)
		}
	}

	// Merge engine globals into data.
	for k, val := range v.engine.Globals {
		if _, exists := data[k]; !exists {
			data[k] = val
		}
	}

	// Append extension if not present.
	if !strings.HasSuffix(name, v.extension) {
		name += v.extension
	}

	// If a layout was passed via Fiber's c.Render("view", data, "layouts/main"),
	// wrap the template content with @extends.
	if len(layout) > 0 && layout[0] != "" {
		layoutName := layout[0]
		if !strings.HasSuffix(layoutName, v.extension) {
			layoutName += v.extension
		}
		// Read the template file and prepend @extends.
		tmplPath := filepath.Join(v.directory, name)
		content, err := os.ReadFile(tmplPath)
		if err != nil {
			return fmt.Errorf("spl: read %s: %w", name, err)
		}
		wrapped := fmt.Sprintf("@extends(%q)\n%s", layoutName, string(content))
		var out string
		if v.ssr {
			out, err = v.engine.RenderSSR(wrapped, data)
		} else {
			out, err = v.engine.Render(wrapped, data)
		}
		if err != nil {
			return fmt.Errorf("spl: render %s with layout %s: %w", name, layoutName, err)
		}
		_, err = io.WriteString(w, out)
		return err
	}

	var out string
	var err error
	if v.ssr {
		out, err = v.engine.RenderSSRFile(name, data)
	} else {
		out, err = v.engine.RenderFile(name, data)
	}
	if err != nil {
		return fmt.Errorf("spl: render %s: %w", name, err)
	}
	_, err = io.WriteString(w, out)
	return err
}

func main() {
	exampleDir := currentExampleDir()
	viewsDir := filepath.Join(exampleDir, "views")
	staticDir := filepath.Join(exampleDir, "static")

	// Create the SPL template engine pointing at the views directory.
	engine := New(viewsDir)
	engine.Reload(true) // dev mode: re-read templates on each request
	engine.SSR(true)    // enable reactive hydration (@signal, @reactive, etc.)
	engine.engine.SecureMode = true

	// Serve the SPL hydration runtime as a cacheable static file.
	runtimeVersion := runtimeAssetVersion(engine.engine.RuntimeJS())
	engine.HydrationRuntimeURL("/static/spl-runtime.min.js?v=" + runtimeVersion)

	// Set global variables available in all templates.
	engine.engine.Globals["siteName"] = "SPL Fiber Demo"

	// Create Fiber app with SPL as the view engine.
	app := fiber.New(fiber.Config{
		Views: engine,
	})

	app.Use(func(c fiber.Ctx) error {
		c.Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'")
		c.Set("Referrer-Policy", "no-referrer")
		c.Set("X-Content-Type-Options", "nosniff")
		c.Set("X-Frame-Options", "DENY")
		c.Set("Permissions-Policy", "camera=(), geolocation=(), microphone=()")
		c.Set("Cross-Origin-Opener-Policy", "same-origin")
		return c.Next()
	})

	// Serve the SPL hydration runtime JS with aggressive caching.
	app.Get("/static/spl-runtime.min.js", func(c fiber.Ctx) error {
		c.Set("Content-Type", "application/javascript")
		c.Set("Cache-Control", "public, max-age=31536000, immutable")
		return c.SendString(engine.engine.RuntimeJS())
	})

	app.Get("/assets/browser-app.js", func(c fiber.Ctx) error {
		c.Set("Content-Type", "application/javascript")
		return c.SendFile(filepath.Join(staticDir, "browser-app.js"))
	})

	app.Get("/assets/wasm_exec.js", func(c fiber.Ctx) error {
		c.Set("Content-Type", "application/javascript")
		path, err := wasmExecAssetPath(staticDir)
		if err != nil {
			return c.Status(500).SendString(err.Error())
		}
		return c.SendFile(path)
	})

	app.Get("/assets/spl.wasm", func(c fiber.Ctx) error {
		wasmPath, compressed, err := wasmAssetPath(staticDir, templateRootDir())
		if err != nil {
			return c.Status(500).SendString(err.Error())
		}
		c.Set("Content-Type", "application/wasm")
		if compressed && strings.Contains(c.Get("Accept-Encoding"), "gzip") {
			c.Set("Content-Encoding", "gzip")
			c.Set("Vary", "Accept-Encoding")
		}
		return c.SendFile(wasmPath)
	})

	app.Get("/assets/spl-bundle.json", func(c fiber.Ctx) error {
		bundle, err := engine.engine.ExportBundle("index.html")
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(bundle)
	})

	// --- Routes ---

	app.Get("/", func(c fiber.Ctx) error {
		return c.Render("index", demoPageData())
	})

	app.Get("/demos", func(c fiber.Ctx) error {
		return c.Render("demos", demoCatalogData())
	})

	app.Get("/demo", func(c fiber.Ctx) error {
		return c.Redirect().To("/demos")
	})

	app.Get("/demo/product-launch", func(c fiber.Ctx) error {
		return c.Render("demo_product_launch", productLaunchPageData())
	})

	app.Get("/demo/ops-control", func(c fiber.Ctx) error {
		return c.Render("demo_ops_control", opsControlPageData())
	})

	app.Get("/demo/commerce-admin", func(c fiber.Ctx) error {
		return c.Render("demo_commerce_admin", commerceAdminPageData())
	})

	app.Get("/demo/support-hub", func(c fiber.Ctx) error {
		return c.Render("demo_support_hub", supportHubPageData())
	})

	app.Get("/browser", func(c fiber.Ctx) error {
		return c.Redirect().To("/browser/interactive")
	})

	app.Get("/browser/:page", func(c fiber.Ctx) error {
		page := c.Params("page")
		entry, data, ok := browserPage(page)
		if !ok {
			return fiber.ErrNotFound
		}

		bundle, err := browserBundleJSON(engine, entry)
		if err != nil {
			return c.Status(500).SendString(err.Error())
		}

		title, _ := data["title"].(string)
		c.Type("html")
		return c.SendString(browserShellHTML(title, page, entry, bundle))
	})

	app.Get("/api/browser/page-data", func(c fiber.Ctx) error {
		return c.JSON(demoPageData())
	})

	app.Get("/api/browser/page-data/:page", func(c fiber.Ctx) error {
		_, data, ok := browserPage(c.Params("page"))
		if !ok {
			return fiber.ErrNotFound
		}
		return c.JSON(data)
	})

	// --- API Endpoints for Forms page ---

	app.Post("/api/submit", func(c fiber.Ctx) error {
		var payload map[string]any
		if err := c.Bind().JSON(&payload); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "Invalid JSON", "success": false})
		}
		// Simulate server-side validation
		personal, _ := payload["personal"].(map[string]any)
		errors := []string{}
		if personal != nil {
			if email, _ := personal["email"].(string); email == "" {
				errors = append(errors, "Email is required")
			}
			if firstName, _ := personal["firstName"].(string); firstName == "" {
				errors = append(errors, "First name is required")
			}
		} else {
			errors = append(errors, "Personal information is missing")
		}
		if len(errors) > 0 {
			return c.Status(422).JSON(fiber.Map{
				"success": false,
				"errors":  errors,
				"message": "Validation failed",
			})
		}
		return c.JSON(fiber.Map{
			"success":   true,
			"message":   "Form submitted successfully!",
			"id":        fmt.Sprintf("SUB-%d", 1000+len(payload)),
			"timestamp": time.Now().Format(time.RFC3339),
		})
	})

	var quoteIdx int
	app.Get("/api/quote", func(c fiber.Ctx) error {
		quotes := []map[string]string{
			{"text": "The only way to do great work is to love what you do.", "author": "Steve Jobs"},
			{"text": "Code is like humor. When you have to explain it, it's bad.", "author": "Cory House"},
			{"text": "First, solve the problem. Then, write the code.", "author": "John Johnson"},
			{"text": "Simplicity is the soul of efficiency.", "author": "Austin Freeman"},
			{"text": "Make it work, make it right, make it fast.", "author": "Kent Beck"},
			{"text": "Talk is cheap. Show me the code.", "author": "Linus Torvalds"},
		}
		idx := quoteIdx % len(quotes)
		quoteIdx++
		return c.JSON(quotes[idx])
	})

	// --- TODO CRUD API (showcase tab) ---

	var (
		todoMu     sync.Mutex
		todos      []map[string]any
		todoNextID int
	)

	app.Get("/api/todos", func(c fiber.Ctx) error {
		todoMu.Lock()
		list := todos
		todoMu.Unlock()
		if list == nil {
			list = []map[string]any{}
		}
		return c.JSON(list)
	})

	app.Post("/api/todos", func(c fiber.Ctx) error {
		var form map[string]any
		if err := c.Bind().JSON(&form); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "Invalid JSON"})
		}
		todoMu.Lock()
		todoNextID++
		todo := map[string]any{
			"id":       todoNextID,
			"title":    form["title"],
			"priority": form["priority"],
			"notes":    form["notes"],
		}
		todos = append(todos, todo)
		list := make([]map[string]any, len(todos))
		copy(list, todos)
		todoMu.Unlock()
		return c.Status(201).JSON(list)
	})

	log.Println("SPL Fiber SSR demo listening on http://localhost:3000/")
	log.Println("SPL demo pages listening on http://localhost:3000/demos")
	log.Println("SPL Fiber browser/WASM demo listening on http://localhost:3000/browser/interactive")
	log.Fatal(app.Listen(":3000"))
}

func runtimeAssetVersion(src string) string {
	sum := sha256.Sum256([]byte(src))
	return hex.EncodeToString(sum[:8])
}

func navLinks() []fiber.Map {
	return []fiber.Map{
		{"key": "interactive", "label": "Interactive Demo", "href": "/"},
		{"key": "demos", "label": "Demo Pages", "href": "/demos"},
		{"key": "browser", "label": "Browser WASM Demo", "href": "/browser/interactive"},
	}
}

func demoRoutes() []fiber.Map {
	return []fiber.Map{
		{
			"slug":          "product-launch",
			"path":          "/demo/product-launch",
			"browserPath":   "/browser/product-launch",
			"title":         "Product Launch Landing",
			"category":      "Marketing",
			"summary":       "A full launch page for an AI workflow platform with proof points, pricing, roadmap, and customer stories.",
			"accent":        "#2563eb",
			"primaryStat":   "28-day launch sprint",
			"secondaryStat": "4.8x faster handoff",
		},
		{
			"slug":          "ops-control",
			"path":          "/demo/ops-control",
			"browserPath":   "/browser/ops-control",
			"title":         "Ops Control Center",
			"category":      "Operations",
			"summary":       "A logistics and fulfillment dashboard with incident tracking, shipment visibility, and automation health.",
			"accent":        "#0f766e",
			"primaryStat":   "98.4% on-time dispatch",
			"secondaryStat": "6 active lanes",
		},
		{
			"slug":          "commerce-admin",
			"path":          "/demo/commerce-admin",
			"browserPath":   "/browser/commerce-admin",
			"title":         "Commerce Admin Workspace",
			"category":      "Retail",
			"summary":       "A modern merchandising and order operations page with revenue, inventory, campaigns, and fulfillment focus.",
			"accent":        "#b45309",
			"primaryStat":   "$248K weekly GMV",
			"secondaryStat": "43 priority orders",
		},
		{
			"slug":          "support-hub",
			"path":          "/demo/support-hub",
			"browserPath":   "/browser/support-hub",
			"title":         "Support Hub Workspace",
			"category":      "Customer Success",
			"summary":       "A service desk view with live queues, SLA health, agent workload, macros, and escalation tracking.",
			"accent":        "#9333ea",
			"primaryStat":   "92 CSAT",
			"secondaryStat": "11m median first reply",
		},
	}
}

func withPageChrome(title, activePage string, data fiber.Map) fiber.Map {
	if data == nil {
		data = fiber.Map{}
	}
	data["title"] = title
	data["activePage"] = activePage
	data["navLinks"] = navLinks()
	data["demoRoutes"] = demoRoutes()
	return data
}

func demoPageData() fiber.Map {
	return withPageChrome("SPL Template Engine — Interactive Demo", "interactive", fiber.Map{
		"countries": []Country{
			{Code: "us", Name: "United States", Region: "Americas"},
			{Code: "uk", Name: "United Kingdom", Region: "Europe"},
			{Code: "ca", Name: "Canada", Region: "Americas"},
			{Code: "au", Name: "Australia", Region: "Oceania"},
			{Code: "de", Name: "Germany", Region: "Europe"},
			{Code: "fr", Name: "France", Region: "Europe"},
			{Code: "jp", Name: "Japan", Region: "Asia"},
			{Code: "in", Name: "India", Region: "Asia"},
			{Code: "br", Name: "Brazil", Region: "Americas"},
			{Code: "np", Name: "Nepal", Region: "Asia"},
		},
		"roles": []Role{
			{Value: "developer", Label: "Developer", Permissions: []string{"read", "write", "deploy"}},
			{Value: "designer", Label: "Designer", Permissions: []string{"read", "write"}},
			{Value: "manager", Label: "Project Manager", Permissions: []string{"read", "write", "admin"}},
			{Value: "devops", Label: "DevOps Engineer", Permissions: []string{"read", "write", "deploy", "admin"}},
			{Value: "qa", Label: "QA Engineer", Permissions: []string{"read", "write", "test"}},
			{Value: "admin", Label: "Administrator", Permissions: []string{"read", "write", "deploy", "admin", "super"}},
		},
		"priorities": []Priority{PriorityLow, PriorityMedium, PriorityHigh, PriorityCritical},
		"config": FormConfig{
			MaxBioLength: 280,
			MinAge:       0,
			MaxAge:       150,
			AllowSignup:  true,
		},
		"regionColors": map[string]string{
			"Americas": "#3b82f6",
			"Europe":   "#22c55e",
			"Asia":     "#f59e0b",
			"Oceania":  "#a855f7",
		},
	})
}

func demoCatalogData() fiber.Map {
	return withPageChrome("Real-World Demo Pages", "demos", fiber.Map{
		"overviewStats": []fiber.Map{
			{"label": "UI routes", "value": "4", "detail": "Distinct full-page examples"},
			{"label": "Render modes", "value": "2", "detail": "SSR and browser/WASM previews"},
			{"label": "Data models", "value": "20+", "detail": "Nested arrays, tables, and dashboard cards"},
			{"label": "Primary focus", "value": "Production-feel", "detail": "Layouts that look like real product surfaces"},
		},
	})
}

func productLaunchPageData() fiber.Map {
	return withPageChrome("NovaFlow Launch Story", "demos", fiber.Map{
		"heroKicker":  "Launch ready in 28 days",
		"heroSummary": "A polished SaaS launch page for a workflow product announcing an AI release, complete with proof, pricing, rollout sequencing, and customer validation.",
		"metrics": []fiber.Map{
			{"label": "Pilot teams", "value": "34", "detail": "Expanded from 6 design partners in five weeks"},
			{"label": "Workflow time saved", "value": "4.8x", "detail": "Average reduction in handoff coordination"},
			{"label": "Deploy confidence", "value": "99.2%", "detail": "Successful release rate across guided rollouts"},
			{"label": "Projected pipeline", "value": "$1.6M", "detail": "Qualified launch month expansion pipeline"},
		},
		"features": []fiber.Map{
			{"title": "AI-assisted runbooks", "copy": "Turn fragmented launch notes into reusable operating playbooks with approvals, owners, and delivery checkpoints."},
			{"title": "Live launch room", "copy": "Keep product, growth, support, and revenue teams on one timeline with shared blockers, risks, and go-live moments."},
			{"title": "Executive reporting", "copy": "Translate release progress into launch health, conversion lift, and activation readiness without exporting spreadsheets."},
		},
		"plans": []fiber.Map{
			{"name": "Starter", "price": "$39", "copy": "For new product teams validating one workflow.", "highlight": "Best for pilots"},
			{"name": "Growth", "price": "$129", "copy": "Unlimited launch rooms, automation templates, and stakeholder dashboards.", "highlight": "Most popular"},
			{"name": "Scale", "price": "Custom", "copy": "SSO, audit exports, launch governance, and rollout orchestration for larger orgs.", "highlight": "Enterprise"},
		},
		"roadmap": []fiber.Map{
			{"phase": "Week 1", "title": "Pilot migration", "copy": "Import launch docs, tag owners, and mirror your current process without retraining the team."},
			{"phase": "Week 2", "title": "Signal mapping", "copy": "Define conversion, release, and CS checkpoints so every department tracks the same launch health."},
			{"phase": "Week 3", "title": "Team rollout", "copy": "Enable review rituals, comms templates, and live blockers before launch week begins."},
			{"phase": "Week 4", "title": "Executive narrative", "copy": "Package the launch into one board for GTM, product, and leadership reviews."},
		},
		"testimonials": []fiber.Map{
			{"quote": "We replaced three launch trackers and finally stopped debating whose spreadsheet was correct.", "name": "Avery Kim", "role": "VP Product, Northstar Cloud"},
			{"quote": "The rollout room gave support and sales the same truth as engineering, which changed the first week outcome completely.", "name": "Mina Patel", "role": "Launch Director, FieldGrid"},
		},
		"launchChecklist": []fiber.Map{
			{"title": "Pre-launch review", "copy": "Creative, support, product, and analytics sign-off captured in one approval lane."},
			{"title": "Zero-day comms", "copy": "Internal updates, customer release notes, and in-app prompts ship from the same source of truth."},
			{"title": "Adoption watch", "copy": "Track activation, retention, and support spikes by segment during the first 72 hours."},
		},
	})
}

func opsControlPageData() fiber.Map {
	return withPageChrome("Northwind Ops Control", "demos", fiber.Map{
		"heroKicker":  "Morning command center",
		"heroSummary": "A real-world operations workspace for regional fulfillment teams balancing dispatch performance, lane incidents, and automation reliability.",
		"metrics": []fiber.Map{
			{"label": "On-time dispatch", "value": "98.4%", "detail": "Up 1.7 points week over week"},
			{"label": "Orders in wave", "value": "12,480", "detail": "Across six active fulfillment lanes"},
			{"label": "Exceptions open", "value": "19", "detail": "Seven need intervention before noon cut-off"},
			{"label": "Automation health", "value": "93%", "detail": "Two low-confidence retries in customs sync"},
		},
		"incidents": []fiber.Map{
			{"severity": "High", "title": "Kathmandu outbound lane backlog", "meta": "18 pallets waiting on seal verification", "owner": "Lhamo Sherpa"},
			{"severity": "Medium", "title": "Berlin sorter latency", "meta": "Rule engine fallback increased label print time by 9 minutes", "owner": "Tariq Hussain"},
			{"severity": "Low", "title": "Sydney replenishment variance", "meta": "Cycle count mismatch for 42 units in zone C7", "owner": "Jess Walker"},
		},
		"shipments": []fiber.Map{
			{"id": "NW-20481", "route": "Kathmandu → Delhi", "status": "Seal check", "eta": "09:45", "load": "92%"},
			{"id": "NW-20477", "route": "Berlin → Paris", "status": "In transit", "eta": "10:10", "load": "77%"},
			{"id": "NW-20463", "route": "Chicago → Denver", "status": "Sorting", "eta": "11:05", "load": "61%"},
			{"id": "NW-20459", "route": "Sydney → Melbourne", "status": "Ready to dispatch", "eta": "08:30", "load": "88%"},
		},
		"regions": []fiber.Map{
			{"name": "APAC cross-border", "throughput": "4,100 orders", "detail": "Highest volume, customs automation at 91% confidence"},
			{"name": "EU priority retail", "throughput": "3,220 orders", "detail": "Store replenishment window closes at 14:00 CET"},
			{"name": "US direct-to-consumer", "throughput": "5,160 orders", "detail": "Carrier mix optimized for same-day handoff"},
		},
		"team": []fiber.Map{
			{"name": "Lhamo Sherpa", "role": "Regional Ops Lead", "status": "On floor", "focus": "APAC backlog clearance"},
			{"name": "Mason Webb", "role": "Automation Analyst", "status": "Reviewing", "focus": "Customs sync retries"},
			{"name": "Tariq Hussain", "role": "Warehouse Systems", "status": "Escalated", "focus": "Berlin sorter latency"},
			{"name": "Camila Soto", "role": "Carrier Success", "status": "Monitoring", "focus": "D2C cut-off coverage"},
		},
		"automations": []fiber.Map{
			{"title": "Wave planner", "copy": "Successfully grouped 8,940 orders into optimized pick waves with 14% less travel distance."},
			{"title": "Exception triage", "copy": "Auto-classified 72 barcode failures and routed only 9 to manual inspection."},
			{"title": "Carrier allocation", "copy": "Rebalanced service levels after a weather advisory without missing promised delivery dates."},
		},
	})
}

func commerceAdminPageData() fiber.Map {
	return withPageChrome("Meridian Commerce Admin", "demos", fiber.Map{
		"heroKicker":  "Merchandising + fulfillment",
		"heroSummary": "An admin surface for a modern commerce team managing weekly revenue, order risk, inventory turns, and live campaign sequencing.",
		"metrics": []fiber.Map{
			{"label": "Weekly GMV", "value": "$248K", "detail": "18% above forecast after spring drop launch"},
			{"label": "AOV", "value": "$92", "detail": "Bundled kits outperforming single-SKU purchases"},
			{"label": "At-risk orders", "value": "43", "detail": "Payment review and address mismatches"},
			{"label": "Sell-through", "value": "71%", "detail": "Top capsule collection pacing ahead of plan"},
		},
		"orders": []fiber.Map{
			{"id": "MC-88012", "customer": "Rina K.", "channel": "Shop App", "status": "Fraud review", "value": "$286"},
			{"id": "MC-88007", "customer": "Jordan W.", "channel": "Web", "status": "Packing", "value": "$148"},
			{"id": "MC-87994", "customer": "Natalie S.", "channel": "Instagram", "status": "Awaiting stock", "value": "$204"},
			{"id": "MC-87973", "customer": "Kai R.", "channel": "Retail partner", "status": "Shipped", "value": "$512"},
		},
		"products": []fiber.Map{
			{"name": "Transit Weekender", "tag": "Best margin", "detail": "42% contribution margin with restock due Friday", "inventory": "128 units"},
			{"name": "Studio Layer Tee", "tag": "High velocity", "detail": "Colorway B is the top attach item in the bundle builder", "inventory": "342 units"},
			{"name": "Capsule Travel Kit", "tag": "Campaign hero", "detail": "Landing page conversion up 23% after creator UGC refresh", "inventory": "76 units"},
		},
		"campaigns": []fiber.Map{
			{"name": "Spring capsule drop", "window": "Today · 14:00", "detail": "Email, SMS, and paid social aligned with low-inventory safeguards"},
			{"name": "VIP replenishment alert", "window": "Tomorrow · 09:30", "detail": "Segment of repeat buyers waiting on premium weekender restock"},
			{"name": "Creator bundle push", "window": "Thu · 16:00", "detail": "Bundle landing page refreshed with creator wardrobe picks"},
		},
		"funnel": []fiber.Map{
			{"step": "Sessions", "value": "88K", "detail": "Traffic led by creators and direct brand search"},
			{"step": "Product views", "value": "34K", "detail": "Highest engagement on capsule and travel categories"},
			{"step": "Checkout starts", "value": "6.1K", "detail": "Free shipping threshold still the strongest lever"},
			{"step": "Orders", "value": "2.7K", "detail": "Bundle attach rate rose to 31%"},
		},
		"restocks": []fiber.Map{
			{"title": "Transit Weekender", "copy": "Approve expedited freight if preorders exceed 180 units by Wednesday."},
			{"title": "Studio Layer Tee", "copy": "Shift 60 units from retail reserve to DTC if creator launch outpaces forecast."},
			{"title": "Travel Kit accessories", "copy": "Lock vendor packaging confirmation before Friday photo refresh."},
		},
	})
}

func supportHubPageData() fiber.Map {
	return withPageChrome("Atlas Support Hub", "demos", fiber.Map{
		"heroKicker":  "Customer success desk",
		"heroSummary": "A support operations workspace showing queue health, team workload, reusable macros, and the conversation paths that matter most right now.",
		"metrics": []fiber.Map{
			{"label": "CSAT", "value": "92", "detail": "Steady despite elevated billing volume"},
			{"label": "Median first reply", "value": "11m", "detail": "Down from 18m last Monday"},
			{"label": "SLA risk", "value": "14", "detail": "Mostly enterprise billing and SSO incidents"},
			{"label": "Resolved today", "value": "186", "detail": "44 by automation and macro-assisted flows"},
		},
		"queues": []fiber.Map{
			{"priority": "Urgent", "subject": "SSO login loop after domain migration", "meta": "Enterprise · 8 affected admins · 6m to breach", "owner": "Priya B."},
			{"priority": "High", "subject": "Invoice export missing tax breakdown", "meta": "Finance lead · escalated from chat · 14m to breach", "owner": "Marco F."},
			{"priority": "Medium", "subject": "Workflow approval stuck in pending state", "meta": "Growth team workspace · reproducible in Chrome", "owner": "Aisha T."},
			{"priority": "Low", "subject": "Need template for onboarding reminders", "meta": "Self-serve customer · routed from help center", "owner": "Automation"},
		},
		"macros": []fiber.Map{
			{"name": "Billing export recovery", "detail": "Explains tax field refresh timing and links the CSV reconciliation guide."},
			{"name": "SSO triage path", "detail": "Collects identity provider details, captures HAR data, and sets escalation context for engineering."},
			{"name": "Workflow stuck diagnostic", "detail": "Gathers approval history, browser state, and affected workflow IDs in one response."},
		},
		"agents": []fiber.Map{
			{"name": "Priya B.", "role": "Enterprise Specialist", "workload": "9 open", "focus": "SSO + provisioning"},
			{"name": "Marco F.", "role": "Billing Support", "workload": "7 open", "focus": "Invoicing and tax questions"},
			{"name": "Aisha T.", "role": "Product Support", "workload": "11 open", "focus": "Workflow investigation"},
			{"name": "Nora K.", "role": "Automation Manager", "workload": "5 queues", "focus": "Macro performance"},
		},
		"escalations": []fiber.Map{
			{"time": "08:40", "title": "Engineering engaged on SSO redirect regression", "copy": "Temporary workaround shared with three enterprise accounts while IdP cookie handling is patched."},
			{"time": "09:05", "title": "Finance ops reviewing invoice rendering mismatch", "copy": "Export job logs show a schema drift introduced during the weekend warehouse sync."},
			{"time": "09:28", "title": "Help center article promoted to top result", "copy": "Workflow approval troubleshooting content now intercepts 27% more self-serve searches."},
		},
		"playbook": []fiber.Map{
			{"title": "Protect response time", "copy": "Route repeat billing questions into assisted macros before human assignment."},
			{"title": "Escalate with context", "copy": "Attach workspace ID, browser details, and reproducible steps before engineering handoff."},
			{"title": "Close the loop", "copy": "Revisit high-impact tickets with a follow-up survey once the fix is confirmed."},
		},
	})
}

func browserPage(page string) (string, fiber.Map, bool) {
	switch page {
	case "", "interactive":
		return "index.html", demoPageData(), true
	case "demos":
		return "demos.html", demoCatalogData(), true
	case "product-launch":
		return "demo_product_launch.html", productLaunchPageData(), true
	case "ops-control":
		return "demo_ops_control.html", opsControlPageData(), true
	case "commerce-admin":
		return "demo_commerce_admin.html", commerceAdminPageData(), true
	case "support-hub":
		return "demo_support_hub.html", supportHubPageData(), true
	default:
		return "", nil, false
	}
}

func browserBundleJSON(engine *SPLViews, entry string) (string, error) {
	bundle, err := engine.engine.ExportBundle(entry)
	if err != nil {
		return "", fmt.Errorf("export bundle %s: %w", entry, err)
	}
	encoded, err := json.Marshal(bundle)
	if err != nil {
		return "", fmt.Errorf("marshal bundle %s: %w", entry, err)
	}
	return string(encoded), nil
}

func browserShellHTML(title, page, entry, bundleJSON string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>%s - SPL Browser Renderer</title>
    <style>
        html, body { margin: 0; padding: 0; background: linear-gradient(180deg, #f8fbff 0%%, #eef4fb 100%%); color: #1e293b; font-family: "Avenir Next", "Segoe UI", sans-serif; }
        .browser-note {
            position: fixed;
            right: 1rem;
            bottom: 1rem;
            z-index: 10;
            padding: 0.75rem 0.9rem;
            border-radius: 0.85rem;
            border: 1px solid rgba(148, 163, 184, 0.2);
            background: rgba(255, 255, 255, 0.88);
            color: #475569;
            box-shadow: 0 14px 30px rgba(15, 23, 42, 0.08);
            font-size: 0.78rem;
        }
    </style>
</head>
<body>
    <div id="app" data-spl-entry="%s" data-spl-bundle-id="spl-browser-bundle" data-spl-data-url="/api/browser/page-data/%s"></div>
    <script id="spl-browser-bundle" type="application/json">%s</script>
    <script src="/assets/wasm_exec.js"></script>
    <script src="/assets/browser-app.js"></script>
    <div class="browser-note">Browser bundle is embedded in the page. Beyond static assets, runtime fetches only live page data.</div>
</body>
</html>`, title, entry, page, bundleJSON)
}

func currentExampleDir() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "."
	}
	return filepath.Dir(file)
}

func templateRootDir() string {
	return filepath.Clean(filepath.Join(currentExampleDir(), "../.."))
}

var wasmBuild struct {
	mu   sync.Mutex
	path string
}

func generatedAssetDir(staticDir string) string {
	return filepath.Join(staticDir, "generated")
}

func wasmExecAssetPath(staticDir string) (string, error) {
	generated := filepath.Join(generatedAssetDir(staticDir), "wasm_exec.js")
	if _, err := os.Stat(generated); err == nil {
		return generated, nil
	}
	return filepath.Join(runtime.GOROOT(), "lib", "wasm", "wasm_exec.js"), nil
}

func wasmAssetPath(staticDir, rootDir string) (string, bool, error) {
	generatedDir := generatedAssetDir(staticDir)
	compressed := filepath.Join(generatedDir, "spl.wasm.gz")
	if _, err := os.Stat(compressed); err == nil {
		return compressed, true, nil
	}
	generated := filepath.Join(generatedDir, "spl.wasm")
	if _, err := os.Stat(generated); err == nil {
		return generated, false, nil
	}
	path, err := ensureWASMAsset(rootDir)
	return path, false, err
}

func ensureWASMAsset(rootDir string) (string, error) {
	wasmBuild.mu.Lock()
	defer wasmBuild.mu.Unlock()
	if wasmBuild.path != "" {
		if _, err := os.Stat(wasmBuild.path); err == nil {
			return wasmBuild.path, nil
		}
	}
	outPath := filepath.Join(os.TempDir(), "spl-browser-renderer.wasm")
	cmd := exec.Command("go", "build", "-ldflags", "-s -w", "-o", outPath, "./cmd/splwasm")
	cmd.Dir = rootDir
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("build spl.wasm: %w\n%s", err, strings.TrimSpace(string(output)))
	}
	wasmBuild.path = outPath
	return outPath, nil
}
