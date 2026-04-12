package template

import (
	"fmt"
	"html"
	"sort"
	"strings"

	"github.com/oarkflow/interpreter"
)

// slotContext holds the slot fills for the current component render.
type slotContext struct {
	fills    map[string]string // named slot → rendered content
	children string            // default slot content
}

// emptyHash is a shared empty hash to avoid allocations.
var emptyHash = &interpreter.Hash{Pairs: map[interpreter.HashKey]interpreter.HashPair{}}

// renderNodes renders a slice of nodes into a string.
func (e *Engine) renderNodes(nodes []Node, data map[string]any, depth int) (string, error) {
	if depth > e.MaxDepth {
		return "", fmt.Errorf("max include depth (%d) exceeded", e.MaxDepth)
	}

	// Check for @extends — if present, do layout rendering
	var extendsPath string
	defines := make(map[string][]Node)
	var regularNodes []Node

	for _, n := range nodes {
		switch v := n.(type) {
		case *ExtendsNode:
			extendsPath = v.Path
		case *ImportNode:
			continue
		case *DefineNode:
			defines[v.Name] = v.Body
		case *ComponentNode:
			e.mu.Lock()
			e.Components[v.Name] = componentDef{Body: v.Body, Props: v.Props}
			e.mu.Unlock()
		default:
			regularNodes = append(regularNodes, n)
		}
	}

	if extendsPath != "" {
		return e.renderLayout(extendsPath, defines, data, depth)
	}

	// Use enclosed environment from the base env instead of creating a new global env
	var merged map[string]any
	if len(e.Globals) > 0 {
		merged = e.mergeData(data)
	} else if data != nil {
		merged = data
	} else {
		merged = make(map[string]any)
	}
	env := interpreter.NewEnclosedEnvironment(e.baseEnv)
	for k, v := range merged {
		env.Set(k, nativeToObject(v))
	}

	var buf strings.Builder
	for _, n := range regularNodes {
		s, err := e.renderNode(n, env, data, depth)
		if err != nil {
			return "", err
		}
		buf.WriteString(s)
	}
	return buf.String(), nil
}

// renderLayout handles @extends with @define blocks.
func (e *Engine) renderLayout(layoutPath string, defines map[string][]Node, data map[string]any, depth int) (string, error) {
	resolved := e.resolvePath(layoutPath)
	layoutNodes, err := e.loadFile(resolved)
	if err != nil {
		return "", fmt.Errorf("layout file error: %w", err)
	}

	var merged map[string]any
	if len(e.Globals) > 0 {
		merged = e.mergeData(data)
	} else if data != nil {
		merged = data
	} else {
		merged = make(map[string]any)
	}
	env := interpreter.NewEnclosedEnvironment(e.baseEnv)
	for k, v := range merged {
		env.Set(k, nativeToObject(v))
	}

	var buf strings.Builder
	for _, n := range layoutNodes {
		if block, ok := n.(*BlockNode); ok {
			if defined, exists := defines[block.Name]; exists {
				// Pre-pass: collect component definitions from the define block
				for _, dn := range defined {
					if comp, ok := dn.(*ComponentNode); ok {
						e.mu.Lock()
						e.Components[comp.Name] = componentDef{Body: comp.Body, Props: comp.Props}
						e.mu.Unlock()
					}
				}
				for _, dn := range defined {
					s, err := e.renderNode(dn, env, data, depth+1)
					if err != nil {
						return "", err
					}
					buf.WriteString(s)
				}
			} else {
				for _, bn := range block.Body {
					s, err := e.renderNode(bn, env, data, depth+1)
					if err != nil {
						return "", err
					}
					buf.WriteString(s)
				}
			}
		} else {
			s, err := e.renderNode(n, env, data, depth+1)
			if err != nil {
				return "", err
			}
			buf.WriteString(s)
		}
	}
	return buf.String(), nil
}

