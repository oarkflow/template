package template

import (
	"fmt"
	"strings"
)

// --- Node types ---

type Node interface {
	nodeType() string
}

type TextNode struct {
	Text string
}

func (n *TextNode) nodeType() string { return "text" }

type ExprNode struct {
	Expr    string
	Raw     bool
	Filters []FilterCall
}

func (n *ExprNode) nodeType() string { return "expr" }

type FilterCall struct {
	Name string
	Args []string
}

type IfBranch struct {
	Cond string // SPL expression; empty for @else
	Body []Node
}

type IfNode struct {
	Branches []IfBranch
	Else     []Node
}

func (n *IfNode) nodeType() string { return "if" }

type ForNode struct {
	KeyVar string // "" if only value
	ValVar string
	Iter   string // SPL expression for the iterable
	Body   []Node
	Empty  []Node // rendered when iterable is empty
}

func (n *ForNode) nodeType() string { return "for" }

type SwitchCase struct {
	Values []string // SPL expressions; empty slice means @default
	Body   []Node
}

type SwitchNode struct {
	Expr    string
	Cases   []SwitchCase
	Default []Node
}

func (n *SwitchNode) nodeType() string { return "switch" }

// MatchCase holds a single @case branch inside @match.
type MatchCase struct {
	PatternExpr string // raw pattern text (e.g. "x: integer", "[a, b]", "> 10")
	Guard       string // optional guard expression after "if"
	Body        []Node
}

// MatchNode represents a @match(expr) { @case(pattern) { ... } @default { ... } } directive.
type MatchNode struct {
	Expr    string // the match subject expression
	Cases   []MatchCase
	Default []Node // @default body (nil if none)
}

func (n *MatchNode) nodeType() string { return "match" }

type RawNode struct {
	Text string
}

func (n *RawNode) nodeType() string { return "raw" }

type IncludeNode struct {
	Path     string // file path (string literal)
	DataExpr string // optional SPL expression for include-local data
}

func (n *IncludeNode) nodeType() string { return "include" }

type ImportNode struct {
	Path string
}

func (n *ImportNode) nodeType() string { return "import" }

type HandlerNode struct {
	Name string
	Expr string
	Body string
}

func (n *HandlerNode) nodeType() string { return "handler" }

type ExtendsNode struct {
	Path string
}

func (n *ExtendsNode) nodeType() string { return "extends" }

type BlockNode struct {
	Name string
	Body []Node
}

func (n *BlockNode) nodeType() string { return "block" }

type DefineNode struct {
	Name string
	Body []Node
}

func (n *DefineNode) nodeType() string { return "define" }

// PropDef describes a single declared prop with optional alias and default.
type PropDef struct {
	Name    string // external prop name (what the caller passes)
	Alias   string // internal variable name ("" = same as Name)
	Default string // SPL expression for default value ("" = none)
}

// ComponentNode defines a reusable component.
type ComponentNode struct {
	Name  string    // component name
	Props []PropDef // declared prop definitions (optional)
	Body  []Node    // component body (template)
}

func (n *ComponentNode) nodeType() string { return "component" }

// RenderNode invokes a component by name.
type RenderNode struct {
	Name      string // component name
	PropsExpr string // optional SPL expression for props (hash literal)
	Children  []Node // body nodes (children/slot fills)
}

func (n *RenderNode) nodeType() string { return "render" }

// SlotNode is a placeholder inside a component body for injected content.
type SlotNode struct {
	Name string // "" = default slot
}

func (n *SlotNode) nodeType() string { return "slot" }

// FillNode provides content for a named slot inside a @render body.
type FillNode struct {
	Name string
	Body []Node
}

func (n *FillNode) nodeType() string { return "fill" }

// LetNode assigns a computed value to a variable at render time.
type LetNode struct {
	VarName string
	Expr    string
}

func (n *LetNode) nodeType() string { return "let" }

// ComputedNode defines a derived value (same semantics as @let, separate for intent).
type ComputedNode struct {
	VarName string
	Expr    string
}

func (n *ComputedNode) nodeType() string { return "computed" }

// WatchNode renders its body only when the watched expression value changes.
type WatchNode struct {
	Expr string
	Body []Node
}

func (n *WatchNode) nodeType() string { return "watch" }

// --- Parser ---

type parser struct {
	src []rune
	pos int
}

func parse(src string) ([]Node, error) {
	p := &parser{src: []rune(src)}
	return p.parseNodes(false)
}

func (p *parser) remaining() int { return len(p.src) - p.pos }
func (p *parser) eof() bool      { return p.pos >= len(p.src) }
func (p *parser) peek() rune {
	if p.eof() {
		return 0
	}
	return p.src[p.pos]
}
func (p *parser) advance() rune {
	ch := p.src[p.pos]
	p.pos++
	return ch
}
func (p *parser) peekAt(offset int) rune {
	i := p.pos + offset
	if i >= len(p.src) || i < 0 {
		return 0
	}
	return p.src[i]
}
func (p *parser) startsWith(s string) bool {
	runes := []rune(s)
	if p.remaining() < len(runes) {
		return false
	}
	for i, r := range runes {
		if p.src[p.pos+i] != r {
			return false
		}
	}
	return true
}
func (p *parser) advanceN(n int) {
	p.pos += n
	if p.pos > len(p.src) {
		p.pos = len(p.src)
	}
}
func (p *parser) skipWhitespace() {
	for !p.eof() && (p.peek() == ' ' || p.peek() == '\t') {
		p.advance()
	}
}

