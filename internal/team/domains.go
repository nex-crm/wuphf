package team

import (
	"strings"
	"unicode"
)

// InferAgentDomain maps an agent slug to its primary work domain.
func InferAgentDomain(slug string) string {
	return inferAgentDomain(slug)
}

// InferTextDomain classifies a text snippet into a work domain.
func InferTextDomain(text string) string {
	return inferTextDomain(text)
}

func inferAgentDomain(slug string) string {
	switch strings.ToLower(strings.TrimSpace(slug)) {
	case "fe", "frontend":
		return "frontend"
	case "be", "backend", "eng", "engineer", "engineering":
		return "backend"
	case "ai", "ml", "llm":
		return "ai"
	case "designer", "design":
		return "design"
	case "cmo", "growth", "marketing", "gtm":
		return "marketing"
	case "cro", "sales", "revenue":
		return "sales"
	case "pm", "product", "ceo":
		return "product"
	default:
		return "general"
	}
}

func inferMessageDomain(msg channelMessage) string {
	return inferTextDomain(msg.Title + " " + msg.Content)
}

func inferTextDomain(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	tokens := tokenize(text)
	switch {
	case hasAnyToken(tokens, "frontend", "ui", "ux", "web", "component", "page", "button", "form", "modal", "css", "html", "animation", "responsive") || containsAny(text, "hero", "cta", "signup page"):
		return "frontend"
	case hasAnyToken(tokens, "backend", "database", "api", "sync", "queue", "service", "auth", "integration",
		"middleware", "endpoint", "http", "server", "request", "response", "cache", "rate", "latency", "webhook"):
		return "backend"
	case hasAnyToken(tokens, "model", "prompt", "llm", "ai", "transcript", "embedding", "rag"):
		return "ai"
	case hasAnyToken(tokens, "design", "visual", "typography", "layout") || containsAny(text, "brand system"):
		return "design"
	case hasAnyToken(tokens, "positioning", "campaign", "launch", "brand", "marketing", "copy", "persona", "messaging", "growth",
		"content", "blog", "post", "article", "seo", "audience", "email", "newsletter", "social",
		"competitive", "competitor", "gtm", "awareness", "conversion", "funnel"):
		return "marketing"
	case hasAnyToken(tokens, "sales", "pipeline", "pricing", "revenue", "deal", "budget", "buyer", "quota", "forecast"):
		return "sales"
	case hasAnyToken(tokens, "product", "roadmap", "scope", "planning", "priority"):
		return "product"
	default:
		return "general"
	}
}

func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func tokenize(text string) map[string]bool {
	var b strings.Builder
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteRune(' ')
		}
	}
	parts := strings.Fields(b.String())
	out := make(map[string]bool, len(parts))
	for _, part := range parts {
		out[part] = true
	}
	return out
}

func hasAnyToken(tokens map[string]bool, words ...string) bool {
	for _, word := range words {
		if tokens[word] {
			return true
		}
	}
	return false
}