// renderNode renders a single node.
func (e *Engine) renderNode(n Node, env *interpreter.Environment, data map[string]any, depth int) (string, error) {
	switch v := n.(type) {
	case *TextNode:
		if e.hydration != nil {
			return transformReactiveAttributes(v.Text), nil
		}
		return v.Text, nil

	case *ExprNode:
		return e.renderExpr(v, env)

	case *IfNode:
		return e.renderIf(v, env, data, depth)

	case *ForNode:
		return e.renderFor(v, env, data, depth)

	case *SwitchNode:
		return e.renderSwitch(v, env, data, depth)

	case *MatchNode:
		return e.renderMatch(v, env, data, depth)

	case *RawNode:
		return v.Text, nil

	case *IncludeNode:
		return e.renderInclude(v, env, data, depth)

	case *ImportNode:
		return e.renderImport(v, env, data, depth)

	case *HandlerNode:
		return e.renderHandler(v, env, data, depth)

	case *BlockNode:
		// When encountered outside of layout context, just render the default body
		var buf strings.Builder
		for _, child := range v.Body {
			s, err := e.renderNode(child, env, data, depth)
			if err != nil {
				return "", err
			}
			buf.WriteString(s)
		}
		return buf.String(), nil

	case *RenderNode:
		return e.renderRender(v, env, data, depth)

	case *SlotNode:
		return e.renderSlot(v)

	case *ComponentNode:
		// Already collected in renderNodes; skip
		return "", nil

	case *FillNode:
		// Handled by renderRender; skip if encountered outside
		return "", nil

	case *LetNode:
		return e.renderLet(v, env, data, depth)

	case *ComputedNode:
		return e.renderComputed(v, env, data, depth)

	case *WatchNode:
		return e.renderWatch(v, env, data, depth)

	case *SignalNode:
		return e.renderSignal(v, env, data, depth)

	case *BindNode:
		return e.renderBind(v, env, data, depth)

	case *EffectNode:
		return e.renderEffect(v, env, data, depth)

	case *ReactiveViewNode:
		return e.renderReactiveView(v, env, data, depth)

	case *ClickNode:
		return e.renderClick(v, env, data, depth)

	case *StreamNode:
		return e.renderBody(v.Body, env, data, depth)

	case *DeferNode:
		if len(v.Fallback) > 0 {
			return e.renderBody(v.Fallback, env, data, depth)
		}
		return e.renderBody(v.Body, env, data, depth)

	case *LazyNode:
		obj, err := e.evalExpr(v.Expr, env)
		if err != nil {
			return "", fmt.Errorf("@lazy(%s): %w", v.Expr, err)
		}
		if interpreter.IsTruthy(obj) {
			return e.renderBody(v.Body, env, data, depth)
		}
		if len(v.Fallback) > 0 {
			return e.renderBody(v.Fallback, env, data, depth)
		}
		return "", nil

	default:
		return "", fmt.Errorf("unknown node type: %T", n)
	}
}

func (e *Engine) renderExpr(n *ExprNode, env *interpreter.Environment) (string, error) {
	obj, err := e.evalExpr(n.Expr, env)
	if err != nil {
		return "", fmt.Errorf("expression ${%s}: %w", n.Expr, err)
	}

	// Fast path: no filters, string result — very common in templates
	if len(n.Filters) == 0 {
		if str, ok := obj.(*interpreter.String); ok {
			if e.AutoEscape && !n.Raw {
				return html.EscapeString(str.Value), nil
			}
			return str.Value, nil
		}
	}

	result := objectToString(obj)

	// Apply filters
	for _, fc := range n.Filters {
		filterFn, ok := e.Filters[fc.Name]
		if !ok {
			return "", fmt.Errorf("unknown filter: %s", fc.Name)
		}
		result = filterFn(result, fc.Args...)
	}

	// Auto-escape unless raw
	if e.AutoEscape && !n.Raw {
		result = html.EscapeString(result)
	}

	return result, nil
}