// parseNodes parses nodes until EOF or a closing '}' (if inBlock is true).
func (p *parser) parseNodes(inBlock bool) ([]Node, error) {
	var nodes []Node
	var textBuf strings.Builder
	var textQuote rune
	var prevTextRune rune

	flushText := func() {
		if textBuf.Len() > 0 {
			nodes = append(nodes, &TextNode{Text: textBuf.String()})
			textBuf.Reset()
		}
	}

	for !p.eof() {
		if textQuote != 0 {
			if p.peek() == '$' && p.peekAt(1) == '{' {
				flushText()
				node, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				nodes = append(nodes, node)
				prevTextRune = 0
				continue
			}
			ch := p.advance()
			textBuf.WriteRune(ch)
			if ch == textQuote && prevTextRune != '\\' {
				textQuote = 0
			}
			prevTextRune = ch
			continue
		}

		// Check for closing brace when inside a block
		if inBlock && p.peek() == '}' {
			p.advance() // consume '}'
			flushText()
			return nodes, nil
		}

		// Expression: ${...}
		if p.peek() == '$' && p.peekAt(1) == '{' {
			flushText()
			node, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, node)
			continue
		}

		// Directive: @keyword
		if p.peek() == '@' {
			// Check what follows
			if p.peekAt(1) == '/' && p.peekAt(2) == '/' {
				// Comment: @// ...
				flushText()
				p.parseComment()
				continue
			}
			keyword := p.peekKeyword()
			switch keyword {
			case "if":
				flushText()
				node, err := p.parseIf()
				if err != nil {
					return nil, err
				}
				nodes = append(nodes, node)
				continue
			case "for":
				flushText()
				node, err := p.parseFor()
				if err != nil {
					return nil, err
				}
				nodes = append(nodes, node)
				continue
			case "switch":
				flushText()
				node, err := p.parseSwitchDirective()
				if err != nil {
					return nil, err
				}
				nodes = append(nodes, node)
				continue
			case "match":
				flushText()
				node, err := p.parseMatchDirective()
				if err != nil {
					return nil, err
				}
				nodes = append(nodes, node)
				continue
			case "raw":
				flushText()
				node, err := p.parseRaw()
				if err != nil {
					return nil, err
				}
				nodes = append(nodes, node)
				continue
			case "include":
				flushText()
				node, err := p.parseInclude()
				if err != nil {
					return nil, err
				}
				nodes = append(nodes, node)
				continue
			case "import":
				flushText()
				node, err := p.parseImportDirective()
				if err != nil {
					return nil, err
				}
				nodes = append(nodes, node)
				continue
			case "handler":
				flushText()
				node, err := p.parseHandlerDirective()
				if err != nil {
					return nil, err
				}
				nodes = append(nodes, node)
				continue
			case "extends":
				flushText()
				node, err := p.parseExtends()
				if err != nil {
					return nil, err
				}
				nodes = append(nodes, node)
				continue
			case "block":
				flushText()
				node, err := p.parseBlock()
				if err != nil {
					return nil, err
				}
				nodes = append(nodes, node)
				continue
			case "define":
				flushText()
				node, err := p.parseDefine()
				if err != nil {
					return nil, err
				}
				nodes = append(nodes, node)
				continue
			case "component":
				flushText()
				node, err := p.parseComponent()
				if err != nil {
					return nil, err
				}
				nodes = append(nodes, node)
				continue
			case "render":
				flushText()
				node, err := p.parseRender()
				if err != nil {
					return nil, err
				}
				nodes = append(nodes, node)
				continue
			case "slot":
				flushText()
				node, err := p.parseSlot()
				if err != nil {
					return nil, err
				}
				nodes = append(nodes, node)
				continue
			case "fill":
				flushText()
				node, err := p.parseFill()
				if err != nil {
					return nil, err
				}
				nodes = append(nodes, node)
				continue
			case "let":
				flushText()
				node, err := p.parseLet()
				if err != nil {
					return nil, err
				}
				nodes = append(nodes, node)
				continue
			case "computed":
				flushText()
				node, err := p.parseComputed()
				if err != nil {
					return nil, err
				}
				nodes = append(nodes, node)
				continue
			case "watch":
				flushText()
				node, err := p.parseWatch()
				if err != nil {
					return nil, err
				}
				nodes = append(nodes, node)
				continue
			case "signal":
				flushText()
				node, err := p.parseSignal()
				if err != nil {
					return nil, err
				}
				nodes = append(nodes, node)
				continue
			case "bind":
				flushText()
				node, err := p.parseBind()
				if err != nil {
					return nil, err
				}
				nodes = append(nodes, node)
				continue
			case "effect":
				flushText()
				node, err := p.parseEffect()
				if err != nil {
					return nil, err
				}
				nodes = append(nodes, node)
				continue
			case "reactive":
				flushText()
				node, err := p.parseReactiveView()
				if err != nil {
					return nil, err
				}
				nodes = append(nodes, node)
				continue
			case "click":
				flushText()
				node, err := p.parseClick()
				if err != nil {
					return nil, err
				}
				nodes = append(nodes, node)
				continue
			case "stream":
				flushText()
				node, err := p.parseStream()
				if err != nil {
					return nil, err
				}
				nodes = append(nodes, node)
				continue
			case "defer":
				flushText()
				node, err := p.parseDefer()
				if err != nil {
					return nil, err
				}
				nodes = append(nodes, node)
				continue
			case "lazy":
				flushText()
				node, err := p.parseLazy()
				if err != nil {
					return nil, err
				}
				nodes = append(nodes, node)
				continue
			case "elseif", "else", "empty", "case", "default", "fallback":
				// These are terminators for parent blocks — stop parsing here
				flushText()
				return nodes, nil
			}
		}

		if p.peek() == '"' || p.peek() == '\'' || p.peek() == '`' {
			ch := p.advance()
			textQuote = ch
			textBuf.WriteRune(ch)
			prevTextRune = ch
			continue
		}

		// Regular text
		ch := p.advance()
		textBuf.WriteRune(ch)
		prevTextRune = ch
	}

	if inBlock {
		return nil, fmt.Errorf("unexpected end of template: unclosed block")
	}

	flushText()
	return nodes, nil
}

// peekKeyword returns the keyword after '@' without advancing.
func (p *parser) peekKeyword() string {
	i := p.pos + 1 // skip '@'
	start := i
	for i < len(p.src) && isAlpha(p.src[i]) {
		i++
	}
	if start == i {
		return ""
	}
	return string(p.src[start:i])
}

func isAlpha(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_'
}

