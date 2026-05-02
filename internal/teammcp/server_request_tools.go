package teammcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func handleTeamRequests(ctx context.Context, _ *mcp.CallToolRequest, args TeamRequestsArgs) (*mcp.CallToolResult, any, error) {
	viewer := strings.TrimSpace(resolveSlugOptional(args.MySlug))
	channel := resolveConversationChannel(ctx, viewer, args.Channel)
	values := url.Values{}
	values.Set("channel", channel)
	if viewer != "" {
		values.Set("viewer_slug", viewer)
	}
	if args.IncludeResolved {
		values.Set("include_resolved", "true")
	}
	var result brokerRequestsResponse
	path := "/requests"
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}
	if err := brokerGetJSON(ctx, path, &result); err != nil {
		return toolError(err), nil, nil
	}
	if len(result.Requests) == 0 {
		return textResult("No active office requests in #" + channel + "."), nil, nil
	}
	lines := make([]string, 0, len(result.Requests))
	for _, req := range result.Requests {
		line := fmt.Sprintf("- %s [%s] @%s", req.ID, req.Kind, req.From)
		if req.Blocking {
			line += " · blocking"
		}
		if req.Required {
			line += " · required"
		}
		if req.Title != "" {
			line += " — " + req.Title
		} else {
			line += " — " + req.Question
		}
		lines = append(lines, line)
	}
	text := "Office requests in #" + channel + ":\n" + strings.Join(lines, "\n")
	if result.Pending != nil {
		text += fmt.Sprintf("\n\nBlocking request pending: %s", result.Pending.Question)
	}
	return textResult(text), nil, nil
}

func handleTeamRequest(ctx context.Context, _ *mcp.CallToolRequest, args TeamRequestArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	ctxTarget := resolveConversationContext(ctx, slug, args.Channel, args.ReplyToID)
	channel := ctxTarget.Channel
	replyTo := ctxTarget.ReplyToID

	kind := defaultRequestKind(args.Kind)
	blocking := args.Blocking
	required := args.Required
	if kind == "approval" || kind == "confirm" || kind == "choice" {
		blocking = true
		required = true
	}
	options, recommendedID := normalizeHumanRequestOptions(kind, args.RecommendedOptionID, args.Options)

	var created struct {
		ID string `json:"id"`
	}
	if err := brokerPostJSON(ctx, "/requests", map[string]any{
		"kind":           kind,
		"channel":        channel,
		"from":           slug,
		"title":          strings.TrimSpace(args.Title),
		"question":       args.Question,
		"context":        args.Context,
		"options":        options,
		"recommended_id": recommendedID,
		"blocking":       blocking,
		"required":       required,
		"secret":         args.Secret,
		"reply_to":       replyTo,
	}, &created); err != nil {
		return toolError(err), nil, nil
	}
	if strings.TrimSpace(created.ID) == "" {
		return toolError(fmt.Errorf("request did not return an ID")), nil, nil
	}
	return textResult(fmt.Sprintf("Created %s request %s in #%s.", defaultRequestKind(args.Kind), created.ID, channel)), nil, nil
}