func (e *Engine) renderIf(n *IfNode, env *interpreter.Environment, data map[string]any, depth int) (string, error) {
	if e.hydration != nil && len(n.Branches) == 1 && n.Else != nil {
		if signalName, ok := isSimpleSignalExpr(n.Branches[0].Cond); ok {
			if current, exists := env.Get(signalName); exists {
				whenTrue, err := e.renderBody(n.Branches[0].Body, env, data, depth)
				if err != nil {
					return "", err
				}
				whenFalse, err := e.renderBody(n.Else, env, data, depth)
				if err != nil {
					return "", err
				}
				if interpreter.IsTruthy(current) {
					return fmt.Sprintf(`<div data-spl-if="%s">%s</div><div data-spl-else="%s" style="display:none">%s</div>`, html.EscapeString(signalName), whenTrue, html.EscapeString(signalName), whenFalse), nil
				}
				return fmt.Sprintf(`<div data-spl-if="%s" style="display:none">%s</div><div data-spl-else="%s">%s</div>`, html.EscapeString(signalName), whenTrue, html.EscapeString(signalName), whenFalse), nil
			}
		}
	}
	for _, branch := range n.Branches {
		obj, err := e.evalExpr(branch.Cond, env)
		if err != nil {
			return "", fmt.Errorf("@if condition: %w", err)
		}
		if interpreter.IsTruthy(obj) {
			return e.renderBody(branch.Body, env, data, depth)
		}
	}
	if n.Else != nil {
		return e.renderBody(n.Else, env, data, depth)
	}
	return "", nil
}

func (e *Engine) renderFor(n *ForNode, env *interpreter.Environment, data map[string]any, depth int) (string, error) {
	iterObj, err := e.evalExpr(n.Iter, env)
	if err != nil {
		return "", fmt.Errorf("@for iterator: %w", err)
	}

	type iterItem struct {
		key   interpreter.Object
		value interpreter.Object
	}

	var items []iterItem

	switch v := iterObj.(type) {
	case *interpreter.Array:
		for i, elem := range v.Elements {
			items = append(items, iterItem{
				key:   &interpreter.Integer{Value: int64(i)},
				value: elem,
			})
		}
	case *interpreter.Hash:
		// Sort keys for deterministic output
		keys := make([]string, 0, len(v.Pairs))
		keyMap := make(map[string]interpreter.HashPair)
		for _, pair := range v.Pairs {
			k := pair.Key.Inspect()
			keys = append(keys, k)
			keyMap[k] = pair
		}
		sort.Strings(keys)
		for _, k := range keys {
			pair := keyMap[k]
			items = append(items, iterItem{key: pair.Key, value: pair.Value})
		}
	default:
		// Check for null/empty
		if iterObj == nil || iterObj.Type() == interpreter.NULL_OBJ {
			if n.Empty != nil {
				return e.renderBody(n.Empty, env, data, depth)
			}
			return "", nil
		}
		return "", fmt.Errorf("@for: cannot iterate over %s", iterObj.Type())
	}

	if len(items) == 0 {
		if n.Empty != nil {
			return e.renderBody(n.Empty, env, data, depth)
		}
		return "", nil
	}

	var buf strings.Builder
	length := len(items)

	// Create loop metadata hash once, update in-place each iteration
	loopHash := makeLoopMeta(0, length)

	for i, item := range items {
		// Set loop variables
		env.Set(n.ValVar, item.value)
		if n.KeyVar != "" {
			env.Set(n.KeyVar, item.key)
		}

		// Update $loop metadata in-place
		updateLoopMeta(loopHash, i, length)
		env.Set("loop", loopHash)

		s, err := e.renderBody(n.Body, env, data, depth)
		if err != nil {
			return "", err
		}
		buf.WriteString(s)
	}

	return buf.String(), nil
}

func makeLoopMeta(index, length int) *interpreter.Hash {
	pairs := map[interpreter.HashKey]interpreter.HashPair{}

	addPair := func(name string, val interpreter.Object) {
		key := &interpreter.String{Value: name}
		pairs[key.HashKey()] = interpreter.HashPair{Key: key, Value: val}
	}

	addPair("index", &interpreter.Integer{Value: int64(index)})
	addPair("index1", &interpreter.Integer{Value: int64(index + 1)})
	addPair("first", nativeBool(index == 0))
	addPair("last", nativeBool(index == length-1))
	addPair("length", &interpreter.Integer{Value: int64(length)})

	return &interpreter.Hash{Pairs: pairs}
}

// updateLoopMeta updates an existing loop metadata hash in-place to avoid allocations.
func updateLoopMeta(h *interpreter.Hash, index, length int) {
	for k, pair := range h.Pairs {
		key, ok := pair.Key.(*interpreter.String)
		if !ok {
			continue
		}
		switch key.Value {
		case "index":
			pair.Value = &interpreter.Integer{Value: int64(index)}
			h.Pairs[k] = pair
		case "index1":
			pair.Value = &interpreter.Integer{Value: int64(index + 1)}
			h.Pairs[k] = pair
		case "first":
			pair.Value = nativeBool(index == 0)
			h.Pairs[k] = pair
		case "last":
			pair.Value = nativeBool(index == length-1)
			h.Pairs[k] = pair
		}
	}
}

