package template

import (
	"io"
	"strings"
	"sync"

	"github.com/oarkflow/interpreter"
)

type runtimeAdapter struct {
	engine *Engine
}

func init() {
	interpreter.RegisterTemplateRuntimeFactory(func(baseDir string) interpreter.TemplateRuntime {
		engine := New()
		if strings.TrimSpace(baseDir) != "" {
			engine.BaseDir = baseDir
		}
		return &runtimeAdapter{engine: engine}
	})
	interpreter.RegisterHotReloadHook(invalidateHotReloadCaches)
}

func (r *runtimeAdapter) Render(tmpl string, data map[string]any) (string, error) {
	return r.engine.Render(tmpl, data)
}

func (r *runtimeAdapter) RenderFile(path string, data map[string]any) (string, error) {
	return r.engine.RenderFile(path, data)
}

func (r *runtimeAdapter) RenderSSR(tmpl string, data map[string]any) (string, error) {
	return r.engine.RenderSSR(tmpl, data)
}

func (r *runtimeAdapter) RenderSSRFile(path string, data map[string]any) (string, error) {
	return r.engine.RenderSSRFile(path, data)
}

func (r *runtimeAdapter) RenderStream(w io.Writer, tmpl string, data map[string]any) error {
	return NewStreamRenderer(r.engine, data).RenderStream(w, tmpl)
}

func (r *runtimeAdapter) RenderStreamFile(w io.Writer, path string, data map[string]any) error {
	return r.engine.RenderStreamFile(w, path, data)
}

func (r *runtimeAdapter) InvalidateCaches() {
	r.engine.InvalidateCaches()
}

var cacheRegistry struct {
	mu      sync.Mutex
	engines []*Engine
}

func invalidateHotReloadCaches(path string) {
	_ = path
	cacheRegistry.mu.Lock()
	engines := append([]*Engine(nil), cacheRegistry.engines...)
	cacheRegistry.mu.Unlock()
	for _, engine := range engines {
		if engine != nil {
			engine.InvalidateCaches()
		}
	}
}
