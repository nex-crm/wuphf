package teammcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func handleTeamChannels(ctx context.Context, _ *mcp.CallToolRequest, _ TeamChannelsArgs) (*mcp.CallToolResult, any, error) {
	var result struct {
		Channels []struct {
			Slug        string   `json:"slug"`
			Name        string   `json:"name"`
			Description string   `json:"description"`
			Members     []string `json:"members"`
			Disabled    []string `json:"disabled"`
		} `json:"channels"`
	}
	if err := brokerGetJSON(ctx, "/channels", &result); err != nil {
		return toolError(err), nil, nil
	}
	if len(result.Channels) == 0 {
		return textResult("No office channels."), nil, nil
	}
	lines := make([]string, 0, len(result.Channels))
	for _, ch := range result.Channels {
		line := fmt.Sprintf("- #%s", ch.Slug)
		if strings.TrimSpace(ch.Description) != "" {
			line += " — " + strings.TrimSpace(ch.Description)
		}
		if len(ch.Members) > 0 {
			line += " · members: @" + strings.Join(ch.Members, ", @")
		}
		if len(ch.Disabled) > 0 {
			line += " · disabled: @" + strings.Join(ch.Disabled, ", @")
		}
		lines = append(lines, line)
	}
	return textResult("Office channels:\n" + strings.Join(lines, "\n") + "\n\nYou can inspect channel names and descriptions even if you are not a member. Only the CEO has full cross-channel content context by default."), nil, nil
}

func handleTeamDMOpen(ctx context.Context, _ *mcp.CallToolRequest, args TeamDMOpenArgs) (*mcp.CallToolResult, any, error) {
	if len(args.Members) < 2 {
		return toolError(fmt.Errorf("members must have at least 2 entries (e.g. [\"human\", \"engineering\"])")), nil, nil
	}
	// Validate: must include human. Agent-to-agent DMs are not allowed.
	hasHuman := false
	for _, m := range args.Members {
		if m == "human" || m == "you" {
			hasHuman = true
			break
		}
	}
	if !hasHuman {
		return toolError(fmt.Errorf("DM must include 'human' as a member; agent-to-agent DMs are not allowed")), nil, nil
	}

	dmType := strings.TrimSpace(strings.ToLower(args.Type))
	if dmType == "" {
		dmType = "direct"
	}

	var result struct {
		ID      string `json:"id"`
		Slug    string `json:"slug"`
		Type    string `json:"type"`
		Name    string `json:"name"`
		Created bool   `json:"created"`
	}
	if err := brokerPostJSON(ctx, "/channels/dm", map[string]any{
		"members": args.Members,
		"type":    dmType,
	}, &result); err != nil {
		return toolError(err), nil, nil
	}

	action := "Found existing"
	if result.Created {
		action = "Created new"
	}
	return textResult(fmt.Sprintf("%s DM channel: #%s (id: %s, type: %s, name: %s)", action, result.Slug, result.ID, result.Type, result.Name)), nil, nil
}

func handleTeamChannel(ctx context.Context, _ *mcp.CallToolRequest, args TeamChannelArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	action := strings.TrimSpace(args.Action)
	channel := normalizeSlug(args.Channel)
	switch action {
	case "create", "remove":
		if channel == "" {
			return toolError(fmt.Errorf("channel slug is required for %s", action)), nil, nil
		}
	default:
		channel = resolveConversationChannel(ctx, slug, args.Channel)
	}
	if err := brokerPostJSON(ctx, "/channels", map[string]any{
		"action":      action,
		"slug":        channel,
		"name":        strings.TrimSpace(args.Name),
		"description": strings.TrimSpace(args.Description),
		"members":     args.Members,
		"created_by":  slug,
	}, nil); err != nil {
		return toolError(err), nil, nil
	}
	if err := reconfigureOfficeSessionFn(); err != nil {
		return toolError(err), nil, nil
	}
	return textResult(fmt.Sprintf("%s channel #%s", titleCaser.String(strings.TrimSpace(args.Action)), channel)), nil, nil
}