func nativeBool(v bool) *interpreter.Boolean {
	if v {
		return interpreter.TRUE
	}
	return interpreter.FALSE
}

func (e *Engine) renderSwitch(n *SwitchNode, env *interpreter.Environment, data map[string]any, depth int) (string, error) {
	switchObj, err := e.evalExpr(n.Expr, env)
	if err != nil {
		return "", fmt.Errorf("@switch expression: %w", err)
	}
	switchStr := objectToString(switchObj)

	for _, c := range n.Cases {
		for _, valExpr := range c.Values {
			caseObj, err := e.evalExpr(valExpr, env)
			if err != nil {
				return "", fmt.Errorf("@case value: %w", err)
			}
			if objectToString(caseObj) == switchStr {
				return e.renderBody(c.Body, env, data, depth)
			}
		}
	}

	if n.Default != nil {
		return e.renderBody(n.Default, env, data, depth)
	}
	return "", nil
}

func (e *Engine) renderMatch(n *MatchNode, env *interpreter.Environment, data map[string]any, depth int) (string, error) {
	// Evaluate match subject
	matchVal, err := e.evalExpr(n.Expr, env)
	if err != nil {
		return "", fmt.Errorf("@match expression: %w", err)
	}

	for _, c := range n.Cases {
		// Parse the pattern by constructing a mini match expression
		matched, caseEnv, err := e.evalMatchCase(c, matchVal, env)
		if err != nil {
			return "", fmt.Errorf("@match @case(%s): %w", c.PatternExpr, err)
		}
		if matched {
			return e.renderBody(c.Body, caseEnv, data, depth)
		}
	}

	if n.Default != nil {
		return e.renderBody(n.Default, env, data, depth)
	}
	return "", nil
}

// evalMatchCase parses a pattern string and tests it against a value.
// Returns (matched, environment with bindings, error).
func (e *Engine) evalMatchCase(c MatchCase, value interpreter.Object, env *interpreter.Environment) (bool, *interpreter.Environment, error) {
	// Build a synthetic match expression: match (__v__) { case <pattern> => true }
	src := "match (__matchval__) { case " + c.PatternExpr + " => true }"

	// Check cache
	program, ok := e.exprCache[src]
	if !ok {
		l := interpreter.NewLexer(src)
		p := interpreter.NewParser(l)
		program = p.ParseProgram()
		if errs := p.Errors(); len(errs) > 0 {
			return false, nil, fmt.Errorf("pattern parse error: %s", strings.Join(errs, "; "))
		}
		e.exprCache[src] = program
	}

	// Extract the MatchExpression and its first case's pattern
	if len(program.Statements) == 0 {
		return false, nil, fmt.Errorf("empty pattern program")
	}
	exprStmt, ok := program.Statements[0].(*interpreter.ExpressionStatement)
	if !ok {
		return false, nil, fmt.Errorf("expected expression statement")
	}
	matchExpr, ok := exprStmt.Expression.(*interpreter.MatchExpression)
	if !ok {
		return false, nil, fmt.Errorf("expected match expression")
	}
	if len(matchExpr.Cases) == 0 {
		return false, nil, fmt.Errorf("no cases in pattern")
	}

	pattern := matchExpr.Cases[0].Pattern
	caseEnv := interpreter.NewEnclosedEnvironment(env)

	if !interpreter.MatchPattern(pattern, value, caseEnv) {
		return false, nil, nil
	}

	// Check guard if present
	if c.Guard != "" {
		guardObj, err := e.evalExpr(c.Guard, caseEnv)
		if err != nil {
			return false, nil, fmt.Errorf("guard error: %w", err)
		}
		if !interpreter.IsTruthy(guardObj) {
			return false, nil, nil
		}
	}

	return true, caseEnv, nil
}

