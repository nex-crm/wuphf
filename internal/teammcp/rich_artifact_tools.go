package teammcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const notebookVisualArtifactGuidance = "Use this after notebook_write when the work would be clearer as a rich visual artifact: complex specs, implementation plans, code explainers, PR reviews, comparison grids, diagrams, mockups, reports, or interactive tuning surfaces. Default to the WUPHF technical-manual artifact style: old mathematics/physics book on real paper, warm paper texture, black editorial serif reading copy, exact Making Software cobalt shades for figure ink (including oklch(50.58% .2886 264.84) / rgb(19, 66, 255) as the primary stroke), muted complementary state colors, faint construction grids inside figure plates, monospaced figure labels like FIG_001, IN/OUT blocks, trust/source metadata, equations or measured annotations when useful, and table-of-contents-style lists. Keep it original to WUPHF; do not copy external logos, illustrations, or brand assets. HTML must be self-contained: inline CSS/JS only, no network fetches, no external images/scripts/fonts, responsive layout, readable copy, and copy/export controls when the artifact is interactive. The paired markdown notebook entry remains the durable source note; this HTML is the visual companion users review in notebooks and the wiki. After creating an artifact, include visual-artifact:ra_0123456789abcdef on its own line in the channel summary so chat renders a compact artifact card."

// TeamNotebookVisualArtifactCreateArgs is the contract for
// notebook_visual_artifact_create.
type TeamNotebookVisualArtifactCreateArgs struct {
	MySlug            string   `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG env."`
	TaskID            string   `json:"task_id,omitempty" jsonschema:"Task ID this visual artifact supports, when relevant."`
	SourcePath        string   `json:"source_path" jsonschema:"Notebook source path this HTML visualizes - MUST be agents/{my_slug}/notebook/{filename}.md. Create or update it with notebook_write first."`
	Title             string   `json:"title" jsonschema:"Short human-readable title for the visual artifact."`
	Summary           string   `json:"summary" jsonschema:"One or two sentence summary of what the artifact helps the human review."`
	HTML              string   `json:"html" jsonschema:"A complete, self-contained HTML document. Use inline CSS/JS only; do not rely on network fetches or external assets."`
	RelatedMessageID  string   `json:"related_message_id,omitempty" jsonschema:"Optional source message ID this artifact explains."`
	RelatedReceiptIDs []string `json:"related_receipt_ids,omitempty" jsonschema:"Optional receipt IDs this artifact explains."`
	CommitMsg         string   `json:"commit_message,omitempty" jsonschema:"Why this visual artifact exists - becomes the git commit message."`
}

// TeamNotebookVisualArtifactListArgs is the contract for
// notebook_visual_artifact_list.
type TeamNotebookVisualArtifactListArgs struct {
	TargetSlug string `json:"target_slug,omitempty" jsonschema:"Agent whose visual artifacts to list. Defaults to your own when source_path is omitted."`
	SourcePath string `json:"source_path,omitempty" jsonschema:"Optional notebook source path filter, like agents/{slug}/notebook/{filename}.md."`
}

// TeamNotebookVisualArtifactReadArgs is the contract for
// notebook_visual_artifact_read.
type TeamNotebookVisualArtifactReadArgs struct {
	ArtifactID string `json:"artifact_id" jsonschema:"Visual artifact ID returned by notebook_visual_artifact_create or notebook_visual_artifact_list, like ra_0123456789abcdef."`
}

// TeamNotebookVisualArtifactPromoteArgs is the contract for
// notebook_visual_artifact_promote.
type TeamNotebookVisualArtifactPromoteArgs struct {
	MySlug          string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG env."`
	ArtifactID      string `json:"artifact_id" jsonschema:"Visual artifact ID to promote, like ra_0123456789abcdef."`
	TargetWikiPath  string `json:"target_wiki_path" jsonschema:"Canonical wiki path - MUST start with team/ and end in .md."`
	MarkdownSummary string `json:"markdown_summary" jsonschema:"Canonical markdown article body to pair with the promoted visual artifact. Include the durable facts and links; the HTML remains the visual companion."`
	Mode            string `json:"mode,omitempty" jsonschema:"One of: create | replace | append_section. Defaults to create."`
	CommitMsg       string `json:"commit_message,omitempty" jsonschema:"Why this visual artifact is ready for the wiki - becomes the git commit message."`
}

