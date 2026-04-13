package template

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
)

// ── Stream/Defer/Lazy Directive Nodes ──────────────────────────────

type StreamNode struct {
	Body []Node
	ID   string
}

func (n *StreamNode) nodeType() string { return "stream" }

type DeferNode struct {
	Body     []Node
	ID       string
	Fallback []Node
}

func (n *DeferNode) nodeType() string { return "defer" }

type LazyNode struct {
	Expr     string
	Body     []Node
	Fallback []Node
}

func (n *LazyNode) nodeType() string { return "lazy" }

// ── Flusher interface ──────────────────────────────────────────────

type writerFlusher interface {
	Flush()
}

// ── StreamRenderer ─────────────────────────────────────────────────

type StreamChunk struct {
	ID      string
	Content string
	IsDefer bool
	Error   error
}

type StreamRenderer struct {
	engine  *Engine
	data    map[string]any
	writer  io.Writer
	mu      sync.Mutex
	deferID int
}

func NewStreamRenderer(engine *Engine, data map[string]any) *StreamRenderer {
	return &StreamRenderer{
		engine: engine,
		data:   data,
	}
}

// RenderStream renders template nodes to an io.Writer, streaming chunks.
// Deferred sections are rendered after the main content is flushed.
func (sr *StreamRenderer) RenderStream(w io.Writer, tmpl string) error {
	nodes, err := parse(tmpl)
	if err != nil {
		return fmt.Errorf("template parse error: %w", err)
	}
	return sr.RenderStreamNodes(w, nodes)
}

// RenderStreamNodes renders already-parsed nodes to a writer.
func (sr *StreamRenderer) RenderStreamNodes(w io.Writer, nodes []Node) error {
	sr.writer = w
	renderer := sr.engine.cloneForRender(nil, cloneComponentDefs(sr.engine.Components))

	var deferred []DeferNode
	var mainContent strings.Builder

	for _, node := range nodes {
		switch n := node.(type) {
		case *DeferNode:
			sr.deferID++
			id := n.ID
			if id == "" {
				id = fmt.Sprintf("spl-defer-%d", sr.deferID)
			}
			if len(n.Fallback) > 0 {
				fallback, err := renderer.renderNodes(n.Fallback, sr.data, 0)
				if err != nil {
					fallback = "Loading..."
				}
				fmt.Fprintf(&mainContent, `<div id="%s">%s</div>`, id, fallback)
			} else {
				fmt.Fprintf(&mainContent, `<div id="%s"><span class="spl-loading">Loading...</span></div>`, id)
			}
			deferred = append(deferred, DeferNode{Body: n.Body, ID: id, Fallback: n.Fallback})
		case *StreamNode:
			rendered, err := renderer.renderNodes(n.Body, sr.data, 0)
			if err != nil {
				return err
			}
			mainContent.WriteString(rendered)
			// Flush after stream node
			sr.mu.Lock()
			_, writeErr := w.Write([]byte(mainContent.String()))
			sr.mu.Unlock()
			if writeErr != nil {
				return writeErr
			}
			mainContent.Reset()
			if flusher, ok := w.(writerFlusher); ok {
				flusher.Flush()
			}
		default:
			rendered, err := renderer.renderNodes([]Node{node}, sr.data, 0)
			if err != nil {
				return err
			}
			mainContent.WriteString(rendered)
		}
	}

	// Write remaining main content
	sr.mu.Lock()
	_, err := w.Write([]byte(mainContent.String()))
	sr.mu.Unlock()
	if err != nil {
		return err
	}
	if flusher, ok := w.(writerFlusher); ok {
		flusher.Flush()
	}

	// Render deferred sections
	if len(deferred) > 0 {
		var wg sync.WaitGroup
		for _, d := range deferred {
			wg.Add(1)
			go func(def DeferNode) {
				defer wg.Done()
				deferRenderer := sr.engine.cloneForRender(nil, cloneComponentDefs(renderer.Components))
				content, err := deferRenderer.renderNodes(def.Body, sr.data, 0)
				if err != nil {
					content = fmt.Sprintf(`<span class="spl-error">Error: %s</span>`, err.Error())
				}
				script := fmt.Sprintf(
					`<script>document.getElementById('%s').innerHTML=%s;</script>`,
					def.ID,
					jsonEscapeStr(content),
				)
				sr.mu.Lock()
				w.Write([]byte(script))
				sr.mu.Unlock()
				if flusher, ok := w.(writerFlusher); ok {
					flusher.Flush()
				}
			}(d)
		}
		wg.Wait()
	}

	return nil
}

// RenderStreamToString renders with streaming semantics but returns a string.
func (sr *StreamRenderer) RenderStreamToString(tmpl string) (string, error) {
	var buf strings.Builder
	err := sr.RenderStream(&buf, tmpl)
	return buf.String(), err
}

// jsonEscapeStr safely escapes a string for use inside a JavaScript string literal
// within an HTML <script> tag. Uses json.Marshal for correctness.
func jsonEscapeStr(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		// Fallback: double-quote with basic escaping
		s = strings.ReplaceAll(s, "\\", "\\\\")
		s = strings.ReplaceAll(s, "\"", "\\\"")
		s = strings.ReplaceAll(s, "\n", "\\n")
		s = strings.ReplaceAll(s, "\r", "\\r")
		return "\"" + s + "\""
	}
	// json.Marshal produces a valid JS string literal with proper escaping
	return string(b)
}