func (e *Engine) renderInclude(n *IncludeNode, env *interpreter.Environment, data map[string]any, depth int) (string, error) {
	if depth > e.MaxDepth {
		return "", fmt.Errorf("max include depth (%d) exceeded", e.MaxDepth)
	}

	resolved := e.resolvePath(n.Path)
	nodes, err := e.loadFile(resolved)
	if err != nil {
		return "", fmt.Errorf("@include %s: %w", n.Path, err)
	}

	// If there's a data expression, create a child scope with merged data
	if n.DataExpr != "" {
		includeData := make(map[string]any)
		for k, v := range data {
			includeData[k] = v
		}
		obj, err := e.evalExpr(n.DataExpr, env)
		if err != nil {
			return "", fmt.Errorf("@include data expression: %w", err)
		}
		if hash, ok := obj.(*interpreter.Hash); ok {
			for _, pair := range hash.Pairs {
				key := objectToString(pair.Key)
				includeData[key] = objectToNative(pair.Value)
			}
		}
		return e.renderNodes(nodes, includeData, depth+1)
	}

	// No data expression — render inline in the parent environment so that
	// @let/@signal/@computed variables defined in one include are visible
	// to subsequent includes (shared scope).
	// Pre-pass: register any component definitions
	var regularNodes []Node
	for _, nd := range nodes {
		if comp, ok := nd.(*ComponentNode); ok {
			e.mu.Lock()
			e.Components[comp.Name] = componentDef{Body: comp.Body, Props: comp.Props}
			e.mu.Unlock()
		} else {
			regularNodes = append(regularNodes, nd)
		}
	}

	var buf strings.Builder
	for _, nd := range regularNodes {
		s, err := e.renderNode(nd, env, data, depth+1)
		if err != nil {
			return "", err
		}
		buf.WriteString(s)
	}
	return buf.String(), nil
}

func (e *Engine) renderImport(n *ImportNode, env *interpreter.Environment, data map[string]any, depth int) (string, error) {
	// Load and register components from the imported file at render time.
	// This handles @import inside @define blocks where the compile-time
	// top-level scan cannot reach.
	resolved := e.resolvePath(n.Path)
	nodes, err := e.loadFile(resolved)
	if err != nil {
		return "", fmt.Errorf("@import %s: %w", n.Path, err)
	}
	for _, nd := range nodes {
		if comp, ok := nd.(*ComponentNode); ok {
			e.mu.Lock()
			e.Components[comp.Name] = componentDef{Body: comp.Body, Props: comp.Props}
			e.mu.Unlock()
		}
	}
	return "", nil
}

func (e *Engine) renderHandler(n *HandlerNode, env *interpreter.Environment, data map[string]any, depth int) (string, error) {
	body := n.Expr
	if strings.TrimSpace(n.Body) != "" {
		body = n.Body
	}
	e.registerHandler(n.Name, body)
	return "", nil
}

func (e *Engine) pushSlotContext(sc *slotContext) { e.slotStack = append(e.slotStack, sc) }
func (e *Engine) popSlotContext()                 { e.slotStack = e.slotStack[:len(e.slotStack)-1] }
func (e *Engine) currentSlotContext() *slotContext {
	if len(e.slotStack) == 0 {
		return nil
	}
	return e.slotStack[len(e.slotStack)-1]
}