func registerNotebookVisualArtifactTools(server *mcp.Server) {
	mcp.AddTool(server, officeWriteTool(
		"notebook_visual_artifact_create",
		"Create an HTML visual companion for a notebook entry. "+notebookVisualArtifactGuidance,
	), handleTeamNotebookVisualArtifactCreate)
	mcp.AddTool(server, readOnlyTool(
		"notebook_visual_artifact_list",
		"List HTML visual artifacts attached to notebook entries so you can reuse, inspect, or promote them. "+notebookVisualArtifactGuidance,
	), handleTeamNotebookVisualArtifactList)
	mcp.AddTool(server, readOnlyTool(
		"notebook_visual_artifact_read",
		"Read one HTML visual artifact and its metadata. Use this before editing or promoting an existing artifact. "+notebookVisualArtifactGuidance,
	), handleTeamNotebookVisualArtifactRead)
	mcp.AddTool(server, officeWriteTool(
		"notebook_visual_artifact_promote",
		"Promote a reviewed notebook HTML visual artifact into the canonical wiki visual view. "+notebookVisualArtifactGuidance,
	), handleTeamNotebookVisualArtifactPromote)
}

func handleTeamNotebookVisualArtifactCreate(ctx context.Context, _ *mcp.CallToolRequest, args TeamNotebookVisualArtifactCreateArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	sourcePath := strings.TrimSpace(args.SourcePath)
	if err := validateOwnedNotebookMarkdownPath(slug, sourcePath, "source_path"); err != nil {
		return toolError(err), nil, nil
	}
	title := strings.TrimSpace(args.Title)
	if title == "" {
		return toolError(fmt.Errorf("title is required")), nil, nil
	}
	if strings.TrimSpace(args.HTML) == "" {
		return toolError(fmt.Errorf("html is required")), nil, nil
	}

	var result map[string]any
	err = brokerPostJSON(ctx, "/notebook/visual-artifacts", map[string]any{
		"slug":                 slug,
		"title":                title,
		"summary":              strings.TrimSpace(args.Summary),
		"html":                 args.HTML,
		"source_markdown_path": sourcePath,
		"related_task_id":      strings.TrimSpace(args.TaskID),
		"related_message_id":   strings.TrimSpace(args.RelatedMessageID),
		"related_receipt_ids":  args.RelatedReceiptIDs,
		"commit_message":       strings.TrimSpace(args.CommitMsg),
	}, &result)
	if err != nil {
		return toolError(err), nil, nil
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return toolError(fmt.Errorf("marshal visual artifact create response: %w", err)), nil, nil
	}
	return textResult(string(payload)), nil, nil
}

func handleTeamNotebookVisualArtifactList(ctx context.Context, _ *mcp.CallToolRequest, args TeamNotebookVisualArtifactListArgs) (*mcp.CallToolResult, any, error) {
	q := url.Values{}
	sourcePath := strings.TrimSpace(args.SourcePath)
	if sourcePath != "" {
		if err := validateNotebookMarkdownPath(sourcePath, "source_path"); err != nil {
			return toolError(err), nil, nil
		}
		q.Set("source_path", sourcePath)
	}
	target := strings.TrimSpace(args.TargetSlug)
	if target == "" && sourcePath == "" {
		target = resolveSlugOptional("")
		if target == "" {
			return toolError(fmt.Errorf("target_slug is required (and WUPHF_AGENT_SLUG is not set)")), nil, nil
		}
	}
	if target != "" {
		q.Set("slug", target)
	}

	var result struct {
		Artifacts []map[string]any `json:"artifacts"`
	}
	path := "/notebook/visual-artifacts"
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}
	if err := brokerGetJSON(ctx, path, &result); err != nil {
		return toolError(err), nil, nil
	}
	if result.Artifacts == nil {
		result.Artifacts = []map[string]any{}
	}
	payload, err := json.Marshal(result.Artifacts)
	if err != nil {
		return toolError(fmt.Errorf("marshal visual artifact list response: %w", err)), nil, nil
	}
	return textResult(string(payload)), nil, nil
}

