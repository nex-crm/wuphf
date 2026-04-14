package teammcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TeamSkillRunArgs are the inputs for the team_skill_run tool.
type TeamSkillRunArgs struct {
	SkillName string `json:"skill_name" jsonschema:"Name of the skill to run (slug, e.g. 'investigate', 'daily-digest')"`
	Channel   string `json:"channel,omitempty" jsonschema:"Optional channel slug to log the invocation into. Defaults to the active conversation channel."`
	MySlug    string `json:"my_slug,omitempty" jsonschema:"Agent slug invoking the skill. Defaults to WUPHF_AGENT_SLUG."`
}

// brokerSkillResponse mirrors the JSON shape returned by
// POST /skills/<name>/invoke on the broker.
type brokerSkillResponse struct {
	Skill struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Content     string   `json:"content"`
		Channel     string   `json:"channel"`
		Tags        []string `json:"tags"`
		Trigger     string   `json:"trigger"`
		UsageCount  int      `json:"usage_count"`
		Status      string   `json:"status"`
	} `json:"skill"`
}

// handleTeamSkillRun invokes a named skill through the broker, mirroring the
// HTTP endpoint humans hit from the UI. The broker bumps UsageCount and
// appends a `skill_invocation` message to the channel so the office sees
// that the agent actually followed the playbook rather than freelancing.
func handleTeamSkillRun(ctx context.Context, _ *mcp.CallToolRequest, args TeamSkillRunArgs) (*mcp.CallToolResult, any, error) {
	name := strings.TrimSpace(args.SkillName)
	if name == "" {
		return toolError(fmt.Errorf("skill_name is required")), nil, nil
	}
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	channel := resolveConversationChannel(ctx, slug, args.Channel)

	var resp brokerSkillResponse
	path := "/skills/" + skillPathSegment(name) + "/invoke"
	if err := brokerPostJSON(ctx, path, map[string]any{
		"invoked_by": slug,
		"channel":    channel,
	}, &resp); err != nil {
		return toolError(fmt.Errorf("invoke skill %q: %w", name, err)), nil, nil
	}

	payload := map[string]any{
		"ok":          true,
		"skill_name":  resp.Skill.Name,
		"title":       resp.Skill.Title,
		"description": resp.Skill.Description,
		"trigger":     resp.Skill.Trigger,
		"usage_count": resp.Skill.UsageCount,
		"channel":     resp.Skill.Channel,
		"content":     resp.Skill.Content,
		"instructions": "Follow the steps in `content` exactly. Do NOT freelance — this skill is the canonical playbook for this request.",
	}
	return textResult(prettyObject(payload)), nil, nil
}

// skillPathSegment normalizes a skill name into the URL path segment the
// broker expects at /skills/<name>/invoke. Broker-side lookup is
// slug-insensitive but we still trim/lowercase here so the path is stable.
func skillPathSegment(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	return s
}
