package teammcp

// playbook_tools.go defines the three v1.3 playbook MCP tools:
//
//   playbook_list               — list compiled playbooks agents can invoke
//   playbook_compile            — manually recompile a specific playbook
//   playbook_execution_record   — record the outcome of a playbook run
//
// Registered only when WUPHF_MEMORY_BACKEND=markdown, matching the wiki +
// notebook + entity tool gates — playbook compilation rides on the same
// markdown git substrate.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TeamPlaybookListArgs is the contract for playbook_list (no inputs).
type TeamPlaybookListArgs struct {
	MySlug string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG env."`
}

// TeamPlaybookCompileArgs is the contract for playbook_compile.
type TeamPlaybookCompileArgs struct {
	MySlug string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG env."`
	Slug   string `json:"slug" jsonschema:"Kebab-case playbook slug matching team/playbooks/{slug}.md."`
}

// TeamPlaybookExecutionRecordArgs is the contract for playbook_execution_record.
type TeamPlaybookExecutionRecordArgs struct {
	MySlug  string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG env."`
	Slug    string `json:"slug" jsonschema:"Kebab-case playbook slug (matches team/playbooks/{slug}.md)."`
	Outcome string `json:"outcome" jsonschema:"One of: success | partial | aborted"`
	Summary string `json:"summary" jsonschema:"One paragraph describing what actually happened and what you changed. Required."`
	Notes   string `json:"notes,omitempty" jsonschema:"Optional — anything the next runner should know that the playbook text doesn't already capture."`
}

// registerPlaybookTools attaches the three playbook tools to the MCP server.
// Caller (registerSharedMemoryTools, markdown branch) is responsible for
// gating on WUPHF_MEMORY_BACKEND.
func registerPlaybookTools(server *mcp.Server) {
	mcp.AddTool(server, readOnlyTool(
		"playbook_list",
		"List every compiled playbook in the team wiki along with its source article, compiled skill path, and how many times it has been executed. Use this when deciding whether there is an existing playbook for the task in front of you before improvising one from scratch.",
	), handlePlaybookListTool)
	mcp.AddTool(server, officeWriteTool(
		"playbook_compile",
		"Manually recompile one playbook. The broker auto-recompiles whenever the source team/playbooks/{slug}.md changes, so you usually do NOT need this. Reserve it for retries after a compile error or when onboarding a new playbook that was just authored.",
	), handlePlaybookCompileTool)
	mcp.AddTool(server, officeWriteTool(
		"playbook_execution_record",
		"Record the outcome of a playbook run. Any agent that invokes a compiled playbook skill is expected to call this when the run finishes (success, partial, or aborted). The log is append-only — wrong outcomes are corrected by adding a new entry, never by editing.",
	), handlePlaybookExecutionRecord)
}

func handlePlaybookListTool(ctx context.Context, _ *mcp.CallToolRequest, args TeamPlaybookListArgs) (*mcp.CallToolResult, any, error) {
	if _, err := resolveSlug(args.MySlug); err != nil {
		return toolError(err), nil, nil
	}
	var result struct {
		Playbooks []map[string]any `json:"playbooks"`
	}
	if err := brokerGetJSON(ctx, "/playbook/list", &result); err != nil {
		return toolError(err), nil, nil
	}
	payload, _ := json.Marshal(result)
	return textResult(string(payload)), nil, nil
}

func handlePlaybookCompileTool(ctx context.Context, _ *mcp.CallToolRequest, args TeamPlaybookCompileArgs) (*mcp.CallToolResult, any, error) {
	if _, err := resolveSlug(args.MySlug); err != nil {
		return toolError(err), nil, nil
	}
	slug := strings.TrimSpace(args.Slug)
	if slug == "" {
		return toolError(fmt.Errorf("slug is required")), nil, nil
	}
	var result struct {
		Slug      string `json:"slug"`
		SkillPath string `json:"skill_path"`
		CommitSHA string `json:"commit_sha"`
	}
	if err := brokerPostJSON(ctx, "/playbook/compile", map[string]any{"slug": slug}, &result); err != nil {
		return toolError(err), nil, nil
	}
	payload, _ := json.Marshal(result)
	return textResult(string(payload)), nil, nil
}

func handlePlaybookExecutionRecord(ctx context.Context, _ *mcp.CallToolRequest, args TeamPlaybookExecutionRecordArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	playbookSlug := strings.TrimSpace(args.Slug)
	outcome := strings.TrimSpace(args.Outcome)
	summary := strings.TrimSpace(args.Summary)
	if playbookSlug == "" {
		return toolError(fmt.Errorf("slug is required")), nil, nil
	}
	if outcome == "" {
		return toolError(fmt.Errorf("outcome is required (success|partial|aborted)")), nil, nil
	}
	if summary == "" {
		return toolError(fmt.Errorf("summary is required")), nil, nil
	}
	body := map[string]any{
		"slug":        playbookSlug,
		"outcome":     outcome,
		"summary":     summary,
		"recorded_by": slug,
	}
	if notes := strings.TrimSpace(args.Notes); notes != "" {
		body["notes"] = notes
	}
	var result struct {
		ExecutionID    string `json:"execution_id"`
		ExecutionCount int    `json:"execution_count"`
	}
	if err := brokerPostJSON(ctx, "/playbook/execution", body, &result); err != nil {
		return toolError(err), nil, nil
	}
	payload, _ := json.Marshal(result)
	return textResult(string(payload)), nil, nil
}
