package teammcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// handleTeamWikiWrite posts the article to the broker's wiki worker queue.
// Queue saturation surfaces as a tool error so the agent sees it and retries
// on the next turn — no hidden retries.
func handleTeamWikiWrite(ctx context.Context, _ *mcp.CallToolRequest, args TeamWikiWriteArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	path := strings.TrimSpace(args.ArticlePath)
	if path == "" {
		return toolError(fmt.Errorf("article_path is required")), nil, nil
	}
	mode := strings.TrimSpace(args.Mode)
	if mode == "" {
		mode = "create"
	}
	switch mode {
	case "create", "replace", "append_section":
	default:
		return toolError(fmt.Errorf("mode must be one of create | replace | append_section; got %q", mode)), nil, nil
	}
	if strings.TrimSpace(args.Content) == "" {
		return toolError(fmt.Errorf("content is required")), nil, nil
	}
	var result struct {
		Path         string `json:"path"`
		CommitSHA    string `json:"commit_sha"`
		BytesWritten int    `json:"bytes_written"`
	}
	err = brokerPostJSON(ctx, "/wiki/write", map[string]any{
		"slug":           slug,
		"path":           path,
		"mode":           mode,
		"content":        args.Content,
		"commit_message": args.CommitMsg,
	}, &result)
	if err != nil {
		return toolError(err), nil, nil
	}
	payload, _ := json.Marshal(map[string]any{
		"path":          result.Path,
		"commit_sha":    result.CommitSHA,
		"bytes_written": result.BytesWritten,
	})
	return textResult(string(payload)), nil, nil
}

// handleTeamWikiRead returns the raw article bytes.
func handleTeamWikiRead(ctx context.Context, _ *mcp.CallToolRequest, args TeamWikiReadArgs) (*mcp.CallToolResult, any, error) {
	path := strings.TrimSpace(args.ArticlePath)
	if path == "" {
		return toolError(fmt.Errorf("article_path is required")), nil, nil
	}
	brokerPath := "/wiki/read?path=" + url.QueryEscape(path)
	// Pass agent slug so the broker can record this read in the attention log.
	// Fall back to NEX_AGENT_SLUG for environments that use the new Nex CLI identity.
	if slug := strings.TrimSpace(os.Getenv("WUPHF_AGENT_SLUG")); slug != "" {
		brokerPath += "&reader=" + url.QueryEscape(slug)
	} else if slug := strings.TrimSpace(os.Getenv("NEX_AGENT_SLUG")); slug != "" {
		brokerPath += "&reader=" + url.QueryEscape(slug)
	}
	bytes, err := brokerGetRaw(ctx, brokerPath)
	if err != nil {
		return toolError(err), nil, nil
	}
	return textResult(string(bytes)), nil, nil
}

// handleTeamWikiSearch runs a literal substring search.
func handleTeamWikiSearch(ctx context.Context, _ *mcp.CallToolRequest, args TeamWikiSearchArgs) (*mcp.CallToolResult, any, error) {
	pattern := strings.TrimSpace(args.Pattern)
	if pattern == "" {
		return toolError(fmt.Errorf("pattern is required")), nil, nil
	}
	var result struct {
		Hits []map[string]any `json:"hits"`
	}
	if err := brokerGetJSON(ctx, "/wiki/search?pattern="+url.QueryEscape(pattern), &result); err != nil {
		return toolError(err), nil, nil
	}
	payload, _ := json.Marshal(result.Hits)
	return textResult(string(payload)), nil, nil
}

// handleTeamWikiList returns the auto-regenerated catalog at index/all.md.
func handleTeamWikiList(ctx context.Context, _ *mcp.CallToolRequest, _ TeamWikiListArgs) (*mcp.CallToolResult, any, error) {
	bytes, err := brokerGetRaw(ctx, "/wiki/list")
	if err != nil {
		return toolError(err), nil, nil
	}
	return textResult(string(bytes)), nil, nil
}

// handleTeamWikiLookup answers a natural-language question with a cited
// response assembled from the team wiki. The broker's /wiki/lookup endpoint
// runs the full QueryHandler pipeline: classify → search → prompt → parse.
// Returns the raw QueryAnswer JSON so the calling agent can render citations.
func handleTeamWikiLookup(ctx context.Context, _ *mcp.CallToolRequest, args TeamWikiLookupArgs) (*mcp.CallToolResult, any, error) {
	q := strings.TrimSpace(args.Query)
	if q == "" {
		return toolError(fmt.Errorf("query is required")), nil, nil
	}
	path := "/wiki/lookup?q=" + url.QueryEscape(q)
	if args.TopK > 0 {
		path += fmt.Sprintf("&top_k=%d", args.TopK)
	}
	bytes, err := brokerGetRaw(ctx, path)
	if err != nil {
		return toolError(err), nil, nil
	}
	return textResult(string(bytes)), nil, nil
}

// ── Lint tools ────────────────────────────────────────────────────────────────

// RunLintArgs is intentionally empty — run_lint takes no input parameters.
type RunLintArgs struct{}

// ResolveContradictionArgs is the contract for resolve_contradiction.
type ResolveContradictionArgs struct {
	ReportDate string `json:"report_date" jsonschema:"YYYY-MM-DD date of the lint report to resolve from"`
	FindingIdx int    `json:"finding_idx" jsonschema:"0-based index into the findings array returned by run_lint"`
	Winner     string `json:"winner"      jsonschema:"A | B | Both — which fact wins; Both acknowledges both as valid"`
}

// handleRunLint calls POST /wiki/lint/run on the broker and returns the
// full LintReport JSON.
func handleRunLint(ctx context.Context, _ *mcp.CallToolRequest, _ RunLintArgs) (*mcp.CallToolResult, any, error) {
	var report any
	if err := brokerPostJSON(ctx, "/wiki/lint/run", nil, &report); err != nil {
		return toolError(err), nil, nil
	}
	payload, _ := json.Marshal(report)
	return textResult(string(payload)), nil, nil
}

// handleResolveContradiction calls POST /wiki/lint/resolve on the broker.
func handleResolveContradiction(ctx context.Context, _ *mcp.CallToolRequest, args ResolveContradictionArgs) (*mcp.CallToolResult, any, error) {
	reportDate := strings.TrimSpace(args.ReportDate)
	if reportDate == "" {
		return toolError(fmt.Errorf("report_date is required")), nil, nil
	}
	winner := strings.TrimSpace(args.Winner)
	if winner != "A" && winner != "B" && winner != "Both" {
		return toolError(fmt.Errorf("winner must be A, B, or Both; got %q", winner)), nil, nil
	}

	var resp map[string]string
	body := map[string]any{
		"report_date": reportDate,
		"finding_idx": args.FindingIdx,
		"winner":      winner,
	}
	if err := brokerPostJSON(ctx, "/wiki/lint/resolve", body, &resp); err != nil {
		return toolError(err), nil, nil
	}
	msg := resp["message"]
	if msg == "" {
		msg = fmt.Sprintf("Resolved finding %d from report %s as winner=%s", args.FindingIdx, reportDate, winner)
	}
	return textResult(msg), nil, nil
}