// parseExpr parses ${expr}, ${raw expr}, ${expr | filter}
func (p *parser) parseExpr() (*ExprNode, error) {
	p.advanceN(2) // skip '${'
	var buf strings.Builder
	depth := 1
	for !p.eof() && depth > 0 {
		ch := p.peek()
		if ch == '{' {
			depth++
			buf.WriteRune(p.advance())
		} else if ch == '}' {
			depth--
			if depth == 0 {
				p.advance() // consume closing '}'
				break
			}
			buf.WriteRune(p.advance())
		} else if ch == '"' || ch == '\'' || ch == '`' {
			// Read string literal to avoid counting braces inside strings
			buf.WriteString(p.readStringLiteral())
		} else {
			buf.WriteRune(p.advance())
		}
	}
	if depth != 0 {
		return nil, fmt.Errorf("unclosed expression ${...}")
	}

	content := buf.String()
	node := &ExprNode{}

	// Check for raw prefix
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "raw ") {
		node.Raw = true
		content = strings.TrimPrefix(trimmed, "raw ")
	}

	// Split on '|' for filters (but not inside strings/parens)
	parts := splitPipes(content)
	node.Expr = strings.TrimSpace(parts[0])
	for _, fp := range parts[1:] {
		fc := parseFilterCall(strings.TrimSpace(fp))
		node.Filters = append(node.Filters, fc)
	}

	return node, nil
}

// splitPipes splits an expression by top-level '|' characters (not inside strings, parens, or braces).
func splitPipes(s string) []string {
	var parts []string
	var buf strings.Builder
	depth := 0 // tracks (), {}, []
	inStr := rune(0)

	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		ch := runes[i]
		if inStr != 0 {
			buf.WriteRune(ch)
			if ch == '\\' && i+1 < len(runes) {
				i++
				buf.WriteRune(runes[i])
			} else if ch == inStr {
				inStr = 0
			}
			continue
		}
		switch ch {
		case '"', '\'', '`':
			inStr = ch
			buf.WriteRune(ch)
		case '(', '{', '[':
			depth++
			buf.WriteRune(ch)
		case ')', '}', ']':
			depth--
			buf.WriteRune(ch)
		case '|':
			if depth == 0 {
				// Check for || (logical OR) — not a pipe
				if i+1 < len(runes) && runes[i+1] == '|' {
					buf.WriteRune(ch)
					i++
					buf.WriteRune(runes[i])
				} else {
					parts = append(parts, buf.String())
					buf.Reset()
				}
			} else {
				buf.WriteRune(ch)
			}
		default:
			buf.WriteRune(ch)
		}
	}
	parts = append(parts, buf.String())
	return parts
}

// parseFilterCall parses "filterName" or "filterName arg1 arg2"
func parseFilterCall(s string) FilterCall {
	// Format: name or name "arg" or name arg
	parts := splitFilterArgs(s)
	fc := FilterCall{Name: parts[0]}
	if len(parts) > 1 {
		fc.Args = parts[1:]
	}
	return fc
}

// splitFilterArgs splits filter name and arguments, respecting quoted strings.
func splitFilterArgs(s string) []string {
	var parts []string
	runes := []rune(s)
	i := 0
	for i < len(runes) {
		// Skip whitespace
		for i < len(runes) && (runes[i] == ' ' || runes[i] == '\t') {
			i++
		}
		if i >= len(runes) {
			break
		}
		if runes[i] == '"' || runes[i] == '\'' {
			quote := runes[i]
			i++ // skip opening quote
			var buf strings.Builder
			for i < len(runes) && runes[i] != quote {
				if runes[i] == '\\' && i+1 < len(runes) {
					i++
					buf.WriteRune(runes[i])
				} else {
					buf.WriteRune(runes[i])
				}
				i++
			}
			if i < len(runes) {
				i++ // skip closing quote
			}
			parts = append(parts, buf.String())
		} else {
			var buf strings.Builder
			for i < len(runes) && runes[i] != ' ' && runes[i] != '\t' {
				buf.WriteRune(runes[i])
				i++
			}
			parts = append(parts, buf.String())
		}
	}
	return parts
}

func (p *parser) readStringLiteral() string {
	var buf strings.Builder
	quote := p.advance()
	buf.WriteRune(quote)
	for !p.eof() {
		ch := p.advance()
		buf.WriteRune(ch)
		if ch == '\\' && !p.eof() {
			buf.WriteRune(p.advance())
		} else if ch == quote {
			break
		}
	}
	return buf.String()
}

// parseComment consumes @// ... until end of line.
func (p *parser) parseComment() {
	p.advanceN(3) // skip '@//'
	for !p.eof() && p.peek() != '\n' {
		p.advance()
	}
	if !p.eof() {
		p.advance() // consume '\n'
	}
}

// parseIf parses @if(cond) { ... } @elseif(cond) { ... } @else { ... }
func (p *parser) parseIf() (*IfNode, error) {
	node := &IfNode{}

	// Parse @if(cond) { body }
	p.advanceN(3) // skip '@if'
	cond, err := p.readParenExpr()
	if err != nil {
		return nil, fmt.Errorf("@if: %w", err)
	}
	p.skipWhitespaceAndNewlines()
	if p.peek() != '{' {
		return nil, fmt.Errorf("@if: expected '{' after condition")
	}
	p.advance() // skip '{'
	body, err := p.parseNodes(true)
	if err != nil {
		return nil, fmt.Errorf("@if body: %w", err)
	}
	node.Branches = append(node.Branches, IfBranch{Cond: cond, Body: body})

	// Parse optional @elseif / @else
	for {
		p.skipWhitespaceAndNewlines()
		if p.startsWith("@elseif") {
			p.advanceN(7) // skip '@elseif'
			cond, err := p.readParenExpr()
			if err != nil {
				return nil, fmt.Errorf("@elseif: %w", err)
			}
			p.skipWhitespaceAndNewlines()
			if p.peek() != '{' {
				return nil, fmt.Errorf("@elseif: expected '{'")
			}
			p.advance()
			body, err := p.parseNodes(true)
			if err != nil {
				return nil, fmt.Errorf("@elseif body: %w", err)
			}
			node.Branches = append(node.Branches, IfBranch{Cond: cond, Body: body})
		} else if p.startsWith("@else") && !p.startsWith("@elseif") {
			p.advanceN(5) // skip '@else'
			p.skipWhitespaceAndNewlines()
			if p.peek() != '{' {
				return nil, fmt.Errorf("@else: expected '{'")
			}
			p.advance()
			body, err := p.parseNodes(true)
			if err != nil {
				return nil, fmt.Errorf("@else body: %w", err)
			}
			node.Else = body
			break
		} else {
			break
		}
	}

	return node, nil
}

