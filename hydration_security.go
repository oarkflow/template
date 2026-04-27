package template

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
)

type clientAction struct {
	Kind   string `json:"kind"`
	Target string `json:"target"`
	Value  any    `json:"value,omitempty"`
}

type compiledEventSpec struct {
	Delay   int            `json:"delay,omitempty"`
	Handler string         `json:"handler,omitempty"`
	Actions []clientAction `json:"actions,omitempty"`
}

var (
	safeSignalPathRe     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*$`)
	safeHandlerRefRe     = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)
	hydrationEventAttrRe = regexp.MustCompile(`data-spl-on-([a-zA-Z][a-zA-Z0-9_-]*)="([^"]*)"`)
	hydrationBindAttrRe  = regexp.MustCompile(`data-spl-bind-([a-zA-Z_][a-zA-Z0-9_]*)="([^"]*)"`)
)

func secureHydrationOutput(renderedHTML string, state *hydrationState) (string, error) {
	compiledHandlers := make(map[string][]clientAction, len(state.Handlers))
	for name, body := range state.Handlers {
		actions, err := compileClientActions(body)
		if err != nil {
			return "", fmt.Errorf("@handler(%s): %w", name, err)
		}
		compiledHandlers[name] = actions
	}

	rewrittenHTML, err := rewriteHydrationMarkup(renderedHTML, compiledHandlers)
	if err != nil {
		return "", err
	}
	for i := range state.Effects {
		state.Effects[i].Source, err = rewriteHydrationMarkup(state.Effects[i].Source, compiledHandlers)
		if err != nil {
			return "", fmt.Errorf("@effect hydration: %w", err)
		}
	}
	for i := range state.Views {
		state.Views[i].Source, err = rewriteHydrationMarkup(state.Views[i].Source, compiledHandlers)
		if err != nil {
			return "", fmt.Errorf("@reactive hydration: %w", err)
		}
	}
	state.CompiledHandlers = compiledHandlers
	return rewrittenHTML, nil
}

func rewriteHydrationMarkup(markup string, handlers map[string][]clientAction) (string, error) {
	var rewriteErr error
	markup = hydrationBindAttrRe.ReplaceAllStringFunc(markup, func(match string) string {
		if rewriteErr != nil {
			return match
		}
		parts := hydrationBindAttrRe.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		expr := html.UnescapeString(parts[2])
		if !safeSignalPathRe.MatchString(strings.TrimSpace(expr)) {
			rewriteErr = fmt.Errorf("unsafe bind expression %q", expr)
			return match
		}
		return fmt.Sprintf(`data-spl-bind-%s="%s"`, parts[1], html.EscapeString(strings.TrimSpace(expr)))
	})
	if rewriteErr != nil {
		return "", rewriteErr
	}

	markup = hydrationEventAttrRe.ReplaceAllStringFunc(markup, func(match string) string {
		if rewriteErr != nil {
			return match
		}
		parts := hydrationEventAttrRe.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		spec := html.UnescapeString(parts[2])
		compiled, err := compileEventSpec(spec, handlers)
		if err != nil {
			rewriteErr = fmt.Errorf("unsafe event expression %q: %w", spec, err)
			return match
		}
		return fmt.Sprintf(`data-spl-on-%s="%s"`, parts[1], html.EscapeString(compiled))
	})
	if rewriteErr != nil {
		return "", rewriteErr
	}
	return markup, nil
}

