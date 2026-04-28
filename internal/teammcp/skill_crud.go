package teammcp

// skill_crud.go exposes the broker's PR 1b skill CRUD surface as MCP tools so
// agents can patch / edit / archive / write sub-resource files on existing
// skills without composing raw HTTP. Each tool proxies to the matching broker
// endpoint registered in skill_crud_endpoints.go.

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TeamSkillPatchArgs are the inputs for the team_skill_patch tool.
type TeamSkillPatchArgs struct {
	Name       string `json:"name" jsonschema:"Skill slug to patch (e.g. 'daily-digest')."`
	OldString  string `json:"old_string" jsonschema:"Exact text to find in the skill body. Must be unique unless replace_all=true."`
	NewString  string `json:"new_string" jsonschema:"Replacement text. Empty deletes the matched span."`
	FilePath   string `json:"file_path,omitempty" jsonschema:"Reserved for future sub-resource patches; leave empty for now."`
	ReplaceAll bool   `json:"replace_all,omitempty" jsonschema:"When true, replace every occurrence. Default false (single-match only)."`
}

// TeamSkillEditArgs are the inputs for the team_skill_edit tool.
type TeamSkillEditArgs struct {
	Name    string `json:"name" jsonschema:"Skill slug to overwrite."`
	Content string `json:"content" jsonschema:"Full SKILL.md document including YAML frontmatter delimiters. Must include name + description."`
}

// TeamSkillArchiveArgs are the inputs for the team_skill_archive tool.
type TeamSkillArchiveArgs struct {
	Name   string `json:"name" jsonschema:"Skill slug to archive."`
	Reason string `json:"reason,omitempty" jsonschema:"Optional human-readable reason for the archive (used in the commit message)."`
}

// TeamSkillWriteFileArgs are the inputs for the team_skill_write_file tool.
type TeamSkillWriteFileArgs struct {
	Name        string `json:"name" jsonschema:"Skill slug whose sub-resource file you want to create."`
	FilePath    string `json:"file_path" jsonschema:"Path under team/skills/{name}/ — must start with references/, templates/, scripts/, or assets/."`
	FileContent string `json:"file_content" jsonschema:"Raw file body. Max 1MiB."`
}

// registerSkillCRUDTools registers all PR 1b CRUD tools.
func registerSkillCRUDTools(server *mcp.Server) {
	mcp.AddTool(server, officeWriteTool(
		"team_skill_patch",
		"Apply a find-replace patch to an existing skill's body. Use for typo fixes or small content edits without re-uploading the entire SKILL.md. Set replace_all=true for global rename refactors.",
	), handleTeamSkillPatch)

	mcp.AddTool(server, officeWriteTool(
		"team_skill_edit",
		"Overwrite an existing skill's full SKILL.md (frontmatter + body). Re-runs the safety guard scan. Prefer team_skill_patch for small edits.",
	), handleTeamSkillEdit)

	mcp.AddTool(server, officeWriteTool(
		"team_skill_archive",
		"Archive an existing skill — flips status to archived, never hard-deletes. The skill stays in the wiki for history but is hidden from active routing.",
	), handleTeamSkillArchive)

	mcp.AddTool(server, officeWriteTool(
		"team_skill_write_file",
		"Write a sub-resource file under team/skills/{name}/. Allowed prefixes: references/, templates/, scripts/, assets/. Max 1MiB. Use for templates the skill body links to.",
	), handleTeamSkillWriteFile)
}

func handleTeamSkillPatch(ctx context.Context, _ *mcp.CallToolRequest, args TeamSkillPatchArgs) (*mcp.CallToolResult, any, error) {
	name := skillPathSegment(args.Name)
	if name == "" {
		return toolError(fmt.Errorf("name is required")), nil, nil
	}
	if strings.TrimSpace(args.OldString) == "" {
		return toolError(fmt.Errorf("old_string is required")), nil, nil
	}

	body := map[string]any{
		"old_string":  args.OldString,
		"new_string":  args.NewString,
		"replace_all": args.ReplaceAll,
	}
	if fp := strings.TrimSpace(args.FilePath); fp != "" {
		body["file_path"] = fp
	}

	var resp brokerSkillResponse
	if err := brokerPostJSON(ctx, "/skills/"+name+"/patch", body, &resp); err != nil {
		return toolError(fmt.Errorf("patch skill %q: %w", name, err)), nil, nil
	}
	return textResult(prettyObject(map[string]any{
		"ok":         true,
		"skill_name": resp.Skill.Name,
		"status":     resp.Skill.Status,
		"updated":    true,
	})), nil, nil
}

func handleTeamSkillEdit(ctx context.Context, _ *mcp.CallToolRequest, args TeamSkillEditArgs) (*mcp.CallToolResult, any, error) {
	name := skillPathSegment(args.Name)
	if name == "" {
		return toolError(fmt.Errorf("name is required")), nil, nil
	}
	if strings.TrimSpace(args.Content) == "" {
		return toolError(fmt.Errorf("content is required")), nil, nil
	}
	body := map[string]any{"content": args.Content}
	var resp brokerSkillResponse
	if err := brokerPutJSON(ctx, "/skills/"+name, body, &resp); err != nil {
		return toolError(fmt.Errorf("edit skill %q: %w", name, err)), nil, nil
	}
	return textResult(prettyObject(map[string]any{
		"ok":         true,
		"skill_name": resp.Skill.Name,
		"status":     resp.Skill.Status,
	})), nil, nil
}

func handleTeamSkillArchive(ctx context.Context, _ *mcp.CallToolRequest, args TeamSkillArchiveArgs) (*mcp.CallToolResult, any, error) {
	name := skillPathSegment(args.Name)
	if name == "" {
		return toolError(fmt.Errorf("name is required")), nil, nil
	}
	body := map[string]any{}
	if r := strings.TrimSpace(args.Reason); r != "" {
		body["reason"] = r
	}
	var resp brokerSkillResponse
	if err := brokerPostJSON(ctx, "/skills/"+name+"/archive", body, &resp); err != nil {
		return toolError(fmt.Errorf("archive skill %q: %w", name, err)), nil, nil
	}
	return textResult(prettyObject(map[string]any{
		"ok":         true,
		"skill_name": resp.Skill.Name,
		"status":     resp.Skill.Status,
	})), nil, nil
}

func handleTeamSkillWriteFile(ctx context.Context, _ *mcp.CallToolRequest, args TeamSkillWriteFileArgs) (*mcp.CallToolResult, any, error) {
	name := skillPathSegment(args.Name)
	if name == "" {
		return toolError(fmt.Errorf("name is required")), nil, nil
	}
	if strings.TrimSpace(args.FilePath) == "" {
		return toolError(fmt.Errorf("file_path is required")), nil, nil
	}
	body := map[string]any{
		"file_path":    args.FilePath,
		"file_content": args.FileContent,
	}
	var resp struct {
		OK       bool   `json:"ok"`
		Path     string `json:"path"`
		Bytes    int    `json:"bytes"`
		Skill    string `json:"skill"`
		FilePath string `json:"file_path"`
	}
	if err := brokerPostJSON(ctx, "/skills/"+name+"/files", body, &resp); err != nil {
		return toolError(fmt.Errorf("write file for skill %q: %w", name, err)), nil, nil
	}
	return textResult(prettyObject(map[string]any{
		"ok":         resp.OK,
		"skill_name": resp.Skill,
		"file_path":  resp.FilePath,
		"path":       resp.Path,
		"bytes":      resp.Bytes,
	})), nil, nil
}