// parseFor parses @for(item in items) { ... } @empty { ... }
func (p *parser) parseFor() (*ForNode, error) {
	p.advanceN(4) // skip '@for'
	inner, err := p.readParenExpr()
	if err != nil {
		return nil, fmt.Errorf("@for: %w", err)
	}

	node := &ForNode{}
	// Parse "item in items" or "i, item in items" or "key, val in hash"
	inner = strings.TrimSpace(inner)
	inIdx := strings.Index(inner, " in ")
	if inIdx < 0 {
		return nil, fmt.Errorf("@for: expected 'VAR in EXPR' syntax, got: %s", inner)
	}
	vars := strings.TrimSpace(inner[:inIdx])
	node.Iter = strings.TrimSpace(inner[inIdx+4:])

	if strings.Contains(vars, ",") {
		parts := strings.SplitN(vars, ",", 2)
		node.KeyVar = strings.TrimSpace(parts[0])
		node.ValVar = strings.TrimSpace(parts[1])
	} else {
		node.ValVar = vars
	}

	p.skipWhitespaceAndNewlines()
	if p.peek() != '{' {
		return nil, fmt.Errorf("@for: expected '{'")
	}
	p.advance()
	body, err := p.parseNodes(true)
	if err != nil {
		return nil, fmt.Errorf("@for body: %w", err)
	}
	node.Body = body

	// Optional @empty { ... }
	p.skipWhitespaceAndNewlines()
	if p.startsWith("@empty") {
		p.advanceN(6)
		p.skipWhitespaceAndNewlines()
		if p.peek() != '{' {
			return nil, fmt.Errorf("@empty: expected '{'")
		}
		p.advance()
		empty, err := p.parseNodes(true)
		if err != nil {
			return nil, fmt.Errorf("@empty body: %w", err)
		}
		node.Empty = empty
	}

	return node, nil
}

// parseSwitchDirective parses @switch(expr) { @case(...) { ... } @default { ... } }
func (p *parser) parseSwitchDirective() (*SwitchNode, error) {
	p.advanceN(7) // skip '@switch'
	expr, err := p.readParenExpr()
	if err != nil {
		return nil, fmt.Errorf("@switch: %w", err)
	}
	node := &SwitchNode{Expr: expr}

	p.skipWhitespaceAndNewlines()
	if p.peek() != '{' {
		return nil, fmt.Errorf("@switch: expected '{'")
	}
	p.advance() // skip outer '{'

	// Parse @case and @default blocks until '}'
	for {
		p.skipWhitespaceAndNewlines()
		if p.eof() {
			return nil, fmt.Errorf("@switch: unclosed block")
		}
		if p.peek() == '}' {
			p.advance()
			break
		}
		if p.startsWith("@case") {
			p.advanceN(5)
			values, err := p.readParenExpr()
			if err != nil {
				return nil, fmt.Errorf("@case: %w", err)
			}
			// values can be comma-separated
			valParts := splitCaseValues(values)
			p.skipWhitespaceAndNewlines()
			if p.peek() != '{' {
				return nil, fmt.Errorf("@case: expected '{'")
			}
			p.advance()
			body, err := p.parseNodes(true)
			if err != nil {
				return nil, fmt.Errorf("@case body: %w", err)
			}
			node.Cases = append(node.Cases, SwitchCase{Values: valParts, Body: body})
		} else if p.startsWith("@default") {
			p.advanceN(8)
			p.skipWhitespaceAndNewlines()
			if p.peek() != '{' {
				return nil, fmt.Errorf("@default: expected '{'")
			}
			p.advance()
			body, err := p.parseNodes(true)
			if err != nil {
				return nil, fmt.Errorf("@default body: %w", err)
			}
			node.Default = body
		} else {
			return nil, fmt.Errorf("@switch: unexpected content, expected @case or @default")
		}
	}

	return node, nil
}

// parseMatchDirective parses @match(expr) { @case(pattern) { ... } @case(pattern if guard) { ... } @default { ... } }
func (p *parser) parseMatchDirective() (*MatchNode, error) {
	p.advanceN(6) // skip '@match'
	expr, err := p.readParenExpr()
	if err != nil {
		return nil, fmt.Errorf("@match: %w", err)
	}
	node := &MatchNode{Expr: expr}

	p.skipWhitespaceAndNewlines()
	if p.peek() != '{' {
		return nil, fmt.Errorf("@match: expected '{'")
	}
	p.advance() // skip outer '{'

	for {
		p.skipWhitespaceAndNewlines()
		if p.eof() {
			return nil, fmt.Errorf("@match: unclosed block")
		}
		if p.peek() == '}' {
			p.advance()
			break
		}
		if p.startsWith("@case") {
			p.advanceN(5)
			patternStr, err := p.readParenExpr()
			if err != nil {
				return nil, fmt.Errorf("@match @case: %w", err)
			}
			// Split pattern and guard on " if " (but not inside strings/parens)
			pattern, guard := splitPatternGuard(patternStr)
			p.skipWhitespaceAndNewlines()
			if p.peek() != '{' {
				return nil, fmt.Errorf("@match @case: expected '{'")
			}
			p.advance()
			body, err := p.parseNodes(true)
			if err != nil {
				return nil, fmt.Errorf("@match @case body: %w", err)
			}
			node.Cases = append(node.Cases, MatchCase{PatternExpr: pattern, Guard: guard, Body: body})
		} else if p.startsWith("@default") {
			p.advanceN(8)
			p.skipWhitespaceAndNewlines()
			if p.peek() != '{' {
				return nil, fmt.Errorf("@match @default: expected '{'")
			}
			p.advance()
			body, err := p.parseNodes(true)
			if err != nil {
				return nil, fmt.Errorf("@match @default body: %w", err)
			}
			node.Default = body
		} else {
			return nil, fmt.Errorf("@match: unexpected content, expected @case or @default")
		}
	}

	return node, nil
}