func compileEventSpec(spec string, handlers map[string][]clientAction) (string, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", fmt.Errorf("empty event expression")
	}
	if wrapped, ok, err := compileDebouncedEventSpec(spec, handlers); err != nil {
		return "", err
	} else if ok {
		encoded, err := json.Marshal(wrapped)
		if err != nil {
			return "", err
		}
		return string(encoded), nil
	}
	if safeHandlerRefRe.MatchString(spec) {
		if _, ok := handlers[spec]; ok {
			return spec, nil
		}
	}
	actions, err := compileClientActions(spec)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(actions)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func compileDebouncedEventSpec(spec string, handlers map[string][]clientAction) (compiledEventSpec, bool, error) {
	spec = strings.TrimSpace(spec)
	if !strings.HasPrefix(spec, "debounce(") || !strings.HasSuffix(spec, ")") {
		return compiledEventSpec{}, false, nil
	}
	args := splitCallArgs(spec[len("debounce(") : len(spec)-1])
	if len(args) != 2 {
		return compiledEventSpec{}, true, fmt.Errorf("debounce expects exactly 2 arguments")
	}
	inner := strings.TrimSpace(args[0])
	if inner == "" {
		return compiledEventSpec{}, true, fmt.Errorf("debounce requires a non-empty event expression")
	}
	delay, err := parseClientDelay(args[1])
	if err != nil {
		return compiledEventSpec{}, true, err
	}
	wrapped := compiledEventSpec{Delay: delay}
	if safeHandlerRefRe.MatchString(inner) {
		if _, ok := handlers[inner]; ok {
			wrapped.Handler = inner
			return wrapped, true, nil
		}
	}
	actions, err := compileClientActions(inner)
	if err != nil {
		return compiledEventSpec{}, true, err
	}
	wrapped.Actions = actions
	return wrapped, true, nil
}

func compileClientActions(src string) ([]clientAction, error) {
	statements := splitClientStatements(src)
	if len(statements) == 0 {
		return nil, fmt.Errorf("no supported actions found")
	}
	actions := make([]clientAction, 0, len(statements))
	for _, stmt := range statements {
		action, err := compileClientStatement(stmt)
		if err != nil {
			return nil, err
		}
		actions = append(actions, action)
	}
	return actions, nil
}

func splitClientStatements(src string) []string {
	var result []string
	var sb strings.Builder
	var quote rune
	escaped := false
	depth := 0
	for _, r := range src {
		if quote != 0 {
			sb.WriteRune(r)
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == quote {
				quote = 0
			}
			continue
		}
		switch r {
		case '\'', '"':
			quote = r
			sb.WriteRune(r)
		case '(':
			depth++
			sb.WriteRune(r)
		case ')':
			if depth > 0 {
				depth--
			}
			sb.WriteRune(r)
		case ';':
			if depth == 0 {
				if stmt := strings.TrimSpace(sb.String()); stmt != "" {
					result = append(result, stmt)
				}
				sb.Reset()
				continue
			}
			sb.WriteRune(r)
		case '\n', '\r':
			if depth == 0 {
				if stmt := strings.TrimSpace(sb.String()); stmt != "" {
					result = append(result, stmt)
				}
				sb.Reset()
				continue
			}
			sb.WriteRune(' ')
		default:
			sb.WriteRune(r)
		}
	}
	if stmt := strings.TrimSpace(sb.String()); stmt != "" {
		result = append(result, stmt)
	}
	return result
}

