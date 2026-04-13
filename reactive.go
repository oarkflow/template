package template

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"

	"github.com/oarkflow/interpreter"
)

type SignalNode struct {
	Name        string
	InitialExpr string
}

func (n *SignalNode) nodeType() string { return "signal" }

type EffectNode struct {
	Body []Node
	Deps []string
}

func (n *EffectNode) nodeType() string { return "effect" }

type ReactiveViewNode struct {
	Body []Node
	Deps []string
}

func (n *ReactiveViewNode) nodeType() string { return "reactive" }

type BindNode struct {
	Attr    string
	Signal  string
	Element string
}

func (n *BindNode) nodeType() string { return "bind" }

type ClickNode struct {
	Label  string
	Signal string
	Action string
	Value  string
}

func (n *ClickNode) nodeType() string { return "click" }

type hydrationState struct {
	Signals   map[string]any
	Handlers  map[string]string
	Effects   []hydrationEffect
	Views     []hydrationView
	BindID    int
	EffectsID int
	ViewID    int
	NeedsBoot bool
}

type hydrationEffect struct {
	Selector string
	Source   string
	Deps     []string
}

type hydrationView struct {
	Selector string
	Source   string
	Deps     []string
}

type SSRRenderer struct {
	engine *Engine
	data   map[string]any
}

func NewSSRRenderer(engine *Engine, data map[string]any) *SSRRenderer {
	return &SSRRenderer{engine: engine, data: data}
}

func (ssr *SSRRenderer) RenderSSR(tmpl string) (string, error) {
	return ssr.engine.RenderSSR(tmpl, ssr.data)
}

func (e *Engine) RenderSSR(tmpl string, data map[string]any) (string, error) {
	state := &hydrationState{Signals: make(map[string]any)}
	compiled, err := e.compileStringTemplate(tmpl)
	if err != nil {
		return "", fmt.Errorf("template parse error: %w", err)
	}
	renderer := e.cloneForRender(state, cloneComponentDefs(e.Components))
	out, err := renderer.renderCompiled(compiled, data, state)
	if err != nil {
		return "", err
	}
	return out + renderer.renderHydrationScript(out), nil
}

func (e *Engine) renderHydrationScript(renderedHTML string) string {
	if e.hydration == nil || !e.hydration.NeedsBoot {
		return ""
	}
	payload := map[string]any{
		"signals":  e.hydration.Signals,
		"handlers": e.hydration.Handlers,
		"effects":  e.hydration.Effects,
		"views":    e.hydration.Views,
	}
	encoded, _ := json.Marshal(payload)

	var sb strings.Builder

	// 1. Runtime: external URL or inline with tree-shaken + obfuscated code.
	if e.HydrationRuntimeURL != "" {
		// External mode: full runtime served separately (all features).
		sb.WriteString(`<script src="`)
		sb.WriteString(e.HydrationRuntimeURL)
		sb.WriteString(`"></script>`)
	} else {
		// Inline mode: detect features per-page for tree-shaking.
		features := detectFeatures(renderedHTML, e.hydration.Effects, e.hydration.Views)
		// Apply global flags.
		if e.DisableAPI {
			features &^= featAPI
		}
		if !e.DisableDebug {
			features |= featDebug
		}
		runtime := getObfuscatedForFeatures(features, e.DisableDebug)
		sb.WriteString(`<script data-spl-runtime>if(!window.__SPL_RT__){window.__SPL_RT__=1;`)
		sb.WriteString(runtime)
		sb.WriteString(`}</script>`)
	}

	// 2. Per-page payload + bootstrap (always inline, unique per page).
	fmt.Fprintf(&sb, `<script data-spl-hydration>(function(){var payload=%s;`, string(encoded))
	sb.WriteString(obfuscatedBootstrap)
	sb.WriteString(`})();</script>`)

	return sb.String()
}

// validIdentRe matches safe JavaScript identifier names.
var validIdentRe = regexp.MustCompile(`^[a-zA-Z_$][a-zA-Z0-9_$]*$`)

// reservedJSNames are names that could cause prototype pollution or other JS issues.
var reservedJSNames = map[string]bool{
	"__proto__": true, "constructor": true, "prototype": true,
	"__defineGetter__": true, "__defineSetter__": true,
	"__lookupGetter__": true, "__lookupSetter__": true,
}

// isValidIdentName validates a signal or handler name for safe use in JavaScript.
func isValidIdentName(name string) bool {
	return name != "" && validIdentRe.MatchString(name) && !reservedJSNames[name]
}

func (e *Engine) registerSignal(name string, value interpreter.Object) {
	if e.hydration == nil {
		return
	}
	if !isValidIdentName(name) {
		return // silently skip invalid signal names
	}
	e.hydration.Signals[name] = objectToNative(value)
	e.hydration.NeedsBoot = true
}

