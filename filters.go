package template

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// registerBuiltinFilters installs all built-in filters into the engine.
func registerBuiltinFilters(e *Engine) {
	e.Filters["upper"] = filterUpper
	e.Filters["lower"] = filterLower
	e.Filters["trim"] = filterTrim
	e.Filters["title"] = filterTitle
	e.Filters["capitalize"] = filterCapitalize
	e.Filters["escape"] = filterEscape
	e.Filters["json"] = filterJSON
	e.Filters["format"] = filterFormat
	e.Filters["default"] = filterDefault
	e.Filters["join"] = filterJoin
	e.Filters["first"] = filterFirst
	e.Filters["last"] = filterLast
	e.Filters["length"] = filterLength
	e.Filters["reverse"] = filterReverse
	e.Filters["truncate"] = filterTruncate
	e.Filters["nl2br"] = filterNl2br
	e.Filters["urlencode"] = filterUrlencode
	e.Filters["striptags"] = filterStriptags
	e.Filters["slug"] = filterSlug
	e.Filters["replace"] = filterReplace
	e.Filters["split"] = filterSplit
	e.Filters["repeat"] = filterRepeat
	e.Filters["padstart"] = filterPadStart
	e.Filters["padend"] = filterPadEnd
	e.Filters["wrap"] = filterWrap
}

func filterUpper(val any, args ...string) string {
	return strings.ToUpper(str(val))
}

func filterLower(val any, args ...string) string {
	return strings.ToLower(str(val))
}

func filterTrim(val any, args ...string) string {
	return strings.TrimSpace(str(val))
}

func filterTitle(val any, args ...string) string {
	s := str(val)
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 0 {
			runes := []rune(w)
			runes[0] = unicode.ToUpper(runes[0])
			words[i] = string(runes)
		}
	}
	return strings.Join(words, " ")
}

func filterCapitalize(val any, args ...string) string {
	s := str(val)
	if len(s) == 0 {
		return s
	}
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

func filterEscape(val any, args ...string) string {
	return html.EscapeString(str(val))
}

func filterJSON(val any, args ...string) string {
	b, err := json.Marshal(val)
	if err != nil {
		return str(val)
	}
	return string(b)
}

func filterFormat(val any, args ...string) string {
	if len(args) == 0 {
		return str(val)
	}
	format := args[0]
	// When val is a string (common in template pipelines where objectToString
	// runs before filters), try to parse it as a number for numeric format verbs
	// like %f, %e, %g, %d, %x, etc. This avoids Go's "%!f(string=...)" output.
	if s, ok := val.(string); ok && hasNumericVerb(format) {
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return fmt.Sprintf(format, f)
		}
		if i, err := strconv.ParseInt(s, 10, 64); err == nil {
			return fmt.Sprintf(format, i)
		}
	}
	return fmt.Sprintf(format, val)
}

// hasNumericVerb returns true if the format string contains a numeric verb
// like %d, %f, %e, %g, %x, %o, %b that expects a number argument.
func hasNumericVerb(format string) bool {
	for i := 0; i < len(format)-1; i++ {
		if format[i] == '%' {
			i++
			// skip flags, width, precision
			for i < len(format) && (format[i] == '-' || format[i] == '+' || format[i] == ' ' || format[i] == '0' || format[i] == '#' || format[i] == '.' || (format[i] >= '0' && format[i] <= '9')) {
				i++
			}
			if i < len(format) {
				switch format[i] {
				case 'd', 'f', 'F', 'e', 'E', 'g', 'G', 'x', 'X', 'o', 'O', 'b':
					return true
				case '%':
					continue
				}
			}
		}
	}
	return false
}

func filterDefault(val any, args ...string) string {
	s := str(val)
	if s == "" {
		if len(args) > 0 {
			return args[0]
		}
		return ""
	}
	return s
}

func filterJoin(val any, args ...string) string {
	sep := ", "
	if len(args) > 0 {
		sep = args[0]
	}
	s := str(val)
	// If the value looks like a JSON array or comma-separated, just return as-is joined with sep
	// In practice, the filter will receive the string representation of the array
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	parts := strings.Split(s, ", ")
	return strings.Join(parts, sep)
}

func filterFirst(val any, args ...string) string {
	s := str(val)
	if len(s) == 0 {
		return ""
	}
	return string([]rune(s)[0])
}

func filterLast(val any, args ...string) string {
	s := str(val)
	r := []rune(s)
	if len(r) == 0 {
		return ""
	}
	return string(r[len(r)-1])
}

func filterLength(val any, args ...string) string {
	return strconv.Itoa(len([]rune(str(val))))
}

func filterReverse(val any, args ...string) string {
	runes := []rune(str(val))
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}

func filterTruncate(val any, args ...string) string {
	s := str(val)
	n := 50
	suffix := "..."
	if len(args) > 0 {
		if v, err := strconv.Atoi(args[0]); err == nil {
			n = v
		}
	}
	if len(args) > 1 {
		suffix = args[1]
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + suffix
}

func filterNl2br(val any, args ...string) string {
	s := str(val)
	s = strings.ReplaceAll(s, "\r\n", "<br>")
	s = strings.ReplaceAll(s, "\n", "<br>")
	return s
}

func filterUrlencode(val any, args ...string) string {
	return url.QueryEscape(str(val))
}

var tagRe = regexp.MustCompile(`<[^>]*>`)

func filterStriptags(val any, args ...string) string {
	return tagRe.ReplaceAllString(str(val), "")
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func filterSlug(val any, args ...string) string {
	s := strings.ToLower(strings.TrimSpace(str(val)))
	s = slugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}

func filterReplace(val any, args ...string) string {
	s := str(val)
	if len(args) >= 2 {
		return strings.ReplaceAll(s, args[0], args[1])
	}
	return s
}

func filterSplit(val any, args ...string) string {
	sep := ","
	if len(args) > 0 {
		sep = args[0]
	}
	parts := strings.Split(str(val), sep)
	b, _ := json.Marshal(parts)
	return string(b)
}

func filterRepeat(val any, args ...string) string {
	n := 1
	if len(args) > 0 {
		if v, err := strconv.Atoi(args[0]); err == nil {
			n = v
		}
	}
	return strings.Repeat(str(val), n)
}

func filterPadStart(val any, args ...string) string {
	s := str(val)
	n := 0
	pad := " "
	if len(args) > 0 {
		if v, err := strconv.Atoi(args[0]); err == nil {
			n = v
		}
	}
	if len(args) > 1 {
		pad = args[1]
	}
	for len([]rune(s)) < n {
		s = pad + s
	}
	return s
}

func filterPadEnd(val any, args ...string) string {
	s := str(val)
	n := 0
	pad := " "
	if len(args) > 0 {
		if v, err := strconv.Atoi(args[0]); err == nil {
			n = v
		}
	}
	if len(args) > 1 {
		pad = args[1]
	}
	for len([]rune(s)) < n {
		s = s + pad
	}
	return s
}

func filterWrap(val any, args ...string) string {
	s := str(val)
	before := ""
	after := ""
	if len(args) > 0 {
		before = args[0]
	}
	if len(args) > 1 {
		after = args[1]
	} else {
		after = before
	}
	return before + s + after
}

// str converts any value to a string, optimized for common types.
func str(val any) string {
	if val == nil {
		return ""
	}
	switch v := val.(type) {
	case string:
		return v
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64)
	case bool:
		if v {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", val)
	}
}
