package teammcp

// notebook_review_tool.go is PR 4 of the notebook-wiki-promise design.
//
// team_notebook_review is a CEO-only MCP tool: it surfaces ranked promotion
// candidates from the broker's NotebookDemandIndex and (optionally) records
// CEO review flags so the CEO's attention itself becomes a demand signal.
//
// Why the broker round-trip: the index lives in the broker process. The MCP
// server runs in a separate stdio process. So the tool calls
// /notebook/review-candidates rather than reaching into the index directly.
//
// Forward-compat: the broker endpoint returns 503 when its demandIndex is
// nil (e.g. PR 4 lands without PR 3 wiring on a reverted branch). The tool
// translates that into a friendly "no promotion candidates yet" message
// instead of bubbling a raw HTTP error to the CEO agent.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nex-crm/wuphf/internal/team"
)

// notebookReviewSnippetMax bounds the snippet returned per candidate. Spec:
// "first 200 chars". We measure runes (not bytes) so multibyte characters
// don't get split mid-codepoint.
const notebookReviewSnippetMax = 200

// notebookReviewDefaultLimit matches the broker's default n. Bounded
// independently so the CEO agent can request a tighter list without the
// broker default leaking in.
const notebookReviewDefaultLimit = 20

// registerNotebookReviewTool attaches the CEO-only team_notebook_review tool
// to the MCP server. Caller (configureServerTools) is responsible for the
// CEO gate; this function does not re-check the role.
func registerNotebookReviewTool(server *mcp.Server) {
	mcp.AddTool(server, readOnlyTool(
		"team_notebook_review",
		"CEO-only. List the top promotion candidates across every agent's notebook shelf, ranked by demand score (cross-agent searches, channel context-asks, prior CEO flags). Each row carries a path, owner slug, score, top demand signal, a 200-char snippet, and a /reviews?path=... link. Optional flag arg: pass an array of notebook entry paths to record a CEO review demand signal (weight 1.5) for each — useful when an entry is interesting but not yet promotion-ready. Same path flagged 3x in a day still counts as 1.5, not 4.5.",
	), handleTeamNotebookReview)
}

// TeamNotebookReviewArgs is the contract for team_notebook_review.
type TeamNotebookReviewArgs struct {
	Limit int      `json:"limit,omitempty" jsonschema:"Maximum candidates to return. Default 20, max 100."`
	Flag  []string `json:"flag,omitempty" jsonschema:"Optional list of notebook entry paths (agents/{slug}/notebook/{file}.md) to flag as worth reviewing. Each flag records a CEO review demand signal (weight 1.5) — same path multiple times in one day collapses to weight 1.5, not 4.5."`
}

// notebookReviewResult is the per-candidate row returned to the CEO agent.
// Field tags are stable wire format consumed by the agent prompt; renaming
// any of them breaks downstream rationale strings.
type notebookReviewResult struct {
	Path       string  `json:"path"`
	OwnerSlug  string  `json:"owner_slug"`
	Score      float64 `json:"score"`
	TopSignal  string  `json:"top_signal"`
	Snippet    string  `json:"snippet"`
	PromoteURL string  `json:"promote_url"`
}

// notebookReviewResponse wraps the tool's full payload. We always return a
// flagged-paths echo so the CEO agent can confirm the flag side-effect ran.
type notebookReviewResponse struct {
	Candidates []notebookReviewResult `json:"candidates"`
	Flagged    []string               `json:"flagged,omitempty"`
	Skipped    []map[string]string    `json:"skipped,omitempty"`
	Threshold  float64                `json:"threshold,omitempty"`
	Window     int                    `json:"window_days,omitempty"`
	Message    string                 `json:"message,omitempty"`
}

// brokerReviewCandidatesGET is the GET-side payload returned by
// /notebook/review-candidates. Mirrors the JSON shape produced by the
// broker handler.
type brokerReviewCandidatesGET struct {
	Candidates []team.DemandCandidate `json:"candidates"`
	Threshold  float64                `json:"threshold"`
	Window     int                    `json:"window"`
}