func (e *Engine) registerHandler(name, expr string) {
	if e.hydration == nil {
		return
	}
	if !isValidIdentName(name) {
		return // silently skip invalid handler names
	}
	if e.hydration.Handlers == nil {
		e.hydration.Handlers = make(map[string]string)
	}
	e.hydration.Handlers[name] = strings.TrimSpace(expr)
	e.hydration.NeedsBoot = true
}

func (e *Engine) nextBindID() int {
	if e.hydration == nil {
		return 0
	}
	e.hydration.BindID++
	return e.hydration.BindID
}

func (e *Engine) nextEffectSelector() string {
	if e.hydration == nil {
		return ""
	}
	e.hydration.EffectsID++
	return fmt.Sprintf(`[data-spl-effect="%d"]`, e.hydration.EffectsID)
}

func (e *Engine) nextViewSelector() string {
	if e.hydration == nil {
		return ""
	}
	e.hydration.ViewID++
	return fmt.Sprintf(`[data-spl-view="%d"]`, e.hydration.ViewID)
}

func (e *Engine) trackEffectHTML(selector, source string, deps []string) {
	if e.hydration == nil {
		return
	}
	e.hydration.Effects = append(e.hydration.Effects, hydrationEffect{Selector: selector, Source: source, Deps: uniqueDeps(deps)})
	e.hydration.NeedsBoot = true
}

func (e *Engine) trackViewHTML(selector, source string, deps []string) {
	if e.hydration == nil {
		return
	}
	e.hydration.Views = append(e.hydration.Views, hydrationView{Selector: selector, Source: source, Deps: uniqueDeps(deps)})
	e.hydration.NeedsBoot = true
}

func uniqueDeps(deps []string) []string {
	uniq := make([]string, 0, len(deps))
	seen := make(map[string]struct{})
	for _, dep := range deps {
		dep = strings.TrimSpace(dep)
		if dep == "" {
			continue
		}
		if _, ok := seen[dep]; ok {
			continue
		}
		seen[dep] = struct{}{}
		uniq = append(uniq, dep)
	}
	return uniq
}

func WrapWithHydration(htmlStr, signalName, attr string, id int) string {
	return fmt.Sprintf(`<span data-spl-bind="%s" data-spl-attr="%s" data-spl-id="%d">%s</span>`, html.EscapeString(signalName), html.EscapeString(attr), id, htmlStr)
}

func WrapReactiveView(htmlStr string, id int) string {
	return fmt.Sprintf(`<div data-spl-view="%d">%s</div>`, id, htmlStr)
}

func WrapClickAction(label string, signalName string, action string, value string) string {
	stmt := "toggle(" + signalName + ")"
	if action == "inc" {
		stmt = signalName + " += " + value
	} else if action == "set" {
		stmt = signalName + " = " + value
	}
	return fmt.Sprintf(`<button type="button" data-spl-on-click="%s">%s</button>`, html.EscapeString(stmt), label)
}

func signalPlaceholder(name string) string {
	return "__SPL_SIGNAL__" + name + "__"
}

func isSimpleSignalExpr(expr string) (string, bool) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return "", false
	}
	// First char must be a letter or underscore (not digit)
	ch := expr[0]
	if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_') {
		return "", false
	}
	for i := 1; i < len(expr); i++ {
		ch = expr[i]
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_') {
			return "", false
		}
	}
	return expr, true
}

func compileHydrationHTML(raw string) string {
	return transformReactiveAttributes(raw)
}

func signalValueForAttr(attr string, obj interpreter.Object) string {
	if attr == "html" {
		return objectToString(obj)
	}
	return html.EscapeString(objectToString(obj))
}

func toJSValue(val any) string {
	b, err := json.Marshal(val)
	if err != nil {
		return strconv.Quote(fmt.Sprintf("%v", val))
	}
	return string(b)
}

var onEventPattern = regexp.MustCompile(`on:([a-zA-Z][a-zA-Z0-9_-]*)(\.[a-zA-Z][a-zA-Z0-9_-]*)*\s*=\s*"([^"]*)"`)
var bindAttrPattern = regexp.MustCompile(`bind:([a-zA-Z_][a-zA-Z0-9_]*)\s*=\s*"([^"]*)"`)

func transformReactiveAttributes(raw string) string {
	out := onEventPattern.ReplaceAllStringFunc(raw, func(match string) string {
		parts := onEventPattern.FindStringSubmatch(match)
		if len(parts) != 4 {
			return match
		}
		eventName := parts[1]
		modsText := strings.TrimPrefix(parts[2], ".")
		expr := html.EscapeString(parts[3])
		if modsText == "" {
			return fmt.Sprintf(`data-spl-on-%s="%s"`, eventName, expr)
		}
		mods := strings.ReplaceAll(modsText, ".", ",")
		return fmt.Sprintf(`data-spl-on-%s="%s" data-spl-on-%s-mods="%s"`, eventName, expr, eventName, html.EscapeString(mods))
	})
	out = bindAttrPattern.ReplaceAllString(out, `data-spl-bind-$1="$2"`)
	return out
}
