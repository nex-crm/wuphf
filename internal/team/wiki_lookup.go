package team

// wiki_lookup.go — the /lookup cited-answer HTTP handler for the broker.
//
// Route: GET /wiki/lookup?q=<query>[&top_k=<int>]
//
// This handler:
//   1. Requires the markdown wiki backend to be active (same gate as
//      /wiki/search, /wiki/read, etc.)
//   2. Constructs a QueryHandler backed by the broker's WikiIndex and a
//      brokerQueryProvider (which shells out to the configured LLM CLI).
//   3. Returns the full QueryAnswer JSON.
//
// The DESIGN-WIKI.md composition contract (hatnote + body + sources +
// page footer) is enforced at the presentation layer (web CitedAnswer
// component + the /lookup slash command renderer). This handler returns
// plain JSON; formatting is the caller's responsibility.
//
// Slash command flow:
//   /lookup <query>  (TUI + web)
//   → broker receives as a human message
//   → commandLookup is dispatched
//   → QueryHandler.Answer is called
//   → response formatted as wiki-shape chat message
//
// MCP tool flow (wuphf_wiki_lookup):
//   agent calls wuphf_wiki_lookup({query, top_k})
//   → MCP handler POSTs to /wiki/lookup
//   → JSON QueryAnswer returned to agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/provider"
)

// brokerQueryProvider wraps provider.RunConfiguredOneShot so it satisfies
// the QueryProvider interface. The LLM CLI is the same one that entity
// synthesis uses — no new dependencies.
type brokerQueryProvider struct{}

func (brokerQueryProvider) RunPrompt(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	return provider.RunConfiguredOneShotCtx(ctx, systemPrompt, userPrompt, "")
}

// handleWikiLookup answers GET /wiki/lookup?q=<query>[&top_k=<int>][&channel=<slug>].
//
// The endpoint is gated behind the wiki worker (same as /wiki/search).
// It is also gated behind requireAuth in StartOnPort.
//
// When channel is provided the formatted answer is also published as an agent
// message in that channel — this is how the web /lookup slash command gets
// the response into the chat stream without an agent round-trip.
func (b *Broker) handleWikiLookup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "q parameter is required",
		})
		return
	}

	topK := 20
	if raw := r.URL.Query().Get("top_k"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			topK = v
		}
	}

	channel := strings.TrimSpace(r.URL.Query().Get("channel"))

	req := QueryRequest{
		Query:       q,
		RequestedBy: "human",
		TopK:        topK,
		Timeout:     15 * time.Second,
	}

	var (
		ans QueryAnswer
		err error
	)
	if worker := b.WikiWorker(); worker != nil {
		// Markdown deployment: retrieve from the broker's WikiIndex.
		idx := b.WikiIndex()
		if idx == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"error": "wiki index is not ready — the boot reconcile may still be in progress",
			})
			return
		}
		ans, err = NewQueryHandler(idx, brokerQueryProvider{}).Answer(r.Context(), req)
	} else {
		// gbrain-backed deployment: retrieve hits from gbrain.Query and run the
		// same cited-answer synthesis. ok is false when gbrain is unreachable.
		var ok bool
		ans, ok, err = b.gbrainLookup(r.Context(), req)
		if !ok {
			writeWikiBackendUnavailable(w, http.StatusServiceUnavailable)
			return
		}
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": err.Error(),
		})
		return
	}

	// When the web slash command supplies a channel, publish the formatted
	// answer as an agent message so the SSE stream delivers it naturally.
	if channel != "" {
		formatted := FormatLookupMessage(ans)
		if _, pubErr := b.PostMessage("wiki", channel, formatted, nil, ""); pubErr != nil {
			// Non-fatal: log and fall through to JSON response.
			log.Printf("wiki/lookup: publish to channel %q: %v", channel, pubErr)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ans)
}

