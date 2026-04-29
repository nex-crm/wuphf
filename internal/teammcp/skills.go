package teammcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TeamSkillRunArgs are the inputs for the team_skill_run tool.
type TeamSkillRunArgs struct {
	SkillName string `json:"skill_name" jsonschema:"Name of the skill to run (slug, e.g. 'investigate', 'daily-digest')"`
	Channel   string `json:"channel,omitempty" jsonschema:"Optional channel slug to log the invocation into. Defaults to the active conversation channel."`
	MySlug    string `json:"my_slug,omitempty" jsonschema:"Agent slug invoking the skill. Defaults to WUPHF_AGENT_SLUG."`
}

// TeamSkillListArgs are the inputs for the team_skill_list tool.
type TeamSkillListArgs struct {
	Scope  string `json:"scope,omitempty" jsonschema:"own (default) returns visible-only with full metadata; all returns every active skill metadata-only"`
	Tag    string `json:"tag,omitempty" jsonschema:"Optional tag filter"`
	MySlug string `json:"my_slug,omitempty" jsonschema:"Agent slug. Defaults to WUPHF_AGENT_SLUG."`
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
	registerSkillCompileTools(server)
	registerSkillCRUDTools(server)
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
		// PR 7 F1: surface the broker's structured 403 (handleInvokeSkill +
		// writeSkillForbidden) so the agent gets a parseable delegate_to
		// list instead of a flat error string. brokerPostJSON wraps the
		// HTTP body inside its error message, so we detect the JSON suffix
		// and decode it; on any decode miss we fall back to toolError so
		// the caller still sees the failure.
		if structured := parseStructuredSkillForbidden(err); structured != nil {
			return textResult(prettyObject(structured)), nil, nil
		}
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

// parseStructuredSkillForbidden extracts the broker's structured 403 body
// (set by writeSkillForbidden in handleInvokeSkill) out of a brokerPostJSON
// error string. Returns nil when the error is not a recognised forbidden
// response, so the caller can fall back to a generic tool error. The known
// reason codes are "not_owner" (PR 7 visibility gate) and "disabled" (PR 7
// step 4 disabled status).
func parseStructuredSkillForbidden(err error) map[string]any {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if !strings.Contains(msg, `"not_owner"`) && !strings.Contains(msg, `"disabled"`) {
		return nil
	}
	// brokerPostJSON formats failures as `... <status> <body>`, so the JSON
	// payload always starts at the first `{` in the message.
	i := strings.Index(msg, "{")
	if i < 0 {
		return nil
	}
	var body struct {
		OK         bool     `json:"ok"`
		Error      string   `json:"error"`
		DelegateTo []string `json:"delegate_to"`
		Hint       string   `json:"hint"`
	}
	if uerr := json.Unmarshal([]byte(msg[i:]), &body); uerr != nil {
		return nil
	}
	if body.Error == "" {
		return nil
	}
	if body.DelegateTo == nil {
		body.DelegateTo = []string{}
	}
	return map[string]any{
		"ok":          false,
		"error":       body.Error,
		"delegate_to": body.DelegateTo,
		"hint":        body.Hint,
	}
}

// brokerSkillListEntry mirrors the JSON shape an agent receives from
// GET /skills/list. Content is empty for scope=all (privacy + token discipline).
type brokerSkillListEntry struct {
	Name        string   `json:"name"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Trigger     string   `json:"trigger,omitempty"`
	OwnerAgents []string `json:"owner_agents,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Content     string   `json:"content,omitempty"`
	Status      string   `json:"status,omitempty"`
	UsageCount  int      `json:"usage_count,omitempty"`
}

// handleTeamSkillList lists skills for the calling agent (scope=own, default)
// or every active skill's metadata (scope=all). Cross-role discovery uses
// scope=all to decide whether to invoke directly or delegate to an owner.
func handleTeamSkillList(ctx context.Context, _ *mcp.CallToolRequest, args TeamSkillListArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	scope := strings.ToLower(strings.TrimSpace(args.Scope))
	if scope == "" {
		scope = "own"
	}
	if scope != "own" && scope != "all" {
		return toolError(fmt.Errorf("scope must be own or all")), nil, nil
	}

	values := url.Values{}
	values.Set("scope", scope)
	if scope == "own" {
		values.Set("for_agent", slug)
	}
	if tag := strings.TrimSpace(args.Tag); tag != "" {
		values.Set("tag", tag)
	}

	var resp struct {
		Skills []brokerSkillListEntry `json:"skills"`
		Scope  string                 `json:"scope"`
	}
	if err := brokerGetJSON(ctx, "/skills/list?"+values.Encode(), &resp); err != nil {
		return toolError(fmt.Errorf("list skills (scope=%s): %w", scope, err)), nil, nil
	}

	skills := make([]map[string]any, 0, len(resp.Skills))
	for _, sk := range resp.Skills {
		entry := map[string]any{
			"name":         sk.Name,
			"title":        sk.Title,
			"description":  sk.Description,
			"trigger":      sk.Trigger,
			"owner_agents": sk.OwnerAgents,
			"tags":         sk.Tags,
		}
		if scope == "own" {
			entry["content"] = sk.Content
		}
		skills = append(skills, entry)
	}

	guidance := "These are the skills you can invoke directly via team_skill_run."
	if scope == "all" {
		guidance = "Cross-role catalog (metadata only). To use a skill listed here, either invoke directly via team_skill_run if you own it, or delegate to one of the owner_agents via team_broadcast."
	}

	payload := map[string]any{
		"ok":           true,
		"scope":        scope,
		"count":        len(skills),
		"skills":       skills,
		"instructions": guidance,
	}
	return textResult(prettyObject(payload)), nil, nil
}
