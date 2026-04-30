package teammcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TeamReviewsArgs lists notebook promotion reviews. Defaults to the caller's
// assigned queue so reviewer agents can discover pending promotions from task
// context without needing the web UI.
type TeamReviewsArgs struct {
	MySlug string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG env."`
	Scope  string `json:"scope,omitempty" jsonschema:"Review scope: mine, all, or a reviewer slug. Defaults to mine."`
}

// TeamReviewArgs acts on one notebook promotion review.
type TeamReviewArgs struct {
	MySlug    string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG env."`
	Action    string `json:"action" jsonschema:"One of: approve | request_changes | comment | resubmit | withdraw"`
	ReviewID  string `json:"review_id" jsonschema:"Review/promotion ID returned by notebook_promote or team_reviews."`
	Rationale string `json:"rationale,omitempty" jsonschema:"Reviewer rationale. Required for request_changes; optional for approve."`
	Body      string `json:"body,omitempty" jsonschema:"Comment body. Required when action=comment."`
}

func registerReviewTools(server *mcp.Server) {
	mcp.AddTool(server, readOnlyTool(
		"team_reviews",
		"List notebook promotion reviews. Defaults to your assigned queue; use scope=all only when coordinating the whole review backlog.",
	), handleTeamReviews)
	mcp.AddTool(server, officeWriteTool(
		"team_review",
		"Approve, request changes, comment on, resubmit, or withdraw a notebook promotion review. Approval is the action that writes an approved notebook entry into the canonical team wiki.",
	), handleTeamReview)
}

func handleTeamReviews(ctx context.Context, _ *mcp.CallToolRequest, args TeamReviewsArgs) (*mcp.CallToolResult, any, error) {
	scope := strings.TrimSpace(args.Scope)
	if scope == "" || scope == "mine" {
		if slug := strings.TrimSpace(resolveSlugOptional(args.MySlug)); slug != "" {
			scope = slug
		} else {
			scope = "mine"
		}
	}
	q := url.Values{}
	q.Set("scope", scope)
	var result struct {
		Reviews []map[string]any `json:"reviews"`
	}
	if err := brokerGetJSON(ctx, "/review/list?"+q.Encode(), &result); err != nil {
		return toolError(err), nil, nil
	}
	if result.Reviews == nil {
		result.Reviews = []map[string]any{}
	}
	payload, _ := json.Marshal(result.Reviews)
	return textResult(string(payload)), nil, nil
}

func handleTeamReview(ctx context.Context, _ *mcp.CallToolRequest, args TeamReviewArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	reviewID := strings.TrimSpace(args.ReviewID)
	if reviewID == "" {
		return toolError(fmt.Errorf("review_id is required")), nil, nil
	}
	action := strings.TrimSpace(strings.ToLower(args.Action))
	verb, err := reviewActionVerb(action)
	if err != nil {
		return toolError(err), nil, nil
	}
	body := map[string]string{"actor_slug": slug}
	switch verb {
	case "request-changes":
		rationale := strings.TrimSpace(args.Rationale)
		if rationale == "" {
			return toolError(fmt.Errorf("rationale is required for action=request_changes")), nil, nil
		}
		body["rationale"] = rationale
	case "approve":
		if rationale := strings.TrimSpace(args.Rationale); rationale != "" {
			body["rationale"] = rationale
		}
	case "comment":
		comment := strings.TrimSpace(args.Body)
		if comment == "" {
			return toolError(fmt.Errorf("body is required for action=comment")), nil, nil
		}
		body["body"] = comment
	}
	var result map[string]any
	if err := brokerPostJSON(ctx, "/review/"+url.PathEscape(reviewID)+"/"+verb, body, &result); err != nil {
		return toolError(err), nil, nil
	}
	payload, _ := json.Marshal(map[string]any{
		"review_id":        reviewID,
		"action":           action,
		"state":            result["state"],
		"target_wiki_path": result["target_path"],
		"commit_sha":       result["commit_sha"],
	})
	return textResult(string(payload)), nil, nil
}

func reviewActionVerb(action string) (string, error) {
	switch action {
	case "approve":
		return "approve", nil
	case "request_changes", "request-changes":
		return "request-changes", nil
	case "comment":
		return "comment", nil
	case "resubmit":
		return "resubmit", nil
	case "withdraw", "reject":
		return "reject", nil
	default:
		return "", fmt.Errorf("action must be one of approve | request_changes | comment | resubmit | withdraw; got %q", action)
	}
}