// gbrainLookup retrieves hits from gbrain.Query and runs the cited-answer
// synthesis over them. ok is false (and the caller degrades to the
// "knowledge backend unavailable" signal) when gbrain is not reachable: no
// broker-registered client, or the query fails with ErrNotInstalled. A
// non-availability query error is logged and treated as zero sources so the
// synthesis still runs (the LLM reports no coverage) rather than 500-ing.
func (b *Broker) gbrainLookup(ctx context.Context, req QueryRequest) (QueryAnswer, bool, error) {
	client := b.wikiReadGBrain()
	if client == nil {
		return QueryAnswer{}, false, nil
	}
	limit := req.TopK
	if limit <= 0 {
		limit = 20
	}
	hits, err := client.Query(ctx, req.Query, limit)
	if err != nil {
		if isGBrainUnavailable(err) {
			return QueryAnswer{}, false, nil
		}
		log.Printf("wiki/lookup: gbrain query %q: %v", req.Query, err)
		hits = nil
	}
	sources := make([]QuerySource, 0, len(hits))
	for _, hit := range hits {
		sources = append(sources, gbrainHitToSource(hit))
	}
	ans, aerr := NewQueryHandler(nil, brokerQueryProvider{}).AnswerWithSources(ctx, req, sources)
	return ans, true, aerr
}

// FormatLookupMessage renders a QueryAnswer as a wiki-shaped chat message
// per DESIGN-WIKI.md anti-pattern 12:
//
//   - Leading hatnote-style italic note ("From the wiki")
//   - Body: AnswerMarkdown verbatim (contains <sup>[n]</sup> citations)
//   - Trailing numbered sources list
//   - PageFooter action-links style: "Last updated: {most-recent valid_from}"
//
// NO card, NO callout, NO alert block (anti-pattern 12).
// The returned string is plain markdown ready for a chat message content field.
func FormatLookupMessage(ans QueryAnswer) string {
	var b strings.Builder

	// Hatnote line.
	b.WriteString("_From the wiki")
	switch ans.Coverage {
	case "partial":
		b.WriteString(" (partial match)")
	case "none":
		b.WriteString(" (no match)")
	}
	b.WriteString("_\n\n")

	// Body.
	body := strings.TrimSpace(ans.AnswerMarkdown)
	if body != "" {
		b.WriteString(body)
		b.WriteString("\n\n")
	}

	// Sources list — only when the LLM cited at least one source.
	if len(ans.SourcesCited) > 0 && len(ans.Sources) > 0 {
		for i, src := range ans.Sources {
			if !isSourceCited(i+1, ans.SourcesCited) {
				continue
			}
			slug := strings.TrimSpace(src.SlugOrID)
			excerpt := strings.TrimSpace(src.Excerpt)
			if len(excerpt) > 120 {
				excerpt = excerpt[:120] + "…"
			}
			line := fmt.Sprintf("%d. [[%s]] — %s", i+1, slug, excerpt)
			if src.SourcePath != "" {
				line += fmt.Sprintf("  source: %s", src.SourcePath)
			}
			b.WriteString(line)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// PageFooter action-links style.
	mostRecent := mostRecentValidFrom(ans.Sources)
	if mostRecent != "" {
		b.WriteString(fmt.Sprintf("Last updated: %s · %dms", mostRecent, ans.LatencyMs))
	} else {
		b.WriteString(fmt.Sprintf("Answer generated in %dms", ans.LatencyMs))
	}
	if len(ans.Sources) > 0 {
		b.WriteString(fmt.Sprintf(" · %d source", len(ans.Sources)))
		if len(ans.Sources) != 1 {
			b.WriteString("s")
		}
	}

	return b.String()
}

// isSourceCited returns true when index (1-based) is in the cited list.
func isSourceCited(index int, cited []int) bool {
	for _, c := range cited {
		if c == index {
			return true
		}
	}
	return false
}

// mostRecentValidFrom returns the most recent valid_from date from the sources
// that have one, formatted as "YYYY-MM-DD". Returns "" when none are present.
func mostRecentValidFrom(sources []QuerySource) string {
	best := ""
	for _, s := range sources {
		vf := strings.TrimSpace(s.ValidFrom)
		if vf == "" {
			continue
		}
		if best == "" || vf > best {
			best = vf
		}
	}
	return best
}
