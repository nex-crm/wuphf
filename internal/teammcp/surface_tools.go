package teammcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type TeamSurfaceListArgs struct {
	MySlug string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG env."`
}

type TeamSurfaceCreateArgs struct {
	MySlug  string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG env."`
	ID      string `json:"id,omitempty" jsonschema:"Optional kebab-case surface ID. Omit to derive from title."`
	Title   string `json:"title" jsonschema:"Human-readable surface title."`
	Channel string `json:"channel,omitempty" jsonschema:"Channel this surface belongs to. Defaults to general."`
}

type TeamSurfaceReadArgs struct {
	MySlug    string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG env."`
	SurfaceID string `json:"surface_id" jsonschema:"Surface ID to read."`
}

type TeamSurfacePatchArgs struct {
	MySlug    string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG env."`
	SurfaceID string `json:"surface_id" jsonschema:"Surface ID to patch."`
	Title     string `json:"title,omitempty" jsonschema:"New title, if changing it."`
}

type TeamWidgetListArgs struct {
	MySlug    string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG env."`
	SurfaceID string `json:"surface_id" jsonschema:"Surface ID whose widgets should be listed."`
}

type TeamWidgetReadArgs struct {
	MySlug    string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG env."`
	SurfaceID string `json:"surface_id" jsonschema:"Surface ID."`
	WidgetID  string `json:"widget_id" jsonschema:"Widget ID."`
}

type TeamWidgetUpsertArgs struct {
	MySlug      string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG env."`
	SurfaceID   string `json:"surface_id" jsonschema:"Surface ID."`
	WidgetID    string `json:"widget_id,omitempty" jsonschema:"Optional kebab-case widget ID. Omit to derive from title."`
	Title       string `json:"title" jsonschema:"Widget title."`
	Description string `json:"description,omitempty" jsonschema:"Short widget description."`
	Kind        string `json:"kind" jsonschema:"One of checklist, table, markdown."`
	Source      string `json:"source" jsonschema:"Declarative widget source YAML. This is the canonical patchable source block."`
}

type TeamWidgetPatchArgs struct {
	MySlug      string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG env."`
	SurfaceID   string `json:"surface_id" jsonschema:"Surface ID."`
	WidgetID    string `json:"widget_id" jsonschema:"Widget ID."`
	Mode        string `json:"mode,omitempty" jsonschema:"Patch mode: line or snippet. Inferred from fields when omitted."`
	StartLine   int    `json:"start_line,omitempty" jsonschema:"1-based start line for line patch."`
	EndLine     int    `json:"end_line,omitempty" jsonschema:"1-based inclusive end line for line patch."`
	Search      string `json:"search,omitempty" jsonschema:"Exact snippet to replace. Must match exactly once."`
	Replacement string `json:"replacement" jsonschema:"Replacement text."`
}

type TeamWidgetRenderCheckArgs struct {
	MySlug    string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG env."`
	SurfaceID string `json:"surface_id" jsonschema:"Surface ID."`
	WidgetID  string `json:"widget_id" jsonschema:"Widget ID."`
	Title     string `json:"title,omitempty" jsonschema:"Optional candidate title when checking unsaved source."`
	Kind      string `json:"kind,omitempty" jsonschema:"Optional candidate kind when checking unsaved source."`
	Source    string `json:"source,omitempty" jsonschema:"Optional candidate source YAML. Omit to check the saved widget."`
}

type TeamSurfacePublishUpdateArgs struct {
	MySlug    string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG env."`
	SurfaceID string `json:"surface_id" jsonschema:"Surface ID."`
	Content   string `json:"content" jsonschema:"Compact channel update to publish with a Studio link."`
}

