package team

// sandbox_html_policy.go is the single write-time HTML sandbox validator shared
// by rich artifacts and Apps. The two are identical except for which elements
// are blocked (Apps permit <form>) and the error label/constructor, so the walk,
// attribute checks, URL checks, and CSS checks live here once. Keeping one copy
// of a security boundary means a future tightening can't be applied to one and
// forgotten on the other.

import (
	"errors"
	"io"
	"strings"

	"golang.org/x/net/html"
)

// sandboxHTMLPolicy parameterizes the shared validator. blockedElement decides
// per-tag rejection (the one real divergence: <form>), label + newErr shape the
// caller-facing error. The error messages preserve the substrings asserted by
// existing tests ("external script src is not allowed", "css @import is not
// allowed").
type sandboxHTMLPolicy struct {
	label          string
	blockedElement func(tag string) (string, bool)
	newErr         func(format string, args ...any) error
}

func validateSandboxHTML(raw string, p sandboxHTMLPolicy) error {
	tokenizer := html.NewTokenizer(strings.NewReader(raw))
	styleDepth := 0
	for {
		tt := tokenizer.Next()
		switch tt {
		case html.ErrorToken:
			if errors.Is(tokenizer.Err(), io.EOF) {
				return nil
			}
			return p.newErr("%s: html parse failed: %v", p.label, tokenizer.Err())
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokenizer.Token()
			tag := strings.ToLower(token.Data)
			if reason, blocked := p.blockedElement(tag); blocked {
				return p.newErr("%s: html element <%s> is not allowed: %s", p.label, tag, reason)
			}
			if tag == "style" && tt == html.StartTagToken {
				styleDepth++
			}
			if err := validateSandboxAttributes(tag, token.Attr, p); err != nil {
				return err
			}
		case html.EndTagToken:
			token := tokenizer.Token()
			if strings.EqualFold(token.Data, "style") && styleDepth > 0 {
				styleDepth--
			}
		case html.TextToken:
			if styleDepth > 0 {
				if err := validateSandboxCSS(tokenizer.Token().Data, p); err != nil {
					return err
				}
			}
		}
	}
}

func validateSandboxAttributes(tag string, attrs []html.Attribute, p sandboxHTMLPolicy) error {
	metaRefresh := false
	for _, attr := range attrs {
		key := strings.ToLower(strings.TrimSpace(attr.Key))
		value := strings.TrimSpace(attr.Val)
		if strings.HasPrefix(key, "on") && len(key) > 2 {
			return p.newErr("%s: html attribute %s on <%s> is not allowed", p.label, key, tag)
		}
		switch key {
		case "srcset":
			return p.newErr("%s: html attribute %s on <%s> is not allowed", p.label, key, tag)
		case "style":
			if err := validateSandboxCSS(value, p); err != nil {
				return err
			}
		case "http-equiv":
			metaRefresh = tag == "meta" && strings.EqualFold(value, "refresh")
		}
		if tag == "script" && key == "src" {
			return p.newErr("%s: external script src is not allowed", p.label)
		}
		if sandboxURLAttribute(key) {
			if err := validateSandboxURL(tag, key, value, p); err != nil {
				return err
			}
		}
	}
	if metaRefresh {
		return p.newErr("%s: meta refresh is not allowed", p.label)
	}
	return nil
}

func sandboxURLAttribute(attr string) bool {
	switch attr {
	case "action", "formaction", "href", "poster", "src", "xlink:href":
		return true
	default:
		return false
	}
}

func validateSandboxURL(tag, attr, value string, p sandboxHTMLPolicy) error {
	if value == "" || strings.HasPrefix(value, "#") {
		return nil
	}
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "data:") || strings.HasPrefix(lower, "blob:") {
		return nil
	}
	return p.newErr("%s: <%s> %s must use a data/blob URL or fragment reference (no network access)", p.label, tag, attr)
}

func validateSandboxCSS(css string, p sandboxHTMLPolicy) error {
	if containsASCIIFold(css, "@import") {
		return p.newErr("%s: css @import is not allowed; inline fonts/styles or use system fonts (Georgia, Times, Cambria, Courier)", p.label)
	}
	if containsASCIIFold(css, "expression(") {
		return p.newErr("%s: css expression() is not allowed", p.label)
	}
	for offset := 0; ; {
		idx := indexASCIIFold(css[offset:], "url(")
		if idx < 0 {
			return nil
		}
		start := offset + idx + len("url(")
		endRel := strings.Index(css[start:], ")")
		if endRel < 0 {
			return p.newErr("%s: css url() is malformed", p.label)
		}
		end := start + endRel
		value := strings.Trim(strings.TrimSpace(css[start:end]), `"'`)
		if err := validateSandboxURL("style", "url()", value, p); err != nil {
			return err
		}
		offset = end + 1
	}
}