func (e *Engine) renderRender(n *RenderNode, env *interpreter.Environment, data map[string]any, depth int) (string, error) {
	// Look up component
	e.mu.RLock()
	comp, ok := e.Components[n.Name]
	e.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("@render: undefined component %q", n.Name)
	}

	// Separate @fill nodes from default children
	var defaultChildren []Node
	var fills map[string]string

	for _, child := range n.Children {
		if fill, ok := child.(*FillNode); ok {
			if fills == nil {
				fills = make(map[string]string)
			}
			rendered, err := e.renderBody(fill.Body, env, data, depth)
			if err != nil {
				return "", fmt.Errorf("@fill(%s): %w", fill.Name, err)
			}
			fills[fill.Name] = rendered
		} else {
			defaultChildren = append(defaultChildren, child)
		}
	}

	// Render default children with caller's scope
	var childrenStr string
	var err error
	if len(defaultChildren) > 0 {
		childrenStr, err = e.renderBody(defaultChildren, env, data, depth)
		if err != nil {
			return "", fmt.Errorf("@render children: %w", err)
		}
	}

	// Evaluate props expression — keep as Object to avoid round-trip conversion
	var propsObj interpreter.Object
	if n.PropsExpr != "" {
		propsObj, err = e.evalExpr(n.PropsExpr, env)
		if err != nil {
			return "", fmt.Errorf("@render props: %w", err)
		}
	}

	// Create component env enclosed from caller env (inherits all caller variables)
	compEnv := interpreter.NewEnclosedEnvironment(env)

	// Set up props based on what we got
	propsHash, isHash := propsObj.(*interpreter.Hash)

	if len(comp.Props) > 0 {
		// Declared props: set only the declared variables
		// Track if we need to rebuild props hash (when defaults are applied)
		needsPropsRebuild := false
		if isHash {
			// Fast path: props already a Hash, set named props directly from hash pairs
			for _, pd := range comp.Props {
				varName := pd.Name
				if pd.Alias != "" {
					varName = pd.Alias
				}
				key := &interpreter.String{Value: pd.Name}
				hk := key.HashKey()
				if pair, exists := propsHash.Pairs[hk]; exists {
					compEnv.Set(varName, pair.Value)
				} else if pd.Default != "" {
					obj, err := e.evalExpr(pd.Default, env)
					if err != nil {
						return "", fmt.Errorf("@component %q prop %q default: %w", n.Name, pd.Name, err)
					}
					compEnv.Set(varName, obj)
					// Add default to props hash so props.xxx reflects it
					propsHash.Pairs[hk] = interpreter.HashPair{Key: key, Value: obj}
					needsPropsRebuild = true
				}
			}
			_ = needsPropsRebuild // props hash was modified in-place
		} else {
			// No props passed — build a hash with just defaults
			pairs := make(map[interpreter.HashKey]interpreter.HashPair)
			for _, pd := range comp.Props {
				if pd.Default != "" {
					varName := pd.Name
					if pd.Alias != "" {
						varName = pd.Alias
					}
					obj, err := e.evalExpr(pd.Default, env)
					if err != nil {
						return "", fmt.Errorf("@component %q prop %q default: %w", n.Name, pd.Name, err)
					}
					compEnv.Set(varName, obj)
					key := &interpreter.String{Value: pd.Name}
					pairs[key.HashKey()] = interpreter.HashPair{Key: key, Value: obj}
				}
			}
			propsObj = &interpreter.Hash{Pairs: pairs}
		}
	} else {
		// No declared props: spread all hash pairs as top-level vars
		if isHash {
			for _, pair := range propsHash.Pairs {
				compEnv.Set(objectToString(pair.Key), pair.Value)
			}
		}
	}

	// Inject "props" object as-is (no round-trip)
	if propsObj != nil {
		compEnv.Set("props", propsObj)
	} else {
		compEnv.Set("props", emptyHash)
	}

	// Make children string available
	compEnv.Set("children", &interpreter.String{Value: childrenStr})

	// Push slot context and render component body
	e.pushSlotContext(&slotContext{fills: fills, children: childrenStr})
	defer e.popSlotContext()

	var buf strings.Builder
	for _, cn := range comp.Body {
		s, err := e.renderNode(cn, compEnv, data, depth+1)
		if err != nil {
			return "", err
		}
		buf.WriteString(s)
	}
	return buf.String(), nil
}

func (e *Engine) renderSlot(n *SlotNode) (string, error) {
	sc := e.currentSlotContext()
	if sc == nil {
		return "", nil
	}
	if n.Name == "" {
		// Default slot — render children
		return sc.children, nil
	}
	// Named slot
	if content, ok := sc.fills[n.Name]; ok {
		return content, nil
	}
	return "", nil
}

func (e *Engine) renderBody(nodes []Node, env *interpreter.Environment, data map[string]any, depth int) (string, error) {
	var buf strings.Builder
	for _, n := range nodes {
		s, err := e.renderNode(n, env, data, depth)
		if err != nil {
			return "", err
		}
		buf.WriteString(s)
	}
	return buf.String(), nil
}

func (e *Engine) renderLet(n *LetNode, env *interpreter.Environment, data map[string]any, depth int) (string, error) {
	obj, err := e.evalExpr(n.Expr, env)
	if err != nil {
		return "", fmt.Errorf("@let(%s): %w", n.VarName, err)
	}
	env.Set(n.VarName, obj)
	if data != nil {
		data[n.VarName] = objectToNative(obj)
	}
	return "", nil
}

