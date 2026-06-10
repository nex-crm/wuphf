package teammcp

// policy_tools.go — the human-chat-feedback policy writer (core-loop step
// 11, B3). Policies have exactly two writers: playbook compilation (broker
// side, policy_compile.go) and human feedback during chat. This tool is the
// chat half: the CEO records ONE atomic rule when — and only when — the
// human gives explicit operating feedback. Lead-only: specialists never see
// the tool, mirroring the other scope-shaping powers.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type TeamPolicyRecordArgs struct {
	MySlug string   `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG env."`
	Rule   string   `json:"rule" jsonschema:"The single atomic operating rule, in the human's words. One rule per call — never bundle."`
	Agents []string `json:"agents,omitempty" jsonschema:"Optional agent slugs this policy applies to. Omit for ALL agents (the default for human feedback); pass only when the human named specific agents."`
}

func registerPolicyTools(server *mcp.Server) {
	mcp.AddTool(server, officeWriteTool(
		"team_policy_record",
		"Record one atomic office policy from explicit human feedback in chat (\"always …\", \"never …\", \"from now on …\"). Applies to ALL agents unless the human named specific agents (pass `agents`). Duplicate rule text reactivates the existing policy instead of minting another. Do NOT call this from your own judgment — only human feedback creates policies in chat; playbook compilation derives the rest automatically.",
	), handleTeamPolicyRecord)
}

func handleTeamPolicyRecord(ctx context.Context, _ *mcp.CallToolRequest, args TeamPolicyRecordArgs) (*mcp.CallToolResult, any, error) {
	if _, err := resolveSlug(args.MySlug); err != nil {
		return toolError(err), nil, nil
	}
	rule := strings.TrimSpace(args.Rule)
	if rule == "" {
		return toolError(fmt.Errorf("rule is required")), nil, nil
	}
	body := map[string]any{
		"source": "human_directed",
		"rule":   rule,
	}
	if len(args.Agents) > 0 {
		body["agents"] = args.Agents
	}
	var result map[string]any
	if err := brokerPostJSON(ctx, "/policies", body, &result); err != nil {
		return toolError(err), nil, nil
	}
	payload, _ := json.Marshal(result)
	return textResult(string(payload)), nil, nil
}
