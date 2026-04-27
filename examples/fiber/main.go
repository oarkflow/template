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

	app.Get("/browser", func(c fiber.Ctx) error {
		c.Type("html")
		return c.SendString(browserShellHTML())
	})

	app.Get("/api/browser/page-data", func(c fiber.Ctx) error {
		return c.JSON(demoPageData())
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

	log.Println("SPL Fiber demo listening on http://localhost:3000")
	log.Fatal(app.Listen(":3000"))
}

func runtimeAssetVersion(src string) string {
	sum := sha256.Sum256([]byte(src))
	return hex.EncodeToString(sum[:8])
}

func demoPageData() fiber.Map {
	return fiber.Map{
		"title": "SPL Template Engine — Interactive Demo",
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
	}
}

func browserShellHTML() string {
	return `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>SPL Browser Renderer</title>
    <style>
        html, body { margin: 0; padding: 0; background: #f8fafc; color: #1e293b; font-family: system-ui, -apple-system, sans-serif; }
    </style>
</head>
<body>
    <div id="app" data-spl-entry="index.html" data-spl-bundle-url="/assets/spl-bundle.json" data-spl-data-url="/api/browser/page-data"></div>
    <script src="/assets/wasm_exec.js"></script>
    <script src="/assets/browser-app.js"></script>
</body>
</html>`
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