func registerSurfaceTools(server *mcp.Server) {
	mcp.AddTool(server, readOnlyTool(
		"team_surface_list",
		"List Studio surfaces visible to you. Use before creating a new surface so you do not duplicate an existing command center.",
	), handleTeamSurfaceList)
	mcp.AddTool(server, officeWriteTool(
		"team_surface_create",
		"Create a channel-bound Studio surface. The surface persists in WUPHF and can hold agent-authored widgets.",
	), handleTeamSurfaceCreate)
	mcp.AddTool(server, readOnlyTool(
		"team_surface_read",
		"Read a Studio surface, including widgets, rendered previews, history, and numbered source lines.",
	), handleTeamSurfaceRead)
	mcp.AddTool(server, officeWriteTool(
		"team_surface_patch",
		"Patch surface metadata such as the title. Widget source edits should use team_widget_patch.",
	), handleTeamSurfacePatch)
	mcp.AddTool(server, readOnlyTool(
		"team_widget_list",
		"List widgets on a Studio surface.",
	), handleTeamWidgetList)
	mcp.AddTool(server, readOnlyTool(
		"team_widget_read",
		"Read one widget with numbered source lines and the current Go render preview.",
	), handleTeamWidgetRead)
	mcp.AddTool(server, readOnlyTool(
		"team_widget_see_rendered",
		"Inspect the current rendered preview for one widget without changing it.",
	), handleTeamWidgetSeeRendered)
	mcp.AddTool(server, officeWriteTool(
		"team_widget_upsert",
		"Create or replace a trusted declarative Studio widget. Use for new widgets and rare full rewrites.",
	), handleTeamWidgetUpsert)
	mcp.AddTool(server, officeWriteTool(
		"team_widget_patch",
		"Patch an existing widget source by line range or exact snippet. Prefer this after a widget exists.",
	), handleTeamWidgetPatch)
	mcp.AddTool(server, readOnlyTool(
		"team_widget_render_check",
		"Validate a saved or candidate widget and return schema_ok, render_ok, normalized_widget, preview_text, and errors.",
	), handleTeamWidgetRenderCheck)
	mcp.AddTool(server, officeWriteTool(
		"team_surface_publish_update",
		"Publish a compact channel update that links humans back to the Studio surface.",
	), handleTeamSurfacePublishUpdate)
}

func handleTeamSurfaceList(ctx context.Context, _ *mcp.CallToolRequest, args TeamSurfaceListArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	var result map[string]any
	if err := brokerGetJSON(ctx, "/surfaces?viewer_slug="+url.QueryEscape(slug), &result); err != nil {
		return toolError(err), nil, nil
	}
	return jsonTextResult(result), nil, nil
}

func handleTeamSurfaceCreate(ctx context.Context, _ *mcp.CallToolRequest, args TeamSurfaceCreateArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	if strings.TrimSpace(args.Title) == "" {
		return toolError(fmt.Errorf("title is required")), nil, nil
	}
	body := map[string]any{
		"id":         strings.TrimSpace(args.ID),
		"title":      strings.TrimSpace(args.Title),
		"channel":    strings.TrimSpace(args.Channel),
		"created_by": slug,
		"my_slug":    slug,
	}
	var result map[string]any
	if err := brokerPostJSON(ctx, "/surfaces", body, &result); err != nil {
		return toolError(err), nil, nil
	}
	return jsonTextResult(result), nil, nil
}

