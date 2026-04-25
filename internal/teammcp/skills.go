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

// TeamSkillCreateArgs are the inputs for the team_skill_create tool.
type TeamSkillCreateArgs struct {
	Name        string   `json:"name" jsonschema:"Stable skill slug, e.g. 'daily-digest'"`
	Title       string   `json:"title" jsonschema:"Short human-readable title shown in the Skills app"`
	Description string   `json:"description,omitempty" jsonschema:"One-line description of what the skill does"`
	Content     string   `json:"content" jsonschema:"Concrete step-by-step instructions agents must follow when running the skill"`
	Trigger     string   `json:"trigger,omitempty" jsonschema:"When agents should invoke this skill"`
	Tags        []string `json:"tags,omitempty" jsonschema:"Optional tags such as engineering, ops, launch"`
	Action      string   `json:"action" jsonschema:"Required: propose or create. Any agent may propose; only CEO may create an active skill immediately."`
	Channel     string   `json:"channel,omitempty" jsonschema:"Optional channel slug to log the proposal into. Defaults to the active conversation channel."`
	MySlug      string   `json:"my_slug,omitempty" jsonschema:"Agent slug creating the skill. Defaults to WUPHF_AGENT_SLUG."`
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

func registerSkillAuthoringTools(server *mcp.Server) {
	mcp.AddTool(server, officeWriteTool(
		"team_skill_create",
		"Create or propose a durable WUPHF skill through structured fields instead of a prose block. Any agent may use action=propose to queue human approval. Only CEO may use action=create to activate immediately when the human explicitly asked to create or activate the skill.",
	), handleTeamSkillCreate)
}

// handleTeamSkillCreate creates a skill through the broker's structured API.
// This is the deterministic path for agent-authored skills.
func handleTeamSkillCreate(ctx context.Context, _ *mcp.CallToolRequest, args TeamSkillCreateArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	name := skillPathSegment(args.Name)
	if name == "" {
		return toolError(fmt.Errorf("name is required")), nil, nil
	}
	content := strings.TrimSpace(args.Content)
	if content == "" {
		return toolError(fmt.Errorf("content is required")), nil, nil
	}
	action := strings.ToLower(strings.TrimSpace(args.Action))
	if action == "" {
		return toolError(fmt.Errorf("action is required: use propose or create")), nil, nil
	}
	if action != "propose" && action != "create" {
		return toolError(fmt.Errorf("action must be propose or create")), nil, nil
	}
	if action == "create" && slug != "ceo" {
		return toolError(fmt.Errorf("only CEO may use action=create; use action=propose to queue a skill proposal")), nil, nil
	}
	channel := resolveConversationChannel(ctx, slug, args.Channel)
	title := strings.TrimSpace(args.Title)
	if title == "" {
		title = name
	}

	var resp brokerSkillResponse
	if err := brokerPostJSON(ctx, "/skills", map[string]any{
		"action":      action,
		"name":        name,
		"title":       title,
		"description": strings.TrimSpace(args.Description),
		"content":     content,
		"created_by":  slug,
		"channel":     channel,
		"tags":        compactStrings(args.Tags),
		"trigger":     strings.TrimSpace(args.Trigger),
	}, &resp); err != nil {
		return toolError(fmt.Errorf("create skill %q: %w", name, err)), nil, nil
	}

	payload := map[string]any{
		"ok":          true,
		"action":      action,
		"skill_name":  resp.Skill.Name,
		"title":       resp.Skill.Title,
		"description": resp.Skill.Description,
		"trigger":     resp.Skill.Trigger,
		"status":      resp.Skill.Status,
		"channel":     resp.Skill.Channel,
	}
	if action == "propose" {
		payload["approval"] = "A non-blocking human approval request was queued; accepting it activates the skill."
	}
	return textResult(prettyObject(payload)), nil, nil
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
		"ok":           true,
		"skill_name":   resp.Skill.Name,
		"title":        resp.Skill.Title,
		"description":  resp.Skill.Description,
		"trigger":      resp.Skill.Trigger,
		"usage_count":  resp.Skill.UsageCount,
		"channel":      resp.Skill.Channel,
		"content":      resp.Skill.Content,
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
