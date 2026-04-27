package template

import (
	"fmt"
	"regexp"
)

var forbiddenHTMLPatterns = []struct {
	reason string
	re     *regexp.Regexp
}{
	{reason: "script tags are not allowed in secure mode", re: regexp.MustCompile(`(?i)<script\b`)},
	{reason: "inline event handlers are not allowed in secure mode", re: regexp.MustCompile(`(?i)\son[a-z0-9_-]+\s*=`)},
	{reason: "javascript: URLs are not allowed in secure mode", re: regexp.MustCompile(`(?i)\b(?:href|src|xlink:href|formaction)\s*=\s*(['"])?\s*javascript:`)},
	{reason: "srcdoc is not allowed in secure mode", re: regexp.MustCompile(`(?i)\ssrcdoc\s*=`)},
	{reason: "active embedded content is not allowed in secure mode", re: regexp.MustCompile(`(?i)<(?:iframe|object|embed)\b|<meta\b[^>]*http-equiv\s*=\s*(['"])?refresh\b`)},
}

func (e *Engine) ensureSecureRenderedHTML(rendered string) error {
	if !e.SecureMode {
		return nil
	}
	for _, pattern := range forbiddenHTMLPatterns {
		if pattern.re.FindStringIndex(rendered) != nil {
			return fmt.Errorf("%s", pattern.reason)
		}
	}
	return nil
}