// splitPatternGuard splits a pattern string on top-level " if " to separate guard from pattern.
func splitPatternGuard(s string) (pattern, guard string) {
	depth := 0
	inStr := rune(0)
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		ch := runes[i]
		if inStr != 0 {
			if ch == inStr && (i == 0 || runes[i-1] != '\\') {
				inStr = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' || ch == '`' {
			inStr = ch
			continue
		}
		if ch == '(' || ch == '[' || ch == '{' {
			depth++
		} else if ch == ')' || ch == ']' || ch == '}' {
			depth--
		}
		if depth == 0 && ch == ' ' && i+4 <= len(runes) && string(runes[i:i+4]) == " if " {
			return strings.TrimSpace(string(runes[:i])), strings.TrimSpace(string(runes[i+4:]))
		}
	}
	return strings.TrimSpace(s), ""
}

// splitCaseValues splits case values by commas, respecting strings.
func splitCaseValues(s string) []string {
	var parts []string
	var buf strings.Builder
	depth := 0
	inStr := rune(0)
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		ch := runes[i]
		if inStr != 0 {
			buf.WriteRune(ch)
			if ch == '\\' && i+1 < len(runes) {
				i++
				buf.WriteRune(runes[i])
			} else if ch == inStr {
				inStr = 0
			}
			continue
		}
		switch ch {
		case '"', '\'':
			inStr = ch
			buf.WriteRune(ch)
		case '(', '{', '[':
			depth++
			buf.WriteRune(ch)
		case ')', '}', ']':
			depth--
			buf.WriteRune(ch)
		case ',':
			if depth == 0 {
				parts = append(parts, strings.TrimSpace(buf.String()))
				buf.Reset()
			} else {
				buf.WriteRune(ch)
			}
		default:
			buf.WriteRune(ch)
		}
	}
	if buf.Len() > 0 {
		parts = append(parts, strings.TrimSpace(buf.String()))
	}
	return parts
}

// parseRaw parses @raw { ... } — everything inside is literal text.
func (p *parser) parseRaw() (*RawNode, error) {
	p.advanceN(4) // skip '@raw'
	p.skipWhitespaceAndNewlines()
	if p.peek() != '{' {
		return nil, fmt.Errorf("@raw: expected '{'")
	}
	p.advance() // skip '{'
	var buf strings.Builder
	depth := 1
	for !p.eof() && depth > 0 {
		ch := p.peek()
		if ch == '{' {
			depth++
			buf.WriteRune(p.advance())
		} else if ch == '}' {
			depth--
			if depth == 0 {
				p.advance()
				break
			}
			buf.WriteRune(p.advance())
		} else {
			buf.WriteRune(p.advance())
		}
	}
	if depth != 0 {
		return nil, fmt.Errorf("@raw: unclosed block")
	}
	return &RawNode{Text: buf.String()}, nil
}

// parseInclude parses @include("path") or @include("path", dataExpr)
func (p *parser) parseInclude() (*IncludeNode, error) {
	p.advanceN(8) // skip '@include'
	inner, err := p.readParenExpr()
	if err != nil {
		return nil, fmt.Errorf("@include: %w", err)
	}
	parts := splitCaseValues(inner)
	if len(parts) == 0 {
		return nil, fmt.Errorf("@include: path is required")
	}
	path := unquote(strings.TrimSpace(parts[0]))
	node := &IncludeNode{Path: path}
	if len(parts) > 1 {
		node.DataExpr = strings.TrimSpace(parts[1])
	}
	return node, nil
}

// parseImportDirective parses @import("components.html")
func (p *parser) parseImportDirective() (*ImportNode, error) {
	p.advanceN(7) // skip '@import'
	inner, err := p.readParenExpr()
	if err != nil {
		return nil, fmt.Errorf("@import: %w", err)
	}
	path := unquote(strings.TrimSpace(inner))
	if path == "" {
		return nil, fmt.Errorf("@import: path is required")
	}
	return &ImportNode{Path: path}, nil
}

// parseHandlerDirective parses @handler(name = expr)
func (p *parser) parseHandlerDirective() (*HandlerNode, error) {
	p.advanceN(8) // skip '@handler'
	inner, err := p.readParenExpr()
	if err != nil {
		return nil, fmt.Errorf("@handler: %w", err)
	}
	trimmed := strings.TrimSpace(inner)
	if trimmed == "" {
		return nil, fmt.Errorf("@handler: handler name is required")
	}
	if idx := findFirstAssignEquals(inner); idx >= 0 {
		name := strings.TrimSpace(inner[:idx])
		expr := strings.TrimSpace(inner[idx+1:])
		if name == "" || expr == "" {
			return nil, fmt.Errorf("@handler: name and expression are required")
		}
		return &HandlerNode{Name: name, Expr: expr}, nil
	}
	p.skipWhitespaceAndNewlines()
	if p.peek() != '{' {
		return nil, fmt.Errorf("@handler: expected '{' for multiline handler body")
	}
	body, err := p.readRawBlock()
	if err != nil {
		return nil, fmt.Errorf("@handler body: %w", err)
	}
	return &HandlerNode{Name: trimmed, Body: strings.TrimSpace(body)}, nil
}

func (p *parser) readRawBlock() (string, error) {
	if p.peek() != '{' {
		return "", fmt.Errorf("expected '{'")
	}
	p.advance()
	var buf strings.Builder
	depth := 1
	for !p.eof() && depth > 0 {
		ch := p.peek()
		if ch == '{' {
			depth++
			buf.WriteRune(p.advance())
			continue
		}
		if ch == '}' {
			depth--
			if depth == 0 {
				p.advance()
				break
			}
			buf.WriteRune(p.advance())
			continue
		}
		// Skip // line comments — don't parse quotes inside them
		if ch == '/' && p.peekAt(1) == '/' {
			for !p.eof() && p.peek() != '\n' {
				buf.WriteRune(p.advance())
			}
			continue
		}
		if ch == '"' || ch == '\'' || ch == '`' {
			buf.WriteString(p.readStringLiteral())
			continue
		}
		buf.WriteRune(p.advance())
	}
	if depth != 0 {
		return "", fmt.Errorf("unclosed handler block")
	}
	return buf.String(), nil
}