func handleHumanInterview(ctx context.Context, _ *mcp.CallToolRequest, args HumanInterviewArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	location := resolveConversationContext(ctx, slug, args.Channel, "")
	channel := location.Channel

	options, recommendedID := normalizeHumanRequestOptions("interview", args.RecommendedOptionID, args.Options)
	var created struct {
		ID string `json:"id"`
	}
	if err := brokerPostJSON(ctx, "/requests", map[string]any{
		"kind":           "interview",
		"channel":        channel,
		"from":           slug,
		"title":          "Human interview",
		"question":       args.Question,
		"context":        args.Context,
		"options":        options,
		"recommended_id": recommendedID,
		"blocking":       false,
		"required":       false,
		"reply_to":       location.ReplyToID,
	}, &created); err != nil {
		return toolError(err), nil, nil
	}
	if strings.TrimSpace(created.ID) == "" {
		return toolError(fmt.Errorf("interview request did not return an ID")), nil, nil
	}

	timeout := time.After(30 * time.Minute)
	ticker := time.NewTicker(1500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return toolError(ctx.Err()), nil, nil
		case <-timeout:
			return toolError(fmt.Errorf("timed out waiting for human interview answer")), nil, nil
		case <-ticker.C:
			var result brokerInterviewAnswerResponse
			path := "/interview/answer?id=" + url.QueryEscape(created.ID)
			if err := brokerGetJSON(ctx, path, &result); err != nil {
				return toolError(err), nil, nil
			}
			switch strings.ToLower(strings.TrimSpace(result.Status)) {
			case "canceled", "cancelled":
				return toolError(fmt.Errorf("human interview canceled")), nil, nil
			case "not_found":
				return toolError(fmt.Errorf("human interview request not found")), nil, nil
			}
			if result.Answered == nil {
				continue
			}
			finalText := strings.TrimSpace(result.Answered.CustomText)
			if finalText == "" {
				finalText = strings.TrimSpace(result.Answered.ChoiceText)
			}
			payload, _ := json.MarshalIndent(map[string]any{
				"interview_id": created.ID,
				"answered":     true,
				"choice_id":    result.Answered.ChoiceID,
				"answer":       finalText,
				"answered_at":  result.Answered.AnsweredAt,
			}, "", "  ")
			return textResult(string(payload)), nil, nil
		}
	}
}

func handleHumanMessage(ctx context.Context, _ *mcp.CallToolRequest, args HumanMessageArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	ctxTarget := resolveConversationContext(ctx, slug, args.Channel, args.ReplyToID)
	channel := ctxTarget.Channel
	replyTo := ctxTarget.ReplyToID

	kind := strings.ToLower(strings.TrimSpace(args.Kind))
	switch kind {
	case "", "report":
		kind = "human_report"
	case "decision":
		kind = "human_decision"
	case "action":
		kind = "human_action"
	default:
		return toolError(fmt.Errorf("unsupported human message kind %q", args.Kind)), nil, nil
	}

	title := strings.TrimSpace(args.Title)
	if title == "" {
		switch kind {
		case "human_decision":
			title = "Decision for you"
		case "human_action":
			title = "Action for you"
		default:
			title = "Update for you"
		}
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := brokerPostJSON(ctx, "/messages", map[string]any{
		"channel":  channel,
		"from":     slug,
		"kind":     kind,
		"title":    title,
		"content":  args.Content,
		"reply_to": replyTo,
	}, &result); err != nil {
		return toolError(err), nil, nil
	}

	location := "#" + channel
	if isOneOnOneMode() {
		location = "this direct session"
	}
	text := fmt.Sprintf("Sent %s to the human in %s as @%s", strings.TrimPrefix(kind, "human_"), location, slug)
	if result.ID != "" {
		text += " (" + result.ID + ")"
	}
	if replyTo != "" {
		text += " in reply to " + replyTo
	}
	text += "."
	return textResult(text), nil, nil
}

func defaultRequestKind(kind string) string {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return "choice"
	}
	return kind
}

