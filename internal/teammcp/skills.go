package teammcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// maxTransientPollErrors caps consecutive broker-poll failures before
// handleRequestSkillEnable gives up. The approval request has already
// been created at this point, so aborting on the first transient read
// error would force the agent to retry and produce duplicate approval
// cards. Three consecutive failures at the 1.5s tick (~4.5s of broker
// unavailability) is the threshold beyond which we surface the error.
const maxTransientPollErrors = 3

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
	mcp.AddTool(server, officeWriteTool(
		"request_skill_enable",
		"Ask the human to enable a skill from DISCOVERABLE SKILLS for you. Use this BEFORE proposing a new skill if a similar one already exists in the library. The human gets a blocking approval card; on approve, the skill moves to your AVAILABLE SKILLS on the next prompt build and you can invoke it via team_skill_run.",
	), handleRequestSkillEnable)
	registerSkillCompileTools(server)
	registerSkillCRUDTools(server)
}

// RequestSkillEnableArgs are the inputs for the request_skill_enable tool.
type RequestSkillEnableArgs struct {
	SkillSlug string `json:"skill_slug" jsonschema:"Slug of the existing skill you want enabled, from DISCOVERABLE SKILLS in your prompt"`
	Reason    string `json:"reason" jsonschema:"One-sentence justification for why this skill solves the current need"`
	MySlug    string `json:"my_slug,omitempty" jsonschema:"Agent slug. Defaults to WUPHF_AGENT_SLUG."`
}

// handleRequestSkillEnable creates a blocking approval request for the
// human to enable an existing skill for the calling agent, polls for an
// answer, and on approval calls /skills/<slug>/enable-for. This is the
// anti-duplication path — agents should ask for an existing skill rather
// than build a redundant one when AVAILABLE SKILLS is missing what they
// need but DISCOVERABLE SKILLS has it.
func handleRequestSkillEnable(ctx context.Context, _ *mcp.CallToolRequest, args RequestSkillEnableArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	skillSlug := skillPathSegment(args.SkillSlug)
	if skillSlug == "" {
		return toolError(fmt.Errorf("skill_slug is required")), nil, nil
	}
	reason := strings.TrimSpace(args.Reason)
	if reason == "" {
		return toolError(fmt.Errorf("reason is required: explain why this skill fits the work")), nil, nil
	}
	channel := resolveConversationChannel(ctx, slug, "")

	question := fmt.Sprintf("Enable skill `%s` for @%s?", skillSlug, slug)
	contextLine := fmt.Sprintf("@%s says: %s", slug, reason)
	options := []map[string]any{
		{"id": "approve", "label": "Enable", "description": "Adds the skill to this agent's AVAILABLE list."},
		{"id": "deny", "label": "Deny", "description": "Skill stays out of the agent's prompt."},
	}

	var created struct {
		ID string `json:"id"`
	}
	if err := brokerPostJSON(ctx, "/requests", map[string]any{
		"kind":           "skill_enable_request",
		"channel":        channel,
		"from":           slug,
		"title":          "Enable skill for agent",
		"question":       question,
		"context":        contextLine,
		"options":        options,
		"recommended_id": "approve",
		"blocking":       true,
		"required":       true,
		"metadata": map[string]any{
			"skill_slug": skillSlug,
			"agent_slug": slug,
		},
	}, &created); err != nil {
		return toolError(fmt.Errorf("create enable request: %w", err)), nil, nil
	}
	if strings.TrimSpace(created.ID) == "" {
		return toolError(fmt.Errorf("enable request did not return an ID")), nil, nil
	}

	timeout := time.After(30 * time.Minute)
	ticker := time.NewTicker(1500 * time.Millisecond)
	defer ticker.Stop()
	consecutivePollErrors := 0

	for {
		select {
		case <-ctx.Done():
			return toolError(ctx.Err()), nil, nil
		case <-timeout:
			return toolError(fmt.Errorf("timed out waiting for human approval")), nil, nil
		case <-ticker.C:
			var result brokerInterviewAnswerResponse
			path := "/interview/answer?id=" + url.QueryEscape(created.ID)
			if err := brokerGetJSON(ctx, path, &result); err != nil {
				// Don't abort the approval flow on a single transient poll
				// error: the request is already created, so aborting forces
				// the agent to retry and emits a duplicate approval card.
				// Tolerate up to maxTransientPollErrors consecutive failures
				// and only error out when the broker is persistently
				// unreachable.
				consecutivePollErrors++
				if consecutivePollErrors >= maxTransientPollErrors {
					return toolError(fmt.Errorf("poll enable request %s failed after %d retries: %w", created.ID, consecutivePollErrors, err)), nil, nil
				}
				slog.Warn("request_skill_enable: transient poll error, retrying",
					"request_id", created.ID,
					"attempt", consecutivePollErrors,
					"max", maxTransientPollErrors,
					"err", err)
				continue
			}
			consecutivePollErrors = 0
			switch strings.ToLower(strings.TrimSpace(result.Status)) {
			case "canceled", "cancelled":
				return toolError(fmt.Errorf("enable request canceled")), nil, nil
			case "not_found":
				return toolError(fmt.Errorf("enable request not found")), nil, nil
			}
			if result.Answered == nil {
				continue
			}
			choice := strings.ToLower(strings.TrimSpace(result.Answered.ChoiceID))
			if choice != "approve" {
				payload, _ := json.MarshalIndent(map[string]any{
					"ok":         false,
					"approved":   false,
					"skill_slug": skillSlug,
					"agent_slug": slug,
					"note":       "Human denied the enablement. Do NOT create a duplicate skill — try a different approach or ask the human for guidance.",
				}, "", "  ")
				return textResult(string(payload)), nil, nil
			}
			var enableResp struct {
				Skill struct {
					Name        string   `json:"name"`
					OwnerAgents []string `json:"owner_agents"`
				} `json:"skill"`
			}
			enablePath := "/skills/" + url.PathEscape(skillSlug) + "/enable-for"
			if err := brokerPostJSON(ctx, enablePath, map[string]any{"agent": slug}, &enableResp); err != nil {
				return toolError(fmt.Errorf("enable skill after approval: %w", err)), nil, nil
			}
			payload, _ := json.MarshalIndent(map[string]any{
				"ok":           true,
				"approved":     true,
				"skill_slug":   enableResp.Skill.Name,
				"agent_slug":   slug,
				"owner_agents": enableResp.Skill.OwnerAgents,
				"note":         "Skill enabled. It will appear in your AVAILABLE SKILLS on the next prompt build. Invoke via team_skill_run.",
			}, "", "  ")
			return textResult(string(payload)), nil, nil
		}
	}
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
	description := strings.TrimSpace(args.Description)
	if description == "" {
		// Mirror the broker's POST /skills validation: a SKILL.md without a
		// description fails RenderSkillMarkdown and the wiki article never
		// lands on disk. Surface the requirement at the MCP boundary so the
		// agent gets a clean error rather than an opaque 400.
		return toolError(fmt.Errorf("description is required: a one-line summary of when to use this skill")), nil, nil
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
		"description": description,
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