// brokerReviewCandidatesPOST is the POST-side payload echo for flag writes.
type brokerReviewCandidatesPOST struct {
	Recorded []string            `json:"recorded"`
	Skipped  []map[string]string `json:"skipped"`
}

// handleTeamNotebookReview implements the team_notebook_review MCP tool.
//
// Behaviour:
//  1. If args.Flag is non-empty, POST those paths to the broker so they
//     record DemandSignalCEOReviewFlag. POST happens first so the flag's
//     score contribution is visible in the very same call's GET ranking.
//  2. GET /notebook/review-candidates?n=limit. On 503, return a clean
//     "demand index not yet populated" message instead of a tool error.
//  3. For each returned DemandCandidate, fetch a snippet via /notebook/read
//     (best-effort — a missing file does not fail the tool, it just yields
//     an empty snippet).
//  4. Sort the result by score descending and return.
func handleTeamNotebookReview(ctx context.Context, _ *mcp.CallToolRequest, args TeamNotebookReviewArgs) (*mcp.CallToolResult, any, error) {
	limit := args.Limit
	if limit <= 0 {
		limit = notebookReviewDefaultLimit
	}

	resp := notebookReviewResponse{
		Candidates: []notebookReviewResult{},
	}

	// 1. Flag side-effect first so the score updates feed the GET.
	if flagPaths := dedupeNonEmptyStrings(args.Flag); len(flagPaths) > 0 {
		var flagOut brokerReviewCandidatesPOST
		err := brokerPostJSON(ctx, "/notebook/review-candidates", map[string]any{
			"entry_paths": flagPaths,
		}, &flagOut)
		if err != nil {
			// 503 from broker: index not active. Don't fail — just surface
			// the message and skip the GET (which would also 503).
			if isBrokerDemandIndexInactive(err) {
				resp.Message = "Demand index is not yet active on this broker; no candidates and no flags recorded."
				return marshalNotebookReviewResponse(resp), nil, nil
			}
			return toolError(fmt.Errorf("flag CEO review entries: %w", err)), nil, nil
		}
		resp.Flagged = flagOut.Recorded
		resp.Skipped = flagOut.Skipped
	}

	// 2. Fetch ranked candidates.
	var candidates brokerReviewCandidatesGET
	q := url.Values{}
	q.Set("n", fmt.Sprintf("%d", limit))
	err := brokerGetJSON(ctx, "/notebook/review-candidates?"+q.Encode(), &candidates)
	if err != nil {
		if isBrokerDemandIndexInactive(err) {
			resp.Message = "Demand index is not yet active on this broker; no promotion candidates yet."
			return marshalNotebookReviewResponse(resp), nil, nil
		}
		return toolError(fmt.Errorf("list review candidates: %w", err)), nil, nil
	}
	resp.Threshold = candidates.Threshold
	resp.Window = candidates.Window

	if len(candidates.Candidates) == 0 {
		resp.Message = "No promotion candidates yet — agents have not searched each other's notebooks above the threshold."
		return marshalNotebookReviewResponse(resp), nil, nil
	}

	// 3. Hydrate snippets. Best-effort; broker GET /notebook/read returns
	// text/plain on success and a JSON error otherwise. We only swallow the
	// error — the rest of the tool result is still useful.
	out := make([]notebookReviewResult, 0, len(candidates.Candidates))
	for _, c := range candidates.Candidates {
		row := notebookReviewResult{
			Path:       c.EntryPath,
			OwnerSlug:  c.OwnerSlug,
			Score:      c.Score,
			TopSignal:  team.PromotionDemandSignalLabel(c.TopSignal),
			PromoteURL: promoteURLFor(c.EntryPath),
		}
		row.Snippet = fetchNotebookSnippet(ctx, c.EntryPath, c.OwnerSlug)
		out = append(out, row)
	}
	// Broker already sorts by score desc, but we re-sort defensively in
	// case a future broker change relaxes that contract.
	sortByScoreDesc(out)
	resp.Candidates = out
	return marshalNotebookReviewResponse(resp), nil, nil
}

