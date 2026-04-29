package teammcp

// skill_compile.go exposes the broker's Stage A wiki→skill compile pipeline
// as a single MCP tool (team_skill_compile). Agents can call this when they
// notice a wiki article that looks like a reusable skill but hasn't been
// promoted yet — the broker handles the LLM gate, dedup, and tombstone
// checks.

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TeamSkillCompileArgs are the inputs for the team_skill_compile tool.
type TeamSkillCompileArgs struct {
	DryRun    bool   `json:"dry_run,omitempty" jsonschema:"When true, run the LLM classification but skip the actual proposal write. Useful for previewing what the compile pass would propose."`
	ScopePath string `json:"scope_path,omitempty" jsonschema:"Optional wiki-relative subpath to limit the scan to (e.g. 'team/customers'). Empty means scan the entire team subtree."`
}

// teamSkillCompileResponse mirrors the JSON shape returned by
// POST /skills/compile on the broker.
type teamSkillCompileResponse struct {
	Scanned         int    `json:"scanned"`
	Matched         int    `json:"matched"`
	Proposed        int    `json:"proposed"`
	Deduped         int    `json:"deduped"`
	RejectedByGuard int    `json:"rejected_by_guard"`
	DurationMs      int64  `json:"duration_ms"`
	Trigger         string `json:"trigger"`
	Errors          []struct {
		Slug   string `json:"slug"`
		Reason string `json:"reason"`
	} `json:"errors,omitempty"`
	Queued  bool   `json:"queued,omitempty"`
	Skipped string `json:"skipped,omitempty"`
}

// registerSkillCompileTools registers the team_skill_compile tool. Called
// alongside registerSkillAuthoringTools.
func registerSkillCompileTools(server *mcp.Server) {
	mcp.AddTool(server, officeWriteTool(
		"team_skill_compile",
		"Trigger a Stage A wiki→skill compile pass. The broker walks team/**/*.md, asks the LLM to classify each article, and writes a skill proposal for any reusable workflow it finds. Use sparingly: the cron already runs every 30m. dry_run=true shows what would be proposed without writing.",
	), handleTeamSkillCompile)
}

// handleTeamSkillCompile proxies the MCP call to POST /skills/compile.
func handleTeamSkillCompile(ctx context.Context, _ *mcp.CallToolRequest, args TeamSkillCompileArgs) (*mcp.CallToolResult, any, error) {
	body := map[string]any{
		"dry_run": args.DryRun,
	}
	if scope := strings.TrimSpace(args.ScopePath); scope != "" {
		body["scope_path"] = scope
	}

	var resp teamSkillCompileResponse
	if err := brokerPostJSON(ctx, "/skills/compile", body, &resp); err != nil {
		return toolError(fmt.Errorf("compile skills: %w", err)), nil, nil
	}

	payload := map[string]any{
		"ok":                resp.Skipped == "" && !resp.Queued,
		"trigger":           resp.Trigger,
		"scanned":           resp.Scanned,
		"matched":           resp.Matched,
		"proposed":          resp.Proposed,
		"deduped":           resp.Deduped,
		"rejected_by_guard": resp.RejectedByGuard,
		"duration_ms":       resp.DurationMs,
		"dry_run":           args.DryRun,
	}
	if len(resp.Errors) > 0 {
		errs := make([]map[string]string, 0, len(resp.Errors))
		for _, e := range resp.Errors {
			errs = append(errs, map[string]string{
				"slug":   e.Slug,
				"reason": e.Reason,
			})
		}
		payload["errors"] = errs
	}
	if resp.Queued {
		payload["queued"] = true
		payload["note"] = "Another compile pass was already in flight; this request was coalesced."
	}
	if resp.Skipped != "" {
		payload["skipped"] = resp.Skipped
	}

	return textResult(prettyObject(payload)), nil, nil
}