func compileClientStatement(stmt string) (clientAction, error) {
	stmt = strings.TrimSpace(stmt)
	if stmt == "" {
		return clientAction{}, fmt.Errorf("empty action")
	}
	if strings.HasPrefix(stmt, "toggle(") && strings.HasSuffix(stmt, ")") {
		target := strings.TrimSpace(stmt[len("toggle(") : len(stmt)-1])
		if !safeSignalPathRe.MatchString(target) {
			return clientAction{}, fmt.Errorf("unsupported toggle target %q", target)
		}
		return clientAction{Kind: "toggle", Target: target}, nil
	}
	if strings.HasPrefix(stmt, "setSignal(") && strings.HasSuffix(stmt, ")") {
		args := splitCallArgs(stmt[len("setSignal(") : len(stmt)-1])
		if len(args) != 2 {
			return clientAction{}, fmt.Errorf("setSignal expects exactly 2 arguments")
		}
		target := strings.TrimSpace(args[0])
		if !safeSignalPathRe.MatchString(target) {
			return clientAction{}, fmt.Errorf("unsupported setSignal target %q", target)
		}
		value, err := parseClientLiteral(args[1])
		if err != nil {
			return clientAction{}, err
		}
		return clientAction{Kind: "set", Target: target, Value: value}, nil
	}
	if idx := strings.Index(stmt, "+="); idx >= 0 {
		target := strings.TrimSpace(stmt[:idx])
		if !safeSignalPathRe.MatchString(target) {
			return clientAction{}, fmt.Errorf("unsupported add target %q", target)
		}
		value, err := parseClientNumber(stmt[idx+2:])
		if err != nil {
			return clientAction{}, err
		}
		return clientAction{Kind: "add", Target: target, Value: value}, nil
	}
	if idx := strings.Index(stmt, "-="); idx >= 0 {
		target := strings.TrimSpace(stmt[:idx])
		if !safeSignalPathRe.MatchString(target) {
			return clientAction{}, fmt.Errorf("unsupported subtract target %q", target)
		}
		value, err := parseClientNumber(stmt[idx+2:])
		if err != nil {
			return clientAction{}, err
		}
		return clientAction{Kind: "sub", Target: target, Value: value}, nil
	}
	if idx := strings.Index(stmt, "="); idx >= 0 && !strings.Contains(stmt[:idx], "==") {
		target := strings.TrimSpace(stmt[:idx])
		if !safeSignalPathRe.MatchString(target) {
			return clientAction{}, fmt.Errorf("unsupported assignment target %q", target)
		}
		value, err := parseClientLiteral(stmt[idx+1:])
		if err != nil {
			return clientAction{}, err
		}
		return clientAction{Kind: "set", Target: target, Value: value}, nil
	}
	return clientAction{}, fmt.Errorf("unsupported client action %q", stmt)
}

func splitCallArgs(raw string) []string {
	var args []string
	var sb strings.Builder
	var quote rune
	escaped := false
	depth := 0
	for _, r := range raw {
		if quote != 0 {
			sb.WriteRune(r)
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == quote {
				quote = 0
			}
			continue
		}
		switch r {
		case '\'', '"':
			quote = r
			sb.WriteRune(r)
		case '(':
			depth++
			sb.WriteRune(r)
		case ')':
			if depth > 0 {
				depth--
			}
			sb.WriteRune(r)
		case ',':
			if depth == 0 {
				args = append(args, strings.TrimSpace(sb.String()))
				sb.Reset()
				continue
			}
			sb.WriteRune(r)
		default:
			sb.WriteRune(r)
		}
	}
	if tail := strings.TrimSpace(sb.String()); tail != "" {
		args = append(args, tail)
	}
	return args
}

func parseClientNumber(raw string) (any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("missing numeric value")
	}
	if i, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return i, nil
	}
	if f, err := strconv.ParseFloat(raw, 64); err == nil {
		return f, nil
	}
	return nil, fmt.Errorf("unsupported numeric literal %q", raw)
}

func parseClientDelay(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("missing debounce delay")
	}
	delay, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("unsupported debounce delay %q", raw)
	}
	if delay < 0 {
		return 0, fmt.Errorf("debounce delay must be non-negative")
	}
	return delay, nil
}

func parseClientLiteral(raw string) (any, error) {
	raw = strings.TrimSpace(raw)
	switch raw {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "null":
		return nil, nil
	}
	if raw == "" {
		return nil, fmt.Errorf("missing literal value")
	}
	if (strings.HasPrefix(raw, "'") && strings.HasSuffix(raw, "'")) || (strings.HasPrefix(raw, `"`) && strings.HasSuffix(raw, `"`)) {
		return unquoteClientString(raw)
	}
	if n, err := parseClientNumber(raw); err == nil {
		return n, nil
	}
	return nil, fmt.Errorf("unsupported literal %q", raw)
}

func unquoteClientString(raw string) (string, error) {
	if strings.HasPrefix(raw, "'") && strings.HasSuffix(raw, "'") {
		raw = `"` + strings.ReplaceAll(raw[1:len(raw)-1], `"`, `\"`) + `"`
	} else if !(strings.HasPrefix(raw, `"`) && strings.HasSuffix(raw, `"`)) {
		return "", fmt.Errorf("unsupported string literal %q", raw)
	}
	value, err := strconv.Unquote(raw)
	if err != nil {
		return "", fmt.Errorf("invalid string literal %q", raw)
	}
	return value, nil
}