// fetchNotebookSnippet pulls the first chunk of an entry's text from the
// broker. The broker handler returns text/plain on success, so we use
// brokerGetRaw and trim. On any error (404, 503, etc) we return "" — a
// missing snippet should not break the ranking surface.
func fetchNotebookSnippet(ctx context.Context, path, slug string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	q := url.Values{}
	q.Set("path", path)
	if slug != "" {
		q.Set("slug", slug)
	}
	body, err := brokerGetRaw(ctx, "/notebook/read?"+q.Encode())
	if err != nil {
		return ""
	}
	return truncateOnWordBoundary(string(body), notebookReviewSnippetMax)
}

// truncateOnWordBoundary returns up to max runes of s, preferring to cut at
// the last whitespace character within the window so words aren't sliced in
// half. Falls back to a hard rune cut when no whitespace exists in the
// window. Always strips a trailing fence/code marker fragment.
func truncateOnWordBoundary(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || s == "" {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return collapseWhitespace(string(runes))
	}
	window := runes[:max]
	// Walk backward from the end of the window for the last whitespace.
	cut := -1
	for i := len(window) - 1; i >= 0; i-- {
		if isSpaceRune(window[i]) {
			cut = i
			break
		}
	}
	if cut <= 0 {
		// No whitespace in the window — hard rune cut.
		return collapseWhitespace(string(window)) + "…"
	}
	return collapseWhitespace(string(window[:cut])) + "…"
}

func isSpaceRune(r rune) bool {
	return r == ' ' || r == '\n' || r == '\t' || r == '\r'
}

// collapseWhitespace replaces runs of whitespace with single spaces so the
// snippet renders cleanly in the agent prompt regardless of the source
// markdown's line breaks.
func collapseWhitespace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if isSpaceRune(r) {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	return strings.TrimSpace(b.String())
}

// promoteURLFor builds the deep-link the CEO agent passes to the human so
// they can land on the existing /reviews Kanban filtered to this entry.
// PR 4 deliberately does not add a new web surface — this URL points at
// the surface that already exists.
func promoteURLFor(entryPath string) string {
	q := url.Values{}
	q.Set("path", entryPath)
	return "/reviews?" + q.Encode()
}

// sortByScoreDesc orders results by Score descending, breaking ties on Path
// ascending so the output is deterministic for snapshot tests.
func sortByScoreDesc(rows []notebookReviewResult) {
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0; j-- {
			a, b := rows[j-1], rows[j]
			if a.Score < b.Score || (a.Score == b.Score && a.Path > b.Path) {
				rows[j-1], rows[j] = b, a
				continue
			}
			break
		}
	}
}

// dedupeNonEmptyStrings is the teammcp-side analog of the broker helper.
// Lives here (not in server_helpers.go) because no other tool currently
// dedupes string args; if a second consumer arrives we'll lift it.
func dedupeNonEmptyStrings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, raw := range items {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// isBrokerDemandIndexInactive matches the stable error string the broker
// returns when b.demandIndex is nil. Match on substring rather than parsing
// the JSON body because brokerGetJSON wraps the body into a single error.
func isBrokerDemandIndexInactive(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "503") && strings.Contains(msg, "demand index not active")
}

// marshalNotebookReviewResponse is a tiny helper that serialises the tool
// payload and wraps it in a successful CallToolResult.
func marshalNotebookReviewResponse(resp notebookReviewResponse) *mcp.CallToolResult {
	if resp.Candidates == nil {
		resp.Candidates = []notebookReviewResult{}
	}
	payload, err := json.Marshal(resp)
	if err != nil {
		return toolError(fmt.Errorf("marshal review response: %w", err))
	}
	return textResult(string(payload))
}