func (e *Engine) renderComputed(n *ComputedNode, env *interpreter.Environment, data map[string]any, depth int) (string, error) {
	obj, err := e.evalExpr(n.Expr, env)
	if err != nil {
		return "", fmt.Errorf("@computed(%s): %w", n.VarName, err)
	}
	env.Set(n.VarName, obj)
	if data != nil {
		data[n.VarName] = objectToNative(obj)
	}
	return "", nil
}

func (e *Engine) renderWatch(n *WatchNode, env *interpreter.Environment, data map[string]any, depth int) (string, error) {
	obj, err := e.evalExpr(n.Expr, env)
	if err != nil {
		return "", fmt.Errorf("@watch(%s): %w", n.Expr, err)
	}
	currentVal := objectToString(obj)

	prevVal, seen := e.watchState[n.Expr]
	if !seen || prevVal != currentVal {
		e.watchState[n.Expr] = currentVal
		return e.renderBody(n.Body, env, data, depth)
	}
	return "", nil
}

func (e *Engine) renderSignal(n *SignalNode, env *interpreter.Environment, data map[string]any, depth int) (string, error) {
	obj, err := e.evalExpr(n.InitialExpr, env)
	if err != nil {
		return "", fmt.Errorf("@signal(%s): %w", n.Name, err)
	}
	env.Set(n.Name, obj)
	if data != nil {
		data[n.Name] = objectToNative(obj)
	}
	e.registerSignal(n.Name, obj)
	return "", nil
}

func (e *Engine) renderBind(n *BindNode, env *interpreter.Environment, data map[string]any, depth int) (string, error) {
	obj, err := e.evalExpr(n.Signal, env)
	if err != nil {
		return "", fmt.Errorf("@bind(%s): %w", n.Signal, err)
	}
	if e.hydration == nil {
		return signalValueForAttr(n.Attr, obj), nil
	}
	id := e.nextBindID()
	return WrapWithHydration(signalValueForAttr(n.Attr, obj), n.Signal, n.Attr, id), nil
}

func (e *Engine) renderEffect(n *EffectNode, env *interpreter.Environment, data map[string]any, depth int) (string, error) {
	if e.hydration == nil {
		return e.renderBody(n.Body, env, data, depth)
	}
	initialBody, err := e.renderBody(n.Body, env, data, depth)
	if err != nil {
		return "", fmt.Errorf("@effect: %w", err)
	}
	selector := e.nextEffectSelector()
	effectEnv := interpreter.NewEnclosedEnvironment(env)
	for _, dep := range n.Deps {
		dep = strings.TrimSpace(dep)
		if dep == "" {
			continue
		}
		if obj, ok := env.Get(dep); ok && isCompoundObject(obj) {
			effectEnv.Set(dep, makePlaceholderObject(obj, dep))
		} else {
			effectEnv.Set(dep, &interpreter.String{Value: signalPlaceholder(dep)})
		}
	}
	body, err := e.renderBody(n.Body, effectEnv, data, depth)
	if err != nil {
		return "", fmt.Errorf("@effect: %w", err)
	}
	compiled := compileHydrationHTML(body)
	e.trackEffectHTML(selector, compiled, n.Deps)
	return fmt.Sprintf(`<div data-spl-effect="%d" style="display: contents;">%s</div>`, e.hydration.EffectsID, initialBody), nil
}

func (e *Engine) renderReactiveView(n *ReactiveViewNode, env *interpreter.Environment, data map[string]any, depth int) (string, error) {
	initialBody, err := e.renderBody(n.Body, env, data, depth)
	if err != nil {
		return "", fmt.Errorf("@reactive: %w", err)
	}
	if e.hydration == nil {
		return initialBody, nil
	}
	selector := e.nextViewSelector()
	viewEnv := interpreter.NewEnclosedEnvironment(env)
	for _, dep := range n.Deps {
		dep = strings.TrimSpace(dep)
		if dep == "" {
			continue
		}
		if obj, ok := env.Get(dep); ok && isCompoundObject(obj) {
			viewEnv.Set(dep, makePlaceholderObject(obj, dep))
		} else {
			viewEnv.Set(dep, &interpreter.String{Value: signalPlaceholder(dep)})
		}
	}
	body, err := e.renderBody(n.Body, viewEnv, data, depth)
	if err != nil {
		return "", fmt.Errorf("@reactive: %w", err)
	}
	e.trackViewHTML(selector, compileHydrationHTML(body), n.Deps)
	return WrapReactiveView(initialBody, e.hydration.ViewID), nil
}