func handleTeamNotebookVisualArtifactRead(ctx context.Context, _ *mcp.CallToolRequest, args TeamNotebookVisualArtifactReadArgs) (*mcp.CallToolResult, any, error) {
	id := strings.TrimSpace(args.ArtifactID)
	if err := validateRichArtifactIDLocal(id); err != nil {
		return toolError(err), nil, nil
	}
	var result map[string]any
	if err := brokerGetJSON(ctx, "/notebook/visual-artifacts/"+url.PathEscape(id), &result); err != nil {
		return toolError(err), nil, nil
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return toolError(fmt.Errorf("marshal visual artifact read response: %w", err)), nil, nil
	}
	return textResult(string(payload)), nil, nil
}

func handleTeamNotebookVisualArtifactPromote(ctx context.Context, _ *mcp.CallToolRequest, args TeamNotebookVisualArtifactPromoteArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	id := strings.TrimSpace(args.ArtifactID)
	if err := validateRichArtifactIDLocal(id); err != nil {
		return toolError(err), nil, nil
	}
	targetPath := strings.TrimSpace(args.TargetWikiPath)
	if !strings.HasPrefix(targetPath, "team/") {
		return toolError(fmt.Errorf("target_wiki_path %q must start with team/", targetPath)), nil, nil
	}
	if !strings.HasSuffix(strings.ToLower(targetPath), ".md") {
		return toolError(fmt.Errorf("target_wiki_path must end in .md; got %q", targetPath)), nil, nil
	}
	markdown := strings.TrimSpace(args.MarkdownSummary)
	if markdown == "" {
		return toolError(fmt.Errorf("markdown_summary is required")), nil, nil
	}
	mode := strings.TrimSpace(args.Mode)
	if mode == "" {
		mode = "create"
	}
	switch mode {
	case "create", "replace", "append_section":
	default:
		return toolError(fmt.Errorf("mode must be one of create | replace | append_section; got %q", mode)), nil, nil
	}

	var result map[string]any
	err = brokerPostJSON(ctx, "/notebook/visual-artifacts/"+url.PathEscape(id)+"/promote", map[string]any{
		"actor_slug":       slug,
		"target_wiki_path": targetPath,
		"markdown_summary": markdown,
		"mode":             mode,
		"commit_message":   strings.TrimSpace(args.CommitMsg),
	}, &result)
	if err != nil {
		return toolError(err), nil, nil
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return toolError(fmt.Errorf("marshal visual artifact promote response: %w", err)), nil, nil
	}
	return textResult(string(payload)), nil, nil
}

func validateOwnedNotebookMarkdownPath(slug, path, field string) error {
	if err := validateNotebookMarkdownPath(path, field); err != nil {
		return err
	}
	expectedPrefix := "agents/" + slug + "/notebook/"
	if !strings.HasPrefix(path, expectedPrefix) {
		return fmt.Errorf("%s %q must start with %s", field, path, expectedPrefix)
	}
	return nil
}

func validateNotebookMarkdownPath(path, field string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("%s is required", field)
	}
	if !strings.HasPrefix(path, "agents/") || !strings.Contains(path, "/notebook/") {
		return fmt.Errorf("%s %q must be a notebook path like agents/{slug}/notebook/{filename}.md", field, path)
	}
	if !strings.HasSuffix(strings.ToLower(path), ".md") {
		return fmt.Errorf("%s must end in .md; got %q", field, path)
	}
	return nil
}

func validateRichArtifactIDLocal(id string) error {
	if len(id) != len("ra_")+16 || !strings.HasPrefix(id, "ra_") {
		return fmt.Errorf("artifact_id must look like ra_0123456789abcdef; got %q", id)
	}
	for _, ch := range strings.TrimPrefix(id, "ra_") {
		if !((ch >= 'a' && ch <= 'f') || (ch >= '0' && ch <= '9')) {
			return fmt.Errorf("artifact_id must look like ra_0123456789abcdef; got %q", id)
		}
	}
	return nil
}