// parseExtends parses @extends("layout.html")
func (p *parser) parseExtends() (*ExtendsNode, error) {
	p.advanceN(8) // skip '@extends'
	inner, err := p.readParenExpr()
	if err != nil {
		return nil, fmt.Errorf("@extends: %w", err)
	}
	return &ExtendsNode{Path: unquote(strings.TrimSpace(inner))}, nil
}

// parseBlock parses @block("name") { ... }
func (p *parser) parseBlock() (*BlockNode, error) {
	p.advanceN(6) // skip '@block'
	inner, err := p.readParenExpr()
	if err != nil {
		return nil, fmt.Errorf("@block: %w", err)
	}
	name := unquote(strings.TrimSpace(inner))
	p.skipWhitespaceAndNewlines()
	if p.peek() != '{' {
		return nil, fmt.Errorf("@block: expected '{'")
	}
	p.advance()
	body, err := p.parseNodes(true)
	if err != nil {
		return nil, fmt.Errorf("@block body: %w", err)
	}
	return &BlockNode{Name: name, Body: body}, nil
}

// parseDefine parses @define("name") { ... }
func (p *parser) parseDefine() (*DefineNode, error) {
	p.advanceN(7) // skip '@define'
	inner, err := p.readParenExpr()
	if err != nil {
		return nil, fmt.Errorf("@define: %w", err)
	}
	name := unquote(strings.TrimSpace(inner))
	p.skipWhitespaceAndNewlines()
	if p.peek() != '{' {
		return nil, fmt.Errorf("@define: expected '{'")
	}
	p.advance()
	body, err := p.parseNodes(true)
	if err != nil {
		return nil, fmt.Errorf("@define body: %w", err)
	}
	return &DefineNode{Name: name, Body: body}, nil
}

// parseComponent parses @component("Name") { ... } or @component("Name", prop1, prop2 as alias = default) { ... }
func (p *parser) parseComponent() (*ComponentNode, error) {
	p.advanceN(10) // skip '@component'
	inner, err := p.readParenExpr()
	if err != nil {
		return nil, fmt.Errorf("@component: %w", err)
	}

	// Parse name and optional prop declarations
	trimmed := strings.TrimSpace(inner)
	var name string
	var props []PropDef

	if len(trimmed) > 0 && (trimmed[0] == '"' || trimmed[0] == '\'') {
		// Find end of quoted name string
		quote := trimmed[0]
		end := 1
		for end < len(trimmed) {
			if trimmed[end] == '\\' {
				end += 2
				continue
			}
			if trimmed[end] == quote {
				end++
				break
			}
			end++
		}
		name = unquote(trimmed[:end])
		rest := strings.TrimSpace(trimmed[end:])
		// Parse comma-separated prop definitions after the name
		if len(rest) > 0 && rest[0] == ',' {
			propsPart := rest[1:]
			propTokens := splitCaseValues(propsPart)
			for _, tok := range propTokens {
				pd, err := parsePropDef(tok)
				if err != nil {
					return nil, fmt.Errorf("@component %q: %w", name, err)
				}
				props = append(props, pd)
			}
		}
	} else {
		name = trimmed
	}

	p.skipWhitespaceAndNewlines()
	if p.peek() != '{' {
		return nil, fmt.Errorf("@component: expected '{'")
	}
	p.advance()
	body, err := p.parseNodes(true)
	if err != nil {
		return nil, fmt.Errorf("@component body: %w", err)
	}
	return &ComponentNode{Name: name, Props: props, Body: body}, nil
}

// parsePropDef parses a single prop definition token: "name", "name as alias", "name = default", "name as alias = default"
func parsePropDef(token string) (PropDef, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return PropDef{}, fmt.Errorf("empty prop definition")
	}

	pd := PropDef{}

	// Split on first assignment '=' to separate name/alias from default
	eqIdx := findFirstAssignEquals(token)
	var namePart string
	if eqIdx >= 0 {
		namePart = strings.TrimSpace(token[:eqIdx])
		pd.Default = strings.TrimSpace(token[eqIdx+1:])
	} else {
		namePart = token
	}

	// Check for " as " alias
	asIdx := strings.Index(namePart, " as ")
	if asIdx >= 0 {
		pd.Name = strings.TrimSpace(namePart[:asIdx])
		pd.Alias = strings.TrimSpace(namePart[asIdx+4:])
	} else {
		pd.Name = strings.TrimSpace(namePart)
	}

	if pd.Name == "" {
		return PropDef{}, fmt.Errorf("prop name is required")
	}
	return pd, nil
}

// findFirstAssignEquals finds the index of the first assignment '=' that is not part of ==, !=, <=, >=.
// Returns -1 if not found. Respects string literals and bracket depth.
func findFirstAssignEquals(s string) int {
	runes := []rune(s)
	inStr := rune(0)
	depth := 0
	for i := 0; i < len(runes); i++ {
		ch := runes[i]
		if inStr != 0 {
			if ch == '\\' && i+1 < len(runes) {
				i++
			} else if ch == inStr {
				inStr = 0
			}
			continue
		}
		switch ch {
		case '"', '\'', '`':
			inStr = ch
		case '(', '{', '[':
			depth++
		case ')', '}', ']':
			depth--
		case '!':
			if i+1 < len(runes) && runes[i+1] == '=' {
				i++ // skip !=
			}
		case '<', '>':
			if i+1 < len(runes) && runes[i+1] == '=' {
				i++ // skip <=, >=
			}
		case '=':
			if depth == 0 {
				if i+1 < len(runes) && runes[i+1] == '=' {
					i++ // skip ==
					continue
				}
				return i
			}
		}
	}
	return -1
}