func handleTeamChannelMember(ctx context.Context, _ *mcp.CallToolRequest, args TeamChannelMemberArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	channel := resolveConversationChannel(ctx, slug, args.Channel)
	member := normalizeSlug(args.MemberSlug)
	if member == "" {
		return toolError(fmt.Errorf("member_slug is required")), nil, nil
	}
	if err := brokerPostJSON(ctx, "/channel-members", map[string]any{
		"action":  strings.TrimSpace(args.Action),
		"channel": channel,
		"slug":    member,
	}, nil); err != nil {
		return toolError(err), nil, nil
	}
	if err := reconfigureOfficeSessionFn(); err != nil {
		return toolError(err), nil, nil
	}
	return textResult(fmt.Sprintf("%s @%s in #%s", titleCaser.String(strings.TrimSpace(args.Action)), member, channel)), nil, nil
}

func handleTeamBridge(ctx context.Context, _ *mcp.CallToolRequest, args TeamBridgeArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	if slug != "ceo" {
		return toolError(fmt.Errorf("only the CEO can bridge channel context; ask @ceo to do it")), nil, nil
	}
	source := resolveChannel(args.SourceChannel)
	target := resolveChannel(args.TargetChannel)
	if source == target {
		return toolError(fmt.Errorf("source and target channels must be different")), nil, nil
	}
	var result struct {
		ID         string   `json:"id"`
		DecisionID string   `json:"decision_id"`
		SignalIDs  []string `json:"signal_ids"`
	}
	if err := brokerPostJSON(ctx, "/bridges", map[string]any{
		"actor":          slug,
		"source_channel": source,
		"target_channel": target,
		"summary":        strings.TrimSpace(args.Summary),
		"tagged":         args.Tagged,
		"reply_to":       strings.TrimSpace(args.ReplyToID),
	}, &result); err != nil {
		return toolError(err), nil, nil
	}
	text := fmt.Sprintf("CEO bridged context from #%s to #%s", source, target)
	if result.ID != "" {
		text += " (" + result.ID + ")"
	}
	text += "."
	return textResult(text), nil, nil
}

func handleTeamMember(ctx context.Context, _ *mcp.CallToolRequest, args TeamMemberArgs) (*mcp.CallToolResult, any, error) {
	if _, err := resolveSlug(args.MySlug); err != nil {
		return toolError(err), nil, nil
	}
	slug := normalizeSlug(args.Slug)
	if slug == "" {
		return toolError(fmt.Errorf("slug is required")), nil, nil
	}
	action := strings.ToLower(strings.TrimSpace(args.Action))
	switch action {
	case "create":
		body := map[string]any{
			"action":          "create",
			"slug":            slug,
			"name":            strings.TrimSpace(args.Name),
			"role":            strings.TrimSpace(args.Role),
			"expertise":       args.Expertise,
			"personality":     strings.TrimSpace(args.Personality),
			"permission_mode": strings.TrimSpace(args.PermissionMode),
			"created_by":      strings.TrimSpace(resolveSlugOptional(args.MySlug)),
		}
		if pkind := strings.TrimSpace(args.Provider); pkind != "" || strings.TrimSpace(args.Model) != "" {
			p := map[string]any{"kind": pkind, "model": strings.TrimSpace(args.Model)}
			if pkind == "openclaw" {
				oc := map[string]any{}
				if v := strings.TrimSpace(args.OpenclawSessionKey); v != "" {
					oc["session_key"] = v
				}
				if v := strings.TrimSpace(args.OpenclawAgentID); v != "" {
					oc["agent_id"] = v
				}
				p["openclaw"] = oc
			}
			body["provider"] = p
		}
		if err := brokerPostJSON(ctx, "/office-members", body, nil); err != nil {
			return toolError(err), nil, nil
		}
		if err := reconfigureOfficeSessionFn(); err != nil {
			return toolError(err), nil, nil
		}
		return textResult(fmt.Sprintf("Created office member @%s.", slug)), nil, nil
	case "remove":
		if err := brokerPostJSON(ctx, "/office-members", map[string]any{
			"action": "remove",
			"slug":   slug,
		}, nil); err != nil {
			return toolError(err), nil, nil
		}
		if err := reconfigureOfficeSessionFn(); err != nil {
			return toolError(err), nil, nil
		}
		return textResult(fmt.Sprintf("Removed office member @%s.", slug)), nil, nil
	default:
		return toolError(fmt.Errorf("unknown action %q", args.Action)), nil, nil
	}
}

func normalizeSlug(input string) string {
	slug := strings.ToLower(strings.TrimSpace(input))
	slug = strings.ReplaceAll(slug, " ", "-")
	slug = strings.ReplaceAll(slug, "_", "-")
	return slug
}
