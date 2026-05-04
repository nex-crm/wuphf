package teammcp

// masterinbox_tools.go defines the Master Inbox MCP tools:
//
//   masterinbox_draft_reply  — draft a reply to a prospect in Master Inbox
//   masterinbox_add_label    — add/correct a prospect label
//   masterinbox_get_prospect — look up a prospect by email
//
// Registered only when WUPHF_MASTERINBOX_API_KEY is set.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MasterInboxDraftReplyArgs is the contract for masterinbox_draft_reply.
type MasterInboxDraftReplyArgs struct {
	MySlug     string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG env."`
	ProspectID string `json:"prospect_id" jsonschema:"The Master Inbox prospect ID (base64-encoded identifier from the prospect record)."`
	Message    string `json:"message" jsonschema:"The draft reply body to create in Master Inbox. Keep concise and contextual."`
}

// MasterInboxAddLabelArgs is the contract for masterinbox_add_label.
type MasterInboxAddLabelArgs struct {
	MySlug     string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG env."`
	ProspectID string `json:"prospect_id" jsonschema:"The Master Inbox prospect ID."`
	LabelID    string `json:"label_id" jsonschema:"The label ID to add to the prospect."`
}

// MasterInboxGetProspectArgs is the contract for masterinbox_get_prospect.
type MasterInboxGetProspectArgs struct {
	MySlug string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG env."`
	Email  string `json:"email" jsonschema:"The prospect email address to look up."`
}

// registerMasterInboxTools attaches the Master Inbox tools to the MCP server.
// Caller is responsible for gating on WUPHF_MASTERINBOX_API_KEY.
func registerMasterInboxTools(server *mcp.Server) {
	mcp.AddTool(server, officeWriteTool(
		"masterinbox_draft_reply",
		"Draft a reply to a prospect in Master Inbox. Creates a draft (not sent) that the human can review and send from Master Inbox. Requires prospect_id and message. Always check entity context and playbooks before drafting.",
	), handleMasterInboxDraftReply)
	mcp.AddTool(server, officeWriteTool(
		"masterinbox_add_label",
		"Add a label to a prospect in Master Inbox. Use to classify prospects (e.g., hot, cold, interested, pricing-inquiry). Requires prospect_id and label_id.",
	), handleMasterInboxAddLabel)
	mcp.AddTool(server, readOnlyTool(
		"masterinbox_get_prospect",
		"Look up a prospect by email in Master Inbox. Returns the prospect's name, company, title, and other profile data. Use to enrich entity context before drafting replies.",
	), handleMasterInboxGetProspect)
}

func handleMasterInboxDraftReply(ctx context.Context, _ *mcp.CallToolRequest, args MasterInboxDraftReplyArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	prospectID := strings.TrimSpace(args.ProspectID)
	message := strings.TrimSpace(args.Message)
	if prospectID == "" {
		return toolError(fmt.Errorf("prospect_id is required")), nil, nil
	}
	if message == "" {
		return toolError(fmt.Errorf("message is required")), nil, nil
	}

	var result struct {
		Status string `json:"status"`
	}
	err = brokerPostJSON(ctx, "/masterinbox/draft", map[string]any{
		"prospect_id": prospectID,
		"message":     message,
	}, &result)
	if err != nil {
		return toolError(fmt.Errorf("draft failed: %w", err)), nil, nil
	}

	return textResult(fmt.Sprintf("Draft created for prospect %s by @%s. The human can review and send from Master Inbox.", prospectID, slug)), nil, nil
}

func handleMasterInboxAddLabel(ctx context.Context, _ *mcp.CallToolRequest, args MasterInboxAddLabelArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	prospectID := strings.TrimSpace(args.ProspectID)
	labelID := strings.TrimSpace(args.LabelID)
	if prospectID == "" {
		return toolError(fmt.Errorf("prospect_id is required")), nil, nil
	}
	if labelID == "" {
		return toolError(fmt.Errorf("label_id is required")), nil, nil
	}

	var result struct {
		Status string `json:"status"`
	}
	err = brokerPostJSON(ctx, "/masterinbox/label", map[string]any{
		"prospect_id": prospectID,
		"label_id":    labelID,
	}, &result)
	if err != nil {
		return toolError(fmt.Errorf("label add failed: %w", err)), nil, nil
	}

	return textResult(fmt.Sprintf("Label %s added to prospect %s by @%s.", labelID, prospectID, slug)), nil, nil
}

func handleMasterInboxGetProspect(ctx context.Context, _ *mcp.CallToolRequest, args MasterInboxGetProspectArgs) (*mcp.CallToolResult, any, error) {
	_, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	email := strings.TrimSpace(args.Email)
	if email == "" {
		return toolError(fmt.Errorf("email is required")), nil, nil
	}

	var prospect json.RawMessage
	err = brokerPostJSON(ctx, "/masterinbox/prospect", map[string]any{
		"email": email,
	}, &prospect)
	if err != nil {
		return toolError(fmt.Errorf("prospect lookup failed: %w", err)), nil, nil
	}

	return textResult(string(prospect)), nil, nil
}
