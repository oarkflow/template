package template

import (
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/oarkflow/interpreter"
)

// Cache size limits to prevent unbounded memory growth.
const (
	maxExprCacheSize         = 10000 // parsed expression ASTs
	maxExprMetaCacheSize     = 10000 // fast-path expression metadata
	maxFileCacheSize         = 1000  // parsed template files
	maxTmplCacheSize         = 500   // parsed template strings
	maxCompiledFileCacheSize = 500   // compiled file templates
	maxCompiledTextCacheSize = 500   // compiled text templates
)

// evictRandom removes ~20% of entries from a map by random sampling.
// Called under write lock. Uses random eviction for O(1) amortized cost.
func evictMap[K comparable, V any](m map[K]V, maxSize int) {
	if len(m) <= maxSize {
		return
	}
	toEvict := len(m) / 5 // remove 20%
	if toEvict < 1 {
		toEvict = 1
	}
	evicted := 0
	for k := range m {
		if evicted >= toEvict {
			break
		}
		// random skip to avoid always evicting the same iteration-order entries
		if rand.IntN(3) != 0 {
			continue
		}
		delete(m, k)
		evicted++
	}
	// if we didn't evict enough (unlucky random), do a second pass
	for k := range m {
		if evicted >= toEvict || len(m) <= maxSize {
			break
		}
		delete(m, k)
		evicted++
	}
}

// Filter transforms a value into a string. Extra positional args may be passed from the template syntax.
type Filter func(value any, args ...string) string

// exprKind classifies an expression for fast-path evaluation.
type exprKind int

const (
	exprGeneric   exprKind = iota // requires full interpreter.Eval
	exprIdent                     // simple identifier: ${name}
	exprDot                       // single dot access: ${item.name}
	exprStringLit                 // string literal: ${"hello"}
	exprIntLit                    // integer literal: ${42}
	exprBoolTrue                  // true
	exprBoolFalse                 // false
	exprConstHash                 // constant hash literal: {"key": "val", ...}
)

// exprFastPath holds pre-analyzed metadata for fast expression evaluation.
type exprFastPath struct {
	kind        exprKind
	ident       string              // for exprIdent: the variable name; for exprDot: the left identifier
	field       string              // for exprDot: the field name
	strVal      string              // for exprStringLit
	intVal      int64               // for exprIntLit
	constResult interpreter.Object  // for exprConstHash: cached evaluation result (cloned on use)
}

// componentDef holds a registered component's body and declared props.
type componentDef struct {
	Body  []Node
	Props []PropDef // declared prop definitions (may be empty)
}

type compiledTemplate struct {
	Nodes      []Node
	Components map[string]componentDef
	Imports    []string
}

// Engine is the main entry point for rendering SPL templates.
type Engine struct {
	BaseDir           string                          // directory for resolving includes/layouts
	Filters           map[string]Filter               // registered filters
	Globals           map[string]any                  // global template variables merged into every render
	AutoEscape        bool                            // auto HTML-escape ${} output (default: true)
	MaxDepth          int                             // max include/layout nesting depth (default: 64)
	Components        map[string]componentDef         // registered reusable components
	slotStack         []*slotContext                  // stack for nested component slot contexts
	watchState        map[string]string               // @watch: expr → last evaluated value string
	exprCache         map[string]*interpreter.Program // cached parsed expression ASTs
	fileCache         map[string][]Node               // cached parsed template files by resolved path
	tmplCache         map[string][]Node               // cached parsed template strings
	compiledFileCache map[string]*compiledTemplate
	compiledTextCache map[string]*compiledTemplate
	baseEnv           *interpreter.Environment // base environment for the current render call
	globalEnv         *interpreter.Environment // cached global environment (created once)
	hydration           *hydrationState          // SSR hydration state for the current render call
	HydrationRuntimeURL string                   // if set, emit <script src="..."> instead of inlining runtime
	DisableDebug        bool                     // exclude debug/getRenderStats from hydration runtime
	DisableAPI          bool                     // exclude API integration (patchAPI, serializeForm, apiParse) from hydration runtime
	mu                  *sync.RWMutex

	// Fast-path expression metadata cache
	exprMeta map[string]exprFastPath // expression string → fast-path info
}