// parseRender parses @render("Name") { ... } or @render("Name", propsExpr) { ... }
func (p *parser) parseRender() (*RenderNode, error) {
	p.advanceN(7) // skip '@render'
	inner, err := p.readParenExpr()
	if err != nil {
		return nil, fmt.Errorf("@render: %w", err)
	}

	// Split into name and optional props expression
	// Use splitCaseValues-like logic but only split on first comma after the name string
	trimmed := strings.TrimSpace(inner)
	var name, propsExpr string

	if len(trimmed) > 0 && (trimmed[0] == '"' || trimmed[0] == '\'') {
		// Find end of quoted string
		quote := trimmed[0]
		end := 1
		for end < len(trimmed) {
			if trimmed[end] == '\\' {
				end += 2
				continue
			}
			if trimmed[end] == quote {
				end++
				break
			}
			end++
		}
		name = unquote(trimmed[:end])
		rest := strings.TrimSpace(trimmed[end:])
		if len(rest) > 0 && rest[0] == ',' {
			propsExpr = strings.TrimSpace(rest[1:])
		}
	} else {
		// Unquoted — treat as name (no props)
		name = trimmed
	}

	p.skipWhitespaceAndNewlines()
	var children []Node
	if p.peek() == '{' {
		p.advance()
		children, err = p.parseNodes(true)
		if err != nil {
			return nil, fmt.Errorf("@render body: %w", err)
		}
	}

	return &RenderNode{Name: name, PropsExpr: propsExpr, Children: children}, nil
}

// parseSlot parses @slot or @slot("name")
func (p *parser) parseSlot() (*SlotNode, error) {
	p.advanceN(5) // skip '@slot'
	p.skipWhitespace()
	var name string
	if p.peek() == '(' {
		inner, err := p.readParenExpr()
		if err != nil {
			return nil, fmt.Errorf("@slot: %w", err)
		}
		name = unquote(strings.TrimSpace(inner))
	}
	return &SlotNode{Name: name}, nil
}

// parseFill parses @fill("name") { ... }
func (p *parser) parseFill() (*FillNode, error) {
	p.advanceN(5) // skip '@fill'
	inner, err := p.readParenExpr()
	if err != nil {
		return nil, fmt.Errorf("@fill: %w", err)
	}
	name := unquote(strings.TrimSpace(inner))
	p.skipWhitespaceAndNewlines()
	if p.peek() != '{' {
		return nil, fmt.Errorf("@fill: expected '{'")
	}
	p.advance()
	body, err := p.parseNodes(true)
	if err != nil {
		return nil, fmt.Errorf("@fill body: %w", err)
	}
	return &FillNode{Name: name, Body: body}, nil
}

// parseLet parses @let(varName = expr)
func (p *parser) parseLet() (*LetNode, error) {
	p.advanceN(4) // skip '@let'
	inner, err := p.readParenExpr()
	if err != nil {
		return nil, fmt.Errorf("@let: %w", err)
	}
	idx := findFirstAssignEquals(inner)
	if idx < 0 {
		return nil, fmt.Errorf("@let: expected 'varName = expr' syntax")
	}
	varName := strings.TrimSpace(inner[:idx])
	expr := strings.TrimSpace(inner[idx+1:])
	if varName == "" || expr == "" {
		return nil, fmt.Errorf("@let: variable name and expression are required")
	}
	return &LetNode{VarName: varName, Expr: expr}, nil
}

// parseComputed parses @computed(varName = expr)
func (p *parser) parseComputed() (*ComputedNode, error) {
	p.advanceN(9) // skip '@computed'
	inner, err := p.readParenExpr()
	if err != nil {
		return nil, fmt.Errorf("@computed: %w", err)
	}
	idx := findFirstAssignEquals(inner)
	if idx < 0 {
		return nil, fmt.Errorf("@computed: expected 'varName = expr' syntax")
	}
	varName := strings.TrimSpace(inner[:idx])
	expr := strings.TrimSpace(inner[idx+1:])
	if varName == "" || expr == "" {
		return nil, fmt.Errorf("@computed: variable name and expression are required")
	}
	return &ComputedNode{VarName: varName, Expr: expr}, nil
}

// parseWatch parses @watch(expr) { body }
func (p *parser) parseWatch() (*WatchNode, error) {
	p.advanceN(6) // skip '@watch'
	expr, err := p.readParenExpr()
	if err != nil {
		return nil, fmt.Errorf("@watch: %w", err)
	}
	p.skipWhitespaceAndNewlines()
	if p.peek() != '{' {
		return nil, fmt.Errorf("@watch: expected '{'")
	}
	p.advance()
	body, err := p.parseNodes(true)
	if err != nil {
		return nil, fmt.Errorf("@watch body: %w", err)
	}
	return &WatchNode{Expr: strings.TrimSpace(expr), Body: body}, nil
}

// parseSignal parses @signal(name = expr)
func (p *parser) parseSignal() (*SignalNode, error) {
	p.advanceN(7) // skip '@signal'
	inner, err := p.readParenExpr()
	if err != nil {
		return nil, fmt.Errorf("@signal: %w", err)
	}
	idx := findFirstAssignEquals(inner)
	if idx < 0 {
		return nil, fmt.Errorf("@signal: expected 'name = expr' syntax")
	}
	name := strings.TrimSpace(inner[:idx])
	expr := strings.TrimSpace(inner[idx+1:])
	if name == "" || expr == "" {
		return nil, fmt.Errorf("@signal: name and expression are required")
	}
	return &SignalNode{Name: name, InitialExpr: expr}, nil
}

// parseBind parses @bind(signal) or @bind(signal, attr)
func (p *parser) parseBind() (*BindNode, error) {
	p.advanceN(5) // skip '@bind'
	inner, err := p.readParenExpr()
	if err != nil {
		return nil, fmt.Errorf("@bind: %w", err)
	}
	parts := splitCaseValues(inner)
	if len(parts) == 0 {
		return nil, fmt.Errorf("@bind: signal is required")
	}
	signal := strings.TrimSpace(parts[0])
	attr := "textContent"
	if len(parts) > 1 {
		attr = unquote(strings.TrimSpace(parts[1]))
	}
	if signal == "" {
		return nil, fmt.Errorf("@bind: signal is required")
	}
	return &BindNode{Signal: signal, Attr: attr}, nil
}