func (e *Engine) renderClick(n *ClickNode, env *interpreter.Environment, data map[string]any, depth int) (string, error) {
	if e.hydration == nil {
		return fmt.Sprintf(`<button type="button">%s</button>`, html.EscapeString(n.Label)), nil
	}
	return WrapClickAction(html.EscapeString(n.Label), n.Signal, n.Action, n.Value), nil
}

// objectToString converts an SPL Object to its string representation for template output.
func objectToString(obj interpreter.Object) string {
	if obj == nil {
		return ""
	}
	switch v := obj.(type) {
	case *interpreter.String:
		return v.Value
	case *interpreter.Integer:
		return fmt.Sprintf("%d", v.Value)
	case *interpreter.Float:
		return fmt.Sprintf("%g", v.Value)
	case *interpreter.Boolean:
		if v.Value {
			return "true"
		}
		return "false"
	case *interpreter.Null:
		return ""
	default:
		return obj.Inspect()
	}
}

// objectToNative converts an SPL Object to a Go native value for passing as template data.
func objectToNative(obj interpreter.Object) any {
	if obj == nil {
		return nil
	}
	switch v := obj.(type) {
	case *interpreter.String:
		return v.Value
	case *interpreter.Integer:
		return v.Value
	case *interpreter.Float:
		return v.Value
	case *interpreter.Boolean:
		return v.Value
	case *interpreter.Null:
		return nil
	case *interpreter.Array:
		result := make([]any, len(v.Elements))
		for i, el := range v.Elements {
			result[i] = objectToNative(el)
		}
		return result
	case *interpreter.Hash:
		result := make(map[string]any)
		for _, pair := range v.Pairs {
			key := objectToString(pair.Key)
			result[key] = objectToNative(pair.Value)
		}
		return result
	default:
		return obj.Inspect()
	}
}

// isCompoundObject returns true if the object is a Hash or Array (not a simple scalar).
func isCompoundObject(obj interpreter.Object) bool {
	switch obj.(type) {
	case *interpreter.Hash, *interpreter.Array:
		return true
	}
	return false
}

// makePlaceholderHash creates a Hash with the same keys as the original,
// but each value is a string placeholder like __SPL_SIGNAL__signalName.key__.
// This is used during the hydration template pass so that ${signal.key}
// produces an interpolatable placeholder in the view source.
func makePlaceholderHash(original *interpreter.Hash, signalName string) *interpreter.Hash {
	ph := &interpreter.Hash{Pairs: make(map[interpreter.HashKey]interpreter.HashPair, len(original.Pairs))}
	for hk, pair := range original.Pairs {
		keyStr := ""
		if s, ok := pair.Key.(*interpreter.String); ok {
			keyStr = s.Value
		} else {
			keyStr = pair.Key.Inspect()
		}
		path := signalName + "." + keyStr
		// For nested hashes, recurse
		if innerHash, ok := pair.Value.(*interpreter.Hash); ok {
			ph.Pairs[hk] = interpreter.HashPair{
				Key:   pair.Key,
				Value: makePlaceholderHash(innerHash, path),
			}
		} else {
			ph.Pairs[hk] = interpreter.HashPair{
				Key:   pair.Key,
				Value: &interpreter.String{Value: signalPlaceholder(path)},
			}
		}
	}
	return ph
}

// makePlaceholderObject creates a placeholder version of a compound object.
// Hashes get placeholder values per key; Arrays get a string placeholder for the whole signal.
func makePlaceholderObject(obj interpreter.Object, signalName string) interpreter.Object {
	switch v := obj.(type) {
	case *interpreter.Hash:
		return makePlaceholderHash(v, signalName)
	default:
		return &interpreter.String{Value: signalPlaceholder(signalName)}
	}
}

// nativeToObject converts a Go native value to an interpreter Object.
// Delegates to the interpreter's comprehensive ToObject which supports
// structs, typed slices, typed maps, and all Go primitive types via reflection.
func nativeToObject(v any) interpreter.Object {
	return interpreter.ToObject(v)
}