// New creates a new Engine with sensible defaults.
func New() *Engine {
	e := &Engine{
		BaseDir:           ".",
		Filters:           make(map[string]Filter),
		Globals:           make(map[string]any),
		AutoEscape:        true,
		MaxDepth:          64,
		Components:        make(map[string]componentDef),
		exprCache:         make(map[string]*interpreter.Program),
		exprMeta:          make(map[string]exprFastPath),
		fileCache:         make(map[string][]Node),
		tmplCache:         make(map[string][]Node),
		compiledFileCache: make(map[string]*compiledTemplate),
		compiledTextCache: make(map[string]*compiledTemplate),
		mu:                &sync.RWMutex{},
	}
	cacheRegistry.mu.Lock()
	cacheRegistry.engines = append(cacheRegistry.engines, e)
	cacheRegistry.mu.Unlock()
	registerBuiltinFilters(e)
	return e
}

// RegisterFilter adds or replaces a named filter.
func (e *Engine) RegisterFilter(name string, fn Filter) {
	e.mu.Lock()
	e.Filters[name] = fn
	e.mu.Unlock()
}

// RegisterComponent parses a component body and registers it by name.
func (e *Engine) RegisterComponent(name string, body string) error {
	nodes, err := parse(body)
	if err != nil {
		return fmt.Errorf("component %q parse error: %w", name, err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Components[name] = componentDef{Body: nodes}
	return nil
}

// newGlobalEnv returns a cached global environment for template rendering.
// Thread-safe: uses RWMutex to protect lazy initialization.
func (e *Engine) newGlobalEnv() *interpreter.Environment {
	e.mu.RLock()
	env := e.globalEnv
	e.mu.RUnlock()
	if env != nil {
		return env
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.globalEnv == nil {
		e.globalEnv = interpreter.NewGlobalEnvironment([]string{})
	}
	return e.globalEnv
}

// Render parses and renders a template string with the given data.
func (e *Engine) Render(tmpl string, data map[string]any) (string, error) {
	compiled, err := e.compileStringTemplate(tmpl)
	if err != nil {
		return "", fmt.Errorf("template parse error: %w", err)
	}
	return e.renderCompiled(compiled, data, e.hydration)
}

// RenderFile loads a template file relative to BaseDir and renders it.
func (e *Engine) RenderFile(path string, data map[string]any) (string, error) {
	resolved := e.resolvePath(path)
	compiled, err := e.compileFileTemplate(resolved)
	if err != nil {
		return "", fmt.Errorf("template file error (%s): %w", path, err)
	}
	return e.renderCompiled(compiled, data, e.hydration)
}

// RenderSSRFile renders a template file and injects hydration metadata.
func (e *Engine) RenderSSRFile(path string, data map[string]any) (string, error) {
	resolved := e.resolvePath(path)
	compiled, err := e.compileFileTemplate(resolved)
	if err != nil {
		return "", fmt.Errorf("template file error (%s): %w", path, err)
	}
	state := &hydrationState{Signals: make(map[string]any)}
	renderer := e.cloneForRender(state, cloneComponentDefs(e.Components))
	out, err := renderer.renderCompiled(compiled, data, state)
	if err != nil {
		return "", err
	}
	return out + renderer.renderHydrationScript(out), nil
}

// RenderStreamFile streams a parsed template file to a writer.
func (e *Engine) RenderStreamFile(w io.Writer, path string, data map[string]any) error {
	resolved := e.resolvePath(path)
	compiled, err := e.compileFileTemplate(resolved)
	if err != nil {
		return fmt.Errorf("template file error (%s): %w", path, err)
	}
	return NewStreamRenderer(e, data).RenderStreamNodes(w, compiled.Nodes)
}

// InvalidateCaches clears parsed template caches so subsequent renders re-read source.
func (e *Engine) InvalidateCaches() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.fileCache = make(map[string][]Node)
	e.tmplCache = make(map[string][]Node)
	e.compiledFileCache = make(map[string]*compiledTemplate)
	e.compiledTextCache = make(map[string]*compiledTemplate)
	e.watchState = make(map[string]string)
}

// loadFile reads and parses a template file, using the file cache.
func (e *Engine) loadFile(resolved string) ([]Node, error) {
	e.mu.RLock()
	if nodes, ok := e.fileCache[resolved]; ok {
		e.mu.RUnlock()
		return nodes, nil
	}
	e.mu.RUnlock()
	content, err := os.ReadFile(resolved)
	if err != nil {
		return nil, err
	}
	nodes, err := parse(string(content))
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	evictMap(e.fileCache, maxFileCacheSize)
	e.fileCache[resolved] = nodes
	e.mu.Unlock()
	return nodes, nil
}

func (e *Engine) cloneForRender(state *hydrationState, components map[string]componentDef) *Engine {
	globalEnv := e.newGlobalEnv()
	cloned := *e
	cloned.Components = components
	cloned.slotStack = nil
	cloned.watchState = nil // lazy-init in renderWatch
	cloned.baseEnv = interpreter.NewEnclosedEnvironment(globalEnv)
	cloned.hydration = state
	return &cloned
}

func cloneComponentDefs(src map[string]componentDef) map[string]componentDef {
	cloned := make(map[string]componentDef, len(src))
	for k, v := range src {
		cloned[k] = v
	}
	return cloned
}

func (e *Engine) compileStringTemplate(tmpl string) (*compiledTemplate, error) {
	e.mu.RLock()
	if ct, ok := e.compiledTextCache[tmpl]; ok {
		e.mu.RUnlock()
		return ct, nil
	}
	e.mu.RUnlock()
	nodes, err := parse(tmpl)
	if err != nil {
		return nil, err
	}
	ct := e.buildCompiledTemplate(nodes)
	e.mu.Lock()
	if existing, ok := e.compiledTextCache[tmpl]; ok {
		e.mu.Unlock()
		return existing, nil
	}
	evictMap(e.tmplCache, maxTmplCacheSize)
	e.tmplCache[tmpl] = nodes
	evictMap(e.compiledTextCache, maxCompiledTextCacheSize)
	e.compiledTextCache[tmpl] = ct
	e.mu.Unlock()
	return ct, nil
}

func (e *Engine) compileFileTemplate(resolved string) (*compiledTemplate, error) {
	e.mu.RLock()
	if ct, ok := e.compiledFileCache[resolved]; ok {
		e.mu.RUnlock()
		return ct, nil
	}
	e.mu.RUnlock()
	nodes, err := e.loadFile(resolved)
	if err != nil {
		return nil, err
	}
	ct := e.buildCompiledTemplate(nodes)
	e.mu.Lock()
	if existing, ok := e.compiledFileCache[resolved]; ok {
		e.mu.Unlock()
		return existing, nil
	}
	evictMap(e.compiledFileCache, maxCompiledFileCacheSize)
	e.compiledFileCache[resolved] = ct
	e.mu.Unlock()
	return ct, nil
}

func (e *Engine) buildCompiledTemplate(nodes []Node) *compiledTemplate {
	components := make(map[string]componentDef)
	imports := make([]string, 0)
	for _, n := range nodes {
		if c, ok := n.(*ComponentNode); ok {
			components[c.Name] = componentDef{Body: c.Body, Props: c.Props}
			continue
		}
		if imp, ok := n.(*ImportNode); ok {
			imports = append(imports, imp.Path)
		}
	}
	return &compiledTemplate{Nodes: nodes, Components: components, Imports: imports}
}

func (e *Engine) renderCompiled(ct *compiledTemplate, data map[string]any, state *hydrationState) (string, error) {
	// Build components: start from engine's registered components, merge template's
	e.mu.RLock()
	engineCompCount := len(e.Components)
	e.mu.RUnlock()

	var components map[string]componentDef
	if engineCompCount == 0 && len(ct.Components) == 0 && len(ct.Imports) == 0 {
		// Fast path: no components anywhere — avoid map allocation entirely
		components = nil
	} else {
		components = make(map[string]componentDef, engineCompCount+len(ct.Components))
		e.mu.RLock()
		for k, v := range e.Components {
			components[k] = v
		}
		e.mu.RUnlock()

		if err := e.registerImportedComponents(ct.Imports, components, make(map[string]struct{})); err != nil {
			return "", err
		}
		for k, v := range ct.Components {
			components[k] = v
		}
	}

	// Create renderer directly — single environment creation, single clone
	globalEnv := e.newGlobalEnv()
	cloned := *e
	if components != nil {
		cloned.Components = components
	}
	cloned.slotStack = nil
	cloned.watchState = nil // lazy-init on first use
	cloned.baseEnv = interpreter.NewEnclosedEnvironment(globalEnv)
	cloned.hydration = state
	return (&cloned).renderNodes(ct.Nodes, data, 0)
}

func (e *Engine) registerImportedComponents(imports []string, components map[string]componentDef, seen map[string]struct{}) error {
	for _, path := range imports {
		resolved := e.resolvePath(path)
		if _, ok := seen[resolved]; ok {
			continue
		}
		seen[resolved] = struct{}{}
		ct, err := e.compileFileTemplate(resolved)
		if err != nil {
			return fmt.Errorf("import %s: %w", path, err)
		}
		if err := e.registerImportedComponents(ct.Imports, components, seen); err != nil {
			return err
		}
		for k, v := range ct.Components {
			components[k] = v
		}
	}
	return nil
}

func (e *Engine) resolvePath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(e.BaseDir, path)
}

// mergeData returns a new map with globals as defaults, overridden by data.
func (e *Engine) mergeData(data map[string]any) map[string]any {
	merged := make(map[string]any, len(e.Globals)+len(data))
	for k, v := range e.Globals {
		merged[k] = v
	}
	for k, v := range data {
		merged[k] = v
	}
	return merged
}

// analyzeExpr pre-analyzes a parsed program to determine if it can use a fast path.
func analyzeExpr(program *interpreter.Program) exprFastPath {
	if len(program.Statements) != 1 {
		return exprFastPath{kind: exprGeneric}
	}
	stmt, ok := program.Statements[0].(*interpreter.ExpressionStatement)
	if !ok {
		return exprFastPath{kind: exprGeneric}
	}
	return classifyExpr(stmt.Expression)
}

func classifyExpr(expr interpreter.Expression) exprFastPath {
	switch v := expr.(type) {
	case *interpreter.Identifier:
		return exprFastPath{kind: exprIdent, ident: v.Name}
	case *interpreter.DotExpression:
		if left, ok := v.Left.(*interpreter.Identifier); ok {
			return exprFastPath{kind: exprDot, ident: left.Name, field: v.Right.Name}
		}
	case *interpreter.StringLiteral:
		return exprFastPath{kind: exprStringLit, strVal: v.Value}
	case *interpreter.IntegerLiteral:
		return exprFastPath{kind: exprIntLit, intVal: v.Value}
	case *interpreter.BooleanLiteral:
		if v.Value {
			return exprFastPath{kind: exprBoolTrue}
		}
		return exprFastPath{kind: exprBoolFalse}
	case *interpreter.HashLiteral:
		if isConstHashLiteral(v) {
			return exprFastPath{kind: exprConstHash}
		}
	}
	return exprFastPath{kind: exprGeneric}
}

// isConstExpr returns true if the expression contains only literal values (no variable references).
func isConstExpr(expr interpreter.Expression) bool {
	switch v := expr.(type) {
	case *interpreter.StringLiteral, *interpreter.IntegerLiteral,
		*interpreter.FloatLiteral, *interpreter.BooleanLiteral,
		*interpreter.NullLiteral:
		return true
	case *interpreter.HashLiteral:
		return isConstHashLiteral(v)
	case *interpreter.ArrayLiteral:
		for _, el := range v.Elements {
			if !isConstExpr(el) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// isConstHashLiteral returns true if a hash literal contains only literal keys and values.
func isConstHashLiteral(h *interpreter.HashLiteral) bool {
	for _, entry := range h.Entries {
		if entry.IsSpread {
			return false
		}
		if !isConstExpr(entry.Key) || !isConstExpr(entry.Value) {
			return false
		}
	}
	return true
}

// evalExpr evaluates an SPL expression string against the given environment.
func (e *Engine) evalExpr(expr string, env *interpreter.Environment) (interpreter.Object, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, fmt.Errorf("empty expression")
	}
	return e.evalExprTrimmed(expr, env)
}

// evalExprTrimmed evaluates a pre-trimmed expression. Used internally to avoid redundant TrimSpace.
func (e *Engine) evalExprTrimmed(expr string, env *interpreter.Environment) (interpreter.Object, error) {
	// Combined cache lookup — single lock acquisition for both caches
	e.mu.RLock()
	meta, metaOK := e.exprMeta[expr]
	program, progOK := e.exprCache[expr]
	e.mu.RUnlock()

	if metaOK {
		switch meta.kind {
		case exprIdent:
			if obj, ok := env.Get(meta.ident); ok {
				return obj, nil
			}
			return nil, fmt.Errorf("expression eval error: identifier not found: %s", meta.ident)
		case exprDot:
			leftObj, ok := env.Get(meta.ident)
			if !ok {
				return nil, fmt.Errorf("expression eval error: identifier not found: %s", meta.ident)
			}
			return fastDotAccess(leftObj, meta.field)
		case exprStringLit:
			return &interpreter.String{Value: meta.strVal}, nil
		case exprIntLit:
			return &interpreter.Integer{Value: meta.intVal}, nil
		case exprBoolTrue:
			return interpreter.TRUE, nil
		case exprBoolFalse:
			return interpreter.FALSE, nil
		case exprConstHash:
			if meta.constResult != nil {
				if h, ok := meta.constResult.(*interpreter.Hash); ok {
					return cloneHash(h), nil
				}
				return meta.constResult, nil
			}
		}
		// exprGeneric: fall through to full eval
	}

	if !progOK {
		l := interpreter.NewLexer(expr)
		p := interpreter.NewParser(l)
		program = p.ParseProgram()
		if errs := p.Errors(); len(errs) > 0 {
			return nil, fmt.Errorf("expression parse error: %s", strings.Join(errs, "; "))
		}
		e.mu.Lock()
		evictMap(e.exprCache, maxExprCacheSize)
		e.exprCache[expr] = program

		// Analyze for fast path
		meta = analyzeExpr(program)
		evictMap(e.exprMeta, maxExprMetaCacheSize)
		e.exprMeta[expr] = meta
		e.mu.Unlock()

		// Try fast path on first encounter too
		switch meta.kind {
		case exprIdent:
			if obj, ok := env.Get(meta.ident); ok {
				return obj, nil
			}
			return nil, fmt.Errorf("expression eval error: identifier not found: %s", meta.ident)
		case exprDot:
			leftObj, ok := env.Get(meta.ident)
			if !ok {
				return nil, fmt.Errorf("expression eval error: identifier not found: %s", meta.ident)
			}
			return fastDotAccess(leftObj, meta.field)
		case exprStringLit:
			return &interpreter.String{Value: meta.strVal}, nil
		case exprIntLit:
			return &interpreter.Integer{Value: meta.intVal}, nil
		case exprBoolTrue:
			return interpreter.TRUE, nil
		case exprBoolFalse:
			return interpreter.FALSE, nil
		case exprConstHash:
			// Evaluate once and cache the result for future calls
			result := interpreter.Eval(program, env)
			if result != nil && result.Type() == interpreter.ERROR_OBJ {
				return nil, fmt.Errorf("expression eval error: %s", result.Inspect())
			}
			meta.constResult = result
			e.mu.Lock()
			e.exprMeta[expr] = meta
			e.mu.Unlock()
			// Return a clone since the caller may mutate (e.g., adding default props)
			if h, ok := result.(*interpreter.Hash); ok {
				return cloneHash(h), nil
			}
			return result, nil
		}
	}

	// Materialize any lazyHash values in the environment before passing to interpreter.Eval,
	// which doesn't know about our lazy wrapper type.
	materializeLazyHashes(env)
	result := interpreter.Eval(program, env)
	if result != nil && result.Type() == interpreter.ERROR_OBJ {
		return nil, fmt.Errorf("expression eval error: %s", result.Inspect())
	}
	return result, nil
}

// materializeLazyHashes converts any lazyHash values in the environment's store
// to real interpreter.Hash objects. Called before passing to interpreter.Eval which
// doesn't know about the lazy wrapper type.
func materializeLazyHashes(env *interpreter.Environment) {
	for k, v := range env.Store {
		if lh, ok := v.(*lazyHash); ok {
			env.Store[k] = lh.materialize()
		}
	}
}

// hashKeyCache caches computed HashKey values for field names to avoid repeated allocations.
var hashKeyCache sync.Map // string → interpreter.HashKey

func cachedHashKey(field string) interpreter.HashKey {
	if v, ok := hashKeyCache.Load(field); ok {
		return v.(interpreter.HashKey)
	}
	key := &interpreter.String{Value: field}
	hk := key.HashKey()
	hashKeyCache.Store(field, hk)
	return hk
}

// fastDotAccess performs a direct field lookup on an object without going through interpreter.Eval.
func fastDotAccess(obj interpreter.Object, field string) (interpreter.Object, error) {
	switch v := obj.(type) {
	case *interpreter.Hash:
		hk := cachedHashKey(field)
		if pair, exists := v.Pairs[hk]; exists {
			return pair.Value, nil
		}
		return interpreter.NULL, nil
	case *lazyHash:
		if val, exists := v.data[field]; exists {
			return toLazyObject(val), nil
		}
		return interpreter.NULL, nil
	}
	// For non-hash types, data in template context is always Hash or lazyHash
	return interpreter.NULL, nil
}