// parseEffect parses @effect(dep1, dep2) { ... }
func (p *parser) parseEffect() (*EffectNode, error) {
	p.advanceN(7) // skip '@effect'
	inner, err := p.readParenExpr()
	if err != nil {
		return nil, fmt.Errorf("@effect: %w", err)
	}
	deps := splitCaseValues(inner)
	for i := range deps {
		deps[i] = strings.TrimSpace(deps[i])
	}
	p.skipWhitespaceAndNewlines()
	if p.peek() != '{' {
		return nil, fmt.Errorf("@effect: expected '{'")
	}
	p.advance()
	body, err := p.parseNodes(true)
	if err != nil {
		return nil, fmt.Errorf("@effect body: %w", err)
	}
	return &EffectNode{Deps: deps, Body: body}, nil
}

// parseReactiveView parses @reactive(dep1, dep2) { ... }
func (p *parser) parseReactiveView() (*ReactiveViewNode, error) {
	p.advanceN(9) // skip '@reactive'
	inner, err := p.readParenExpr()
	if err != nil {
		return nil, fmt.Errorf("@reactive: %w", err)
	}
	deps := splitCaseValues(inner)
	for i := range deps {
		deps[i] = strings.TrimSpace(deps[i])
	}
	p.skipWhitespaceAndNewlines()
	if p.peek() != '{' {
		return nil, fmt.Errorf("@reactive: expected '{'")
	}
	p.advance()
	body, err := p.parseNodes(true)
	if err != nil {
		return nil, fmt.Errorf("@reactive body: %w", err)
	}
	return &ReactiveViewNode{Deps: deps, Body: body}, nil
}

// parseClick parses @click(label, signal, action, value?)
func (p *parser) parseClick() (*ClickNode, error) {
	p.advanceN(6) // skip '@click'
	inner, err := p.readParenExpr()
	if err != nil {
		return nil, fmt.Errorf("@click: %w", err)
	}
	parts := splitCaseValues(inner)
	if len(parts) < 3 {
		return nil, fmt.Errorf("@click: expected label, signal, action")
	}
	node := &ClickNode{
		Label:  unquote(strings.TrimSpace(parts[0])),
		Signal: strings.TrimSpace(parts[1]),
		Action: unquote(strings.TrimSpace(parts[2])),
	}
	if len(parts) > 3 {
		node.Value = unquote(strings.TrimSpace(parts[3]))
	}
	if node.Label == "" || node.Signal == "" || node.Action == "" {
		return nil, fmt.Errorf("@click: label, signal, and action are required")
	}
	return node, nil
}

// parseStream parses @stream { ... }
func (p *parser) parseStream() (*StreamNode, error) {
	p.advanceN(7) // skip '@stream'
	p.skipWhitespaceAndNewlines()
	if p.peek() != '{' {
		return nil, fmt.Errorf("@stream: expected '{'")
	}
	p.advance()
	body, err := p.parseNodes(true)
	if err != nil {
		return nil, fmt.Errorf("@stream body: %w", err)
	}
	return &StreamNode{Body: body}, nil
}

// parseDefer parses @defer { ... } [@fallback { ... }]
func (p *parser) parseDefer() (*DeferNode, error) {
	p.advanceN(6) // skip '@defer'
	p.skipWhitespaceAndNewlines()
	if p.peek() != '{' {
		return nil, fmt.Errorf("@defer: expected '{'")
	}
	p.advance()
	body, err := p.parseNodes(true)
	if err != nil {
		return nil, fmt.Errorf("@defer body: %w", err)
	}
	p.skipWhitespaceAndNewlines()
	var fallback []Node
	if p.startsWith("@fallback") {
		p.advanceN(9)
		p.skipWhitespaceAndNewlines()
		if p.peek() != '{' {
			return nil, fmt.Errorf("@fallback: expected '{'")
		}
		p.advance()
		fallback, err = p.parseNodes(true)
		if err != nil {
			return nil, fmt.Errorf("@fallback body: %w", err)
		}
	}
	return &DeferNode{Body: body, Fallback: fallback}, nil
}

// parseLazy parses @lazy(expr) { ... } [@fallback { ... }]
func (p *parser) parseLazy() (*LazyNode, error) {
	p.advanceN(5) // skip '@lazy'
	expr, err := p.readParenExpr()
	if err != nil {
		return nil, fmt.Errorf("@lazy: %w", err)
	}
	p.skipWhitespaceAndNewlines()
	if p.peek() != '{' {
		return nil, fmt.Errorf("@lazy: expected '{'")
	}
	p.advance()
	body, err := p.parseNodes(true)
	if err != nil {
		return nil, fmt.Errorf("@lazy body: %w", err)
	}
	p.skipWhitespaceAndNewlines()
	var fallback []Node
	if p.startsWith("@fallback") {
		p.advanceN(9)
		p.skipWhitespaceAndNewlines()
		if p.peek() != '{' {
			return nil, fmt.Errorf("@fallback: expected '{'")
		}
		p.advance()
		fallback, err = p.parseNodes(true)
		if err != nil {
			return nil, fmt.Errorf("@fallback body: %w", err)
		}
	}
	return &LazyNode{Expr: strings.TrimSpace(expr), Body: body, Fallback: fallback}, nil
}

// readParenExpr reads content between matching '(' and ')'.
func (p *parser) readParenExpr() (string, error) {
	p.skipWhitespaceAndNewlines()
	if p.peek() != '(' {
		return "", fmt.Errorf("expected '('")
	}
	p.advance() // skip '('
	var buf strings.Builder
	depth := 1
	for !p.eof() && depth > 0 {
		ch := p.peek()
		if ch == '(' {
			depth++
			buf.WriteRune(p.advance())
		} else if ch == ')' {
			depth--
			if depth == 0 {
				p.advance()
				break
			}
			buf.WriteRune(p.advance())
		} else if ch == '"' || ch == '\'' {
			buf.WriteString(p.readStringLiteral())
		} else {
			buf.WriteRune(p.advance())
		}
	}
	if depth != 0 {
		return "", fmt.Errorf("unclosed '('")
	}
	return buf.String(), nil
}

func (p *parser) skipWhitespaceAndNewlines() {
	for !p.eof() {
		ch := p.peek()
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			p.advance()
		} else {
			break
		}
	}
}

// unquote removes surrounding quotes from a string if present.
func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
