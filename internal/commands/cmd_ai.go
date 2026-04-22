package commands

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/nex-crm/wuphf/internal/api"
)

func cmdAsk(ctx *SlashContext, args string) error {
	if args == "" {
		ctx.AddMessage("system", "Usage: /ask <question>")
		return nil
	}
	if !requireAuth(ctx) {
		return nil
	}
	ctx.SetLoading(true)
	result, err := api.Post[map[string]any](ctx.APIClient, "/v1/context/ask", map[string]any{"query": args}, 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	ctx.AddMessage("agent", formatMapResult(result))
	return nil
}

func cmdSearch(ctx *SlashContext, args string) error {
	if args == "" {
		ctx.AddMessage("system", "Usage: /search <query>")
		return nil
	}
	if !requireAuth(ctx) {
		return nil
	}
	ctx.SetLoading(true)
	result, err := api.Post[map[string]any](ctx.APIClient, "/v1/search", map[string]any{"query": args}, 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	ctx.AddMessage("system", formatMapResult(result))
	return nil
}

func cmdRemember(ctx *SlashContext, args string) error {
	if args == "" {
		ctx.AddMessage("system", "Usage: /remember <content>")
		return nil
	}
	if !requireAuth(ctx) {
		return nil
	}
	ctx.SetLoading(true)
	_, err := api.Post[map[string]any](ctx.APIClient, "/v1/context/text", map[string]any{"content": args}, 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	ctx.AddMessage("system", "Stored.")
	return nil
}

func cmdLookup(ctx *SlashContext, args string) error {
	if args == "" {
		ctx.AddMessage("system", "Usage: /lookup <question>\nRuns a cited-answer search against the team wiki.")
		return nil
	}
	if !requireAuth(ctx) {
		return nil
	}
	ctx.SetLoading(true)
	path := "/wiki/lookup?q=" + url.QueryEscape(args)
	result, err := api.Get[map[string]any](ctx.APIClient, path, 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	ctx.AddMessage("agent", formatLookupResult(result))
	return nil
}

// formatLookupResult renders a /wiki/lookup QueryAnswer map into a
// wiki-shaped chat message:
//
//   - Hatnote italic (coverage context)
//   - answer_markdown body
//   - Numbered sources for cited entries
//   - PageFooter latency + source count
//
// This mirrors team.FormatLookupMessage without importing the team package
// (which imports commands, creating a cycle).
func formatLookupResult(m map[string]any) string {
	var b strings.Builder

	// Hatnote.
	b.WriteString("_From the wiki")
	if cov, _ := m["coverage"].(string); cov == "partial" {
		b.WriteString(" (partial match)")
	} else if cov == "none" {
		b.WriteString(" (no match)")
	}
	b.WriteString("_\n\n")

	// Body.
	body := strings.TrimSpace(stringField(m, "answer_markdown"))
	if body != "" {
		b.WriteString(body)
		b.WriteString("\n\n")
	}

	// Sources.
	cited := intSliceField(m, "sources_cited")
	sources, _ := m["sources"].([]any)
	if len(cited) > 0 && len(sources) > 0 {
		for i, rawSrc := range sources {
			if !containsInt(cited, i+1) {
				continue
			}
			src, _ := rawSrc.(map[string]any)
			slug := strings.TrimSpace(stringField(src, "slug_or_id"))
			excerpt := strings.TrimSpace(stringField(src, "excerpt"))
			if len(excerpt) > 120 {
				excerpt = excerpt[:120] + "…"
			}
			b.WriteString(fmt.Sprintf("%d. [[%s]] — %s\n", i+1, slug, excerpt))
		}
		b.WriteString("\n")
	}

	// PageFooter.
	latency, _ := m["latency_ms"].(float64)
	srcCount := len(sources)
	if srcCount > 0 {
		unit := "source"
		if srcCount != 1 {
			unit = "sources"
		}
		b.WriteString(fmt.Sprintf("Answer generated in %dms · %d %s", int64(latency), srcCount, unit))
	} else {
		b.WriteString(fmt.Sprintf("Answer generated in %dms", int64(latency)))
	}

	return b.String()
}

func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

func intSliceField(m map[string]any, key string) []int {
	raw, ok := m[key].([]any)
	if !ok {
		return nil
	}
	out := make([]int, 0, len(raw))
	for _, v := range raw {
		if f, ok := v.(float64); ok {
			out = append(out, int(f))
		}
	}
	return out
}

func containsInt(slice []int, v int) bool {
	for _, x := range slice {
		if x == v {
			return true
		}
	}
	return false
}
