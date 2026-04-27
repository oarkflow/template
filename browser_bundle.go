package template

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Bundle is a browser-loadable template registry.
type Bundle struct {
	Entry     string            `json:"entry"`
	Templates map[string]string `json:"templates"`
	Globals   map[string]any    `json:"globals,omitempty"`
}

type browserState struct {
	Entry    string
	Data     map[string]any
	Signals  map[string]any
	Handlers map[string]string
}

func cloneAnyMap(src map[string]any) map[string]any {
	if src == nil {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(src))
	for k, v := range src {
		cloned[k] = v
	}
	return cloned
}

func normalizeTemplatePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("template path is required")
	}
	cleanPath := filepath.Clean(path)
	if filepath.IsAbs(cleanPath) {
		return "", fmt.Errorf("absolute template paths are not allowed")
	}
	if cleanPath == ".." || strings.HasPrefix(cleanPath, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("template path escapes base directory")
	}
	return filepath.ToSlash(cleanPath), nil
}

// LoadBundle switches the engine to an in-memory template registry.
func (e *Engine) LoadBundle(bundle Bundle) error {
	templates := make(map[string]string, len(bundle.Templates))
	for name, content := range bundle.Templates {
		normalized, err := normalizeTemplatePath(name)
		if err != nil {
			return fmt.Errorf("bundle template %q: %w", name, err)
		}
		templates[normalized] = content
	}
	e.mu.Lock()
	e.bundle = &Bundle{
		Entry:     bundle.Entry,
		Templates: templates,
		Globals:   cloneAnyMap(bundle.Globals),
	}
	for k, v := range bundle.Globals {
		if _, exists := e.Globals[k]; !exists {
			e.Globals[k] = v
		}
	}
	e.fileCache = make(map[string][]Node)
	e.compiledFileCache = make(map[string]*compiledTemplate)
	e.mu.Unlock()
	return nil
}

// ExportBundle serializes every template file under BaseDir for browser use.
func (e *Engine) ExportBundle(entry string) (Bundle, error) {
	entry, err := normalizeTemplatePath(entry)
	if err != nil {
		return Bundle{}, err
	}
	baseDir := e.BaseDir
	if baseDir == "" {
		baseDir = "."
	}
	baseAbs, err := filepath.Abs(baseDir)
	if err != nil {
		return Bundle{}, fmt.Errorf("resolve base dir: %w", err)
	}
	templates := make(map[string]string)
	err = filepath.Walk(baseAbs, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info == nil || info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(baseAbs, path)
		if err != nil {
			return err
		}
		normalized := filepath.ToSlash(rel)
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		templates[normalized] = string(content)
		return nil
	})
	if err != nil {
		return Bundle{}, err
	}
	return Bundle{
		Entry:     entry,
		Templates: templates,
		Globals:   cloneAnyMap(e.Globals),
	}, nil
}

func (e *Engine) readTemplateSource(resolved string) ([]byte, error) {
	e.mu.RLock()
	bundle := e.bundle
	e.mu.RUnlock()
	if bundle != nil {
		content, ok := bundle.Templates[resolved]
		if !ok {
			return nil, fmt.Errorf("template not found in bundle: %s", resolved)
		}
		return []byte(content), nil
	}
	return os.ReadFile(resolved)
}

func (e *Engine) currentBrowserState() *browserState {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.browser
}

func (e *Engine) SetSignal(path string, value any) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("signal path is required")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.browser == nil {
		e.browser = &browserState{
			Data:     map[string]any{},
			Signals:  map[string]any{},
			Handlers: map[string]string{},
		}
	}
	if e.browser.Signals == nil {
		e.browser.Signals = make(map[string]any)
	}
	return setSignalPathValue(e.browser.Signals, path, value)
}

func (e *Engine) GetSignal(path string) (any, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.browser == nil {
		return nil, false
	}
	return getSignalPathValue(e.browser.Signals, path)
}

func (e *Engine) BrowserHandlers() map[string]string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.browser == nil {
		return map[string]string{}
	}
	return cloneAnyStringMap(e.browser.Handlers)
}

func (e *Engine) BrowserSignals() map[string]any {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.browser == nil {
		return map[string]any{}
	}
	return cloneAnyMap(e.browser.Signals)
}

func cloneAnyStringMap(src map[string]string) map[string]string {
	if src == nil {
		return map[string]string{}
	}
	cloned := make(map[string]string, len(src))
	for k, v := range src {
		cloned[k] = v
	}
	return cloned
}

func (e *Engine) RenderBrowserFile(path string, data map[string]any) (string, error) {
	resolved, err := e.resolvePath(path)
	if err != nil {
		return "", fmt.Errorf("template file error (%s): %w", path, err)
	}
	e.mu.Lock()
	if e.browser == nil {
		e.browser = &browserState{}
	}
	e.browser.Entry = resolved
	if data != nil || e.browser.Data == nil {
		e.browser.Data = cloneAnyMap(data)
	}
	if e.browser.Signals == nil {
		e.browser.Signals = make(map[string]any)
	}
	e.browser.Handlers = make(map[string]string)
	e.mu.Unlock()

	compiled, err := e.compileFileTemplate(resolved)
	if err != nil {
		return "", fmt.Errorf("template file error (%s): %w", path, err)
	}
	out, err := e.renderCompiled(compiled, e.currentBrowserState().Data, nil)
	if err != nil {
		return "", err
	}
	if err := e.ensureSecureRenderedHTML(out); err != nil {
		return "", err
	}
	return out, nil
}

func getSignalPathValue(signals map[string]any, path string) (any, bool) {
	parts := strings.Split(strings.TrimSpace(path), ".")
	if len(parts) == 0 || parts[0] == "" {
		return nil, false
	}
	current, ok := signals[parts[0]]
	if !ok {
		return nil, false
	}
	for _, part := range parts[1:] {
		obj, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = obj[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func setSignalPathValue(signals map[string]any, path string, value any) error {
	parts := strings.Split(strings.TrimSpace(path), ".")
	if len(parts) == 0 || parts[0] == "" {
		return fmt.Errorf("invalid signal path %q", path)
	}
	if len(parts) == 1 {
		signals[parts[0]] = value
		return nil
	}
	current, ok := signals[parts[0]]
	if !ok {
		current = map[string]any{}
		signals[parts[0]] = current
	}
	obj, ok := current.(map[string]any)
	if !ok {
		obj = map[string]any{}
		signals[parts[0]] = obj
	}
	for _, part := range parts[1 : len(parts)-1] {
		next, ok := obj[part]
		if !ok {
			child := map[string]any{}
			obj[part] = child
			obj = child
			continue
		}
		child, ok := next.(map[string]any)
		if !ok {
			child = map[string]any{}
			obj[part] = child
		}
		obj = child
	}
	obj[parts[len(parts)-1]] = value
	return nil
}