func handleTeamSurfaceRead(ctx context.Context, _ *mcp.CallToolRequest, args TeamSurfaceReadArgs) (*mcp.CallToolResult, any, error) {
	path, err := surfacePathWithSlug(args.SurfaceID, args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	var result map[string]any
	if err := brokerGetJSON(ctx, path, &result); err != nil {
		return toolError(err), nil, nil
	}
	return jsonTextResult(result), nil, nil
}

func handleTeamSurfacePatch(ctx context.Context, _ *mcp.CallToolRequest, args TeamSurfacePatchArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	surfaceID := strings.TrimSpace(args.SurfaceID)
	if surfaceID == "" {
		return toolError(fmt.Errorf("surface_id is required")), nil, nil
	}
	body := map[string]any{"my_slug": slug, "actor": slug}
	if title := strings.TrimSpace(args.Title); title != "" {
		body["title"] = title
	}
	var result map[string]any
	if err := brokerDoJSON(ctx, http.MethodPatch, "/surfaces/"+url.PathEscape(surfaceID), body, &result); err != nil {
		return toolError(err), nil, nil
	}
	return jsonTextResult(result), nil, nil
}

func handleTeamWidgetList(ctx context.Context, _ *mcp.CallToolRequest, args TeamWidgetListArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	surfaceID := strings.TrimSpace(args.SurfaceID)
	if surfaceID == "" {
		return toolError(fmt.Errorf("surface_id is required")), nil, nil
	}
	var result map[string]any
	path := "/surfaces/" + url.PathEscape(surfaceID) + "/widgets?viewer_slug=" + url.QueryEscape(slug)
	if err := brokerGetJSON(ctx, path, &result); err != nil {
		return toolError(err), nil, nil
	}
	return jsonTextResult(result), nil, nil
}

func handleTeamWidgetRead(ctx context.Context, _ *mcp.CallToolRequest, args TeamWidgetReadArgs) (*mcp.CallToolResult, any, error) {
	path, err := widgetPathWithSlug(args.SurfaceID, args.WidgetID, args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	var result map[string]any
	if err := brokerGetJSON(ctx, path, &result); err != nil {
		return toolError(err), nil, nil
	}
	return jsonTextResult(result), nil, nil
}

func handleTeamWidgetSeeRendered(ctx context.Context, req *mcp.CallToolRequest, args TeamWidgetReadArgs) (*mcp.CallToolResult, any, error) {
	return handleTeamWidgetRead(ctx, req, args)
}

func handleTeamWidgetUpsert(ctx context.Context, _ *mcp.CallToolRequest, args TeamWidgetUpsertArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	surfaceID := strings.TrimSpace(args.SurfaceID)
	if surfaceID == "" {
		return toolError(fmt.Errorf("surface_id is required")), nil, nil
	}
	if strings.TrimSpace(args.Title) == "" || strings.TrimSpace(args.Kind) == "" || strings.TrimSpace(args.Source) == "" {
		return toolError(fmt.Errorf("title, kind, and source are required")), nil, nil
	}
	body := map[string]any{
		"my_slug": slug,
		"actor":   slug,
		"widget": map[string]any{
			"id":          strings.TrimSpace(args.WidgetID),
			"title":       strings.TrimSpace(args.Title),
			"description": strings.TrimSpace(args.Description),
			"kind":        strings.TrimSpace(args.Kind),
			"source":      args.Source,
			"created_by":  slug,
			"updated_by":  slug,
		},
	}
	var result map[string]any
	if err := brokerPostJSON(ctx, "/surfaces/"+url.PathEscape(surfaceID)+"/widgets", body, &result); err != nil {
		return toolError(err), nil, nil
	}
	return jsonTextResult(result), nil, nil
}

func handleTeamWidgetPatch(ctx context.Context, _ *mcp.CallToolRequest, args TeamWidgetPatchArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	surfaceID := strings.TrimSpace(args.SurfaceID)
	widgetID := strings.TrimSpace(args.WidgetID)
	if surfaceID == "" || widgetID == "" {
		return toolError(fmt.Errorf("surface_id and widget_id are required")), nil, nil
	}
	if strings.TrimSpace(args.Replacement) == "" {
		return toolError(fmt.Errorf("replacement is required")), nil, nil
	}
	body := map[string]any{
		"actor":       slug,
		"mode":        strings.TrimSpace(args.Mode),
		"start_line":  args.StartLine,
		"end_line":    args.EndLine,
		"search":      args.Search,
		"replacement": args.Replacement,
	}
	var result map[string]any
	path := "/surfaces/" + url.PathEscape(surfaceID) + "/widgets/" + url.PathEscape(widgetID)
	if err := brokerDoJSON(ctx, http.MethodPatch, path, body, &result); err != nil {
		return toolError(err), nil, nil
	}
	return jsonTextResult(result), nil, nil
}

func handleTeamWidgetRenderCheck(ctx context.Context, _ *mcp.CallToolRequest, args TeamWidgetRenderCheckArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	surfaceID := strings.TrimSpace(args.SurfaceID)
	widgetID := strings.TrimSpace(args.WidgetID)
	if surfaceID == "" || widgetID == "" {
		return toolError(fmt.Errorf("surface_id and widget_id are required")), nil, nil
	}
	body := map[string]any{"my_slug": slug, "actor": slug}
	if strings.TrimSpace(args.Source) != "" {
		body["widget"] = map[string]any{
			"id":     widgetID,
			"title":  strings.TrimSpace(args.Title),
			"kind":   strings.TrimSpace(args.Kind),
			"source": args.Source,
		}
	}
	var result map[string]any
	path := "/surfaces/" + url.PathEscape(surfaceID) + "/widgets/" + url.PathEscape(widgetID) + "/render-check"
	if err := brokerPostJSON(ctx, path, body, &result); err != nil {
		return toolError(err), nil, nil
	}
	return jsonTextResult(result), nil, nil
}

func handleTeamSurfacePublishUpdate(ctx context.Context, _ *mcp.CallToolRequest, args TeamSurfacePublishUpdateArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	if strings.TrimSpace(args.Content) == "" {
		return toolError(fmt.Errorf("content is required")), nil, nil
	}
	path, err := surfacePathWithSlug(args.SurfaceID, slug)
	if err != nil {
		return toolError(err), nil, nil
	}
	var surface struct {
		Surface struct {
			Channel string `json:"channel"`
			Title   string `json:"title"`
		} `json:"surface"`
	}
	if err := brokerGetJSON(ctx, path, &surface); err != nil {
		return toolError(err), nil, nil
	}
	content := strings.TrimSpace(args.Content) + "\n\nOpen Studio: #/apps/studio"
	var result map[string]any
	if err := brokerPostJSON(ctx, "/messages", map[string]any{
		"from":    slug,
		"channel": surface.Surface.Channel,
		"kind":    "status",
		"title":   surface.Surface.Title,
		"content": content,
	}, &result); err != nil {
		return toolError(err), nil, nil
	}
	return jsonTextResult(result), nil, nil
}

func surfacePathWithSlug(surfaceID, slugInput string) (string, error) {
	slug, err := resolveSlug(slugInput)
	if err != nil {
		return "", err
	}
	surfaceID = strings.TrimSpace(surfaceID)
	if surfaceID == "" {
		return "", fmt.Errorf("surface_id is required")
	}
	return "/surfaces/" + url.PathEscape(surfaceID) + "?viewer_slug=" + url.QueryEscape(slug), nil
}

func widgetPathWithSlug(surfaceID, widgetID, slugInput string) (string, error) {
	slug, err := resolveSlug(slugInput)
	if err != nil {
		return "", err
	}
	surfaceID = strings.TrimSpace(surfaceID)
	widgetID = strings.TrimSpace(widgetID)
	if surfaceID == "" || widgetID == "" {
		return "", fmt.Errorf("surface_id and widget_id are required")
	}
	return "/surfaces/" + url.PathEscape(surfaceID) + "/widgets/" + url.PathEscape(widgetID) + "?viewer_slug=" + url.QueryEscape(slug), nil
}

func jsonTextResult(value any) *mcp.CallToolResult {
	payload, _ := json.Marshal(value)
	return textResult(string(payload))
}

func brokerDoJSON(ctx context.Context, method, path string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, brokerBaseURL()+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header = authHeaders()
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("broker %s %s failed: %s %s", method, path, res.Status, strings.TrimSpace(string(respBody)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}