func humanRequestOptionDefaults(kind string) ([]HumanInterviewOption, string) {
	switch strings.TrimSpace(kind) {
	case "approval":
		return []HumanInterviewOption{
			{ID: "approve", Label: "Approve", Description: "Green-light this and let the team execute immediately."},
			{ID: "approve_with_note", Label: "Approve with note", Description: "Proceed, but attach explicit constraints or guardrails.", RequiresText: true, TextHint: "Type the conditions, constraints, or guardrails the team must follow."},
			{ID: "reject", Label: "Reject", Description: "Do not proceed with this."},
			{ID: "reject_with_steer", Label: "Reject with steer", Description: "Do not proceed as proposed. Redirect the team with clearer steering.", RequiresText: true, TextHint: "Type the steering, redirect, or rationale for rejecting this request."},
			{ID: "hold", Label: "Hold", Description: "Pause until you review or unblock this yourself."},
		}, "approve"
	case "confirm":
		return []HumanInterviewOption{
			{ID: "confirm_proceed", Label: "Confirm", Description: "Looks good. Proceed as planned."},
			{ID: "adjust", Label: "Adjust", Description: "Proceed only after applying the changes you specify.", RequiresText: true, TextHint: "Type the changes that must happen before proceeding."},
			{ID: "reassign", Label: "Reassign", Description: "Move this to a different owner or scope.", RequiresText: true, TextHint: "Type who should own this instead, or how the scope should change."},
			{ID: "hold", Label: "Hold", Description: "Do not act yet. Keep this pending for review."},
		}, "confirm_proceed"
	case "choice":
		return []HumanInterviewOption{
			{ID: "move_fast", Label: "Move fast", Description: "Bias toward speed. Ship now and iterate later."},
			{ID: "balanced", Label: "Balanced", Description: "Balance speed, risk, and quality."},
			{ID: "be_careful", Label: "Be careful", Description: "Bias toward caution and a tighter review loop."},
			{ID: "needs_more_info", Label: "Need more info", Description: "Gather more context before deciding.", RequiresText: true, TextHint: "Type what is missing or what should be investigated next."},
			{ID: "delegate", Label: "Delegate", Description: "Hand this to a specific owner for a closer call.", RequiresText: true, TextHint: "Type who should own this decision and any guidance for them."},
		}, "balanced"
	case "freeform", "secret":
		return []HumanInterviewOption{
			{ID: "proceed", Label: "Proceed", Description: "Let the team handle it with their best judgment."},
			{ID: "give_direction", Label: "Give direction", Description: "Proceed, but only after you provide specific guidance.", RequiresText: true, TextHint: "Type the direction or constraints the team should follow."},
			{ID: "delegate", Label: "Delegate", Description: "Route this to a specific person.", RequiresText: true, TextHint: "Type who should own this and what they should do."},
			{ID: "hold", Label: "Hold", Description: "Pause until you review this further."},
		}, "proceed"
	default:
		return nil, ""
	}
}

func normalizeHumanRequestOptions(kind, recommendedID string, options []HumanInterviewOption) ([]HumanInterviewOption, string) {
	defaults, fallback := humanRequestOptionDefaults(kind)
	if len(options) == 0 {
		return defaults, chooseRecommendedID(strings.TrimSpace(recommendedID), fallback)
	}
	meta := make(map[string]HumanInterviewOption, len(defaults))
	for _, option := range defaults {
		meta[strings.TrimSpace(option.ID)] = option
	}
	out := make([]HumanInterviewOption, 0, len(options))
	for _, option := range options {
		if base, ok := meta[strings.TrimSpace(option.ID)]; ok {
			if !option.RequiresText {
				option.RequiresText = base.RequiresText
			}
			if strings.TrimSpace(option.TextHint) == "" {
				option.TextHint = base.TextHint
			}
			if strings.TrimSpace(option.Label) == "" {
				option.Label = base.Label
			}
			if strings.TrimSpace(option.Description) == "" {
				option.Description = base.Description
			}
		}
		out = append(out, option)
	}
	return out, chooseRecommendedID(strings.TrimSpace(recommendedID), fallback)
}

func chooseRecommendedID(preferred, fallback string) string {
	if preferred != "" {
		return preferred
	}
	return fallback
}

func formatRequestSummary(ctx context.Context, channel string) string {
	values := url.Values{}
	values.Set("channel", channel)
	var result brokerRequestsResponse
	path := "/requests?" + values.Encode()
	if err := brokerGetJSON(ctx, path, &result); err != nil || len(result.Requests) == 0 {
		return "Open requests: none"
	}
	lines := make([]string, 0, len(result.Requests))
	for _, req := range result.Requests {
		line := fmt.Sprintf("- %s [%s] @%s", req.ID, req.Kind, req.From)
		if req.Blocking {
			line += " · blocking"
		}
		if req.Title != "" {
			line += " — " + req.Title
		} else {
			line += " — " + req.Question
		}
		lines = append(lines, line)
	}
	return "Open requests:\n" + strings.Join(lines, "\n")
}
