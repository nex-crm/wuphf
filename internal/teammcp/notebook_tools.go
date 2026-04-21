package teammcp

// notebook_tools.go defines the notebook_{write,read,list,search} MCP tools.
// Registered only when WUPHF_MEMORY_BACKEND=markdown — notebooks are the
// v1.1 per-agent draft workspace that sits on top of the wiki's git substrate.
//
// Tool shape (matches family of team_wiki_* tools):
//   notebook_write  — author-only. Path MUST start with agents/{my_slug}/notebook/.
//   notebook_read   — cross-agent. Any agent can read any other agent's draft.
//   notebook_list   — cross-agent. Defaults target_slug to caller when omitted.
//   notebook_search — cross-agent. Scoped to one target_slug's subtree.
//
// The write side is author-owned (enforced by the broker); reads / list /
// search are open across agents by design. Notebook privacy is by-convention,
// not by access control — the assumption is that agents on a team trust each
// other and can inspect one another's drafts for context.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TeamNotebookWriteArgs is the contract for notebook_write.
type TeamNotebookWriteArgs struct {
	MySlug      string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG env."`
	ArticlePath string `json:"article_path" jsonschema:"Path within wiki root — MUST be agents/{my_slug}/notebook/{filename}.md"`
	Mode        string `json:"mode" jsonschema:"One of: create | replace | append_section"`
	Content     string `json:"content" jsonschema:"Full entry content (create/replace) or new section text (append_section)"`
	CommitMsg   string `json:"commit_message" jsonschema:"Why this change — becomes the git commit message"`
}

// TeamNotebookReadArgs is the contract for notebook_read.
type TeamNotebookReadArgs struct {
	MySlug      string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Optional; not required for cross-agent reads."`
	ArticlePath string `json:"article_path" jsonschema:"Path like agents/{slug}/notebook/{filename}.md (may be another agent's)"`
}

// TeamNotebookListArgs is the contract for notebook_list.
type TeamNotebookListArgs struct {
	TargetSlug string `json:"target_slug,omitempty" jsonschema:"Agent whose notebook to list. Defaults to WUPHF_AGENT_SLUG (your own)."`
}

// TeamNotebookSearchArgs is the contract for notebook_search.
type TeamNotebookSearchArgs struct {
	TargetSlug string `json:"target_slug" jsonschema:"Agent whose notebook to search."`
	Pattern    string `json:"pattern" jsonschema:"Literal substring to search (not regex)."`
}

// TeamNotebookPromoteArgs is the contract for notebook_promote. Used to
// submit a draft notebook entry for reviewer approval + promotion to the
// canonical team wiki. Does NOT delete the source — the notebook entry is
// preserved with a back-link frontmatter block once approved.
type TeamNotebookPromoteArgs struct {
	MySlug         string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG env."`
	SourcePath     string `json:"source_path" jsonschema:"Notebook source path — MUST be agents/{my_slug}/notebook/{filename}.md"`
	TargetWikiPath string `json:"target_wiki_path" jsonschema:"Proposed wiki path — MUST start with team/ and end in .md (e.g. team/playbooks/q2-launch.md)"`
	Rationale      string `json:"rationale" jsonschema:"Why this entry is ready for promotion — the reviewer sees this as the commit message rationale."`
	ReviewerSlug   string `json:"reviewer_slug,omitempty" jsonschema:"Optional reviewer override. When omitted, the blueprint's reviewer_paths decides."`
}

// registerNotebookTools attaches the 4 notebook tools to the MCP server.
// Caller (configureServerTools, markdown branch) is responsible for gating on
// WUPHF_MEMORY_BACKEND; this function does not re-check the env.
func registerNotebookTools(server *mcp.Server) {
	mcp.AddTool(server, officeWriteTool(
		"notebook_write",
		"Write a draft entry to your personal notebook at agents/{my_slug}/notebook/{filename}.md. Use this for half-baked thoughts, working notes, and draft playbooks before anything is reviewed and promoted to the team wiki. Author-only: you cannot write to another agent's notebook.",
	), handleTeamNotebookWrite)
	mcp.AddTool(server, readOnlyTool(
		"notebook_read",
		"Read an entry from any agent's notebook (yours or a teammate's). Pass the full path like agents/{slug}/notebook/2026-04-20-retro.md.",
	), handleTeamNotebookRead)
	mcp.AddTool(server, readOnlyTool(
		"notebook_list",
		"List entries in one agent's notebook, newest first. Defaults to your own notebook when target_slug is omitted.",
	), handleTeamNotebookList)
	mcp.AddTool(server, readOnlyTool(
		"notebook_search",
		"Literal substring search scoped to one agent's notebook subtree. Pattern is matched as a substring (not a regex).",
	), handleTeamNotebookSearch)
	mcp.AddTool(server, officeWriteTool(
		"notebook_promote",
		"Submit a notebook entry for reviewer approval + promotion to the team wiki. Copy-not-move: once approved the source entry is retained with a back-link frontmatter block. Target path must start with team/ and end in .md.",
	), handleTeamNotebookPromote)
}

func handleTeamNotebookWrite(ctx context.Context, _ *mcp.CallToolRequest, args TeamNotebookWriteArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	path := strings.TrimSpace(args.ArticlePath)
	if path == "" {
		return toolError(fmt.Errorf("article_path is required")), nil, nil
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
	if strings.TrimSpace(args.Content) == "" {
		return toolError(fmt.Errorf("content is required")), nil, nil
	}
	// Client-side path enforcement — the broker re-validates but catching
	// here produces a cleaner tool error with no network round-trip.
	expectedPrefix := "agents/" + slug + "/notebook/"
	if !strings.HasPrefix(path, expectedPrefix) {
		return toolError(fmt.Errorf("notebook_path_not_author_owned: path %q must start with %s", path, expectedPrefix)), nil, nil
	}

	var result struct {
		Path         string `json:"path"`
		CommitSHA    string `json:"commit_sha"`
		BytesWritten int    `json:"bytes_written"`
	}
	err = brokerPostJSON(ctx, "/notebook/write", map[string]any{
		"slug":           slug,
		"path":           path,
		"mode":           mode,
		"content":        args.Content,
		"commit_message": args.CommitMsg,
	}, &result)
	if err != nil {
		return toolError(err), nil, nil
	}
	payload, _ := json.Marshal(map[string]any{
		"path":          result.Path,
		"commit_sha":    result.CommitSHA,
		"bytes_written": result.BytesWritten,
	})
	return textResult(string(payload)), nil, nil
}

func handleTeamNotebookRead(ctx context.Context, _ *mcp.CallToolRequest, args TeamNotebookReadArgs) (*mcp.CallToolResult, any, error) {
	path := strings.TrimSpace(args.ArticlePath)
	if path == "" {
		return toolError(fmt.Errorf("article_path is required")), nil, nil
	}
	q := url.Values{}
	q.Set("path", path)
	if slug := strings.TrimSpace(args.MySlug); slug != "" {
		q.Set("slug", slug)
	}
	bytes, err := brokerGetRaw(ctx, "/notebook/read?"+q.Encode())
	if err != nil {
		return toolError(err), nil, nil
	}
	return textResult(string(bytes)), nil, nil
}

func handleTeamNotebookList(ctx context.Context, _ *mcp.CallToolRequest, args TeamNotebookListArgs) (*mcp.CallToolResult, any, error) {
	target := strings.TrimSpace(args.TargetSlug)
	if target == "" {
		// Default to the caller's own notebook.
		target = resolveSlugOptional("")
	}
	if target == "" {
		return toolError(fmt.Errorf("target_slug is required (and WUPHF_AGENT_SLUG is not set)")), nil, nil
	}
	var result struct {
		Entries []map[string]any `json:"entries"`
	}
	if err := brokerGetJSON(ctx, "/notebook/list?slug="+url.QueryEscape(target), &result); err != nil {
		return toolError(err), nil, nil
	}
	if result.Entries == nil {
		result.Entries = []map[string]any{}
	}
	payload, _ := json.Marshal(result.Entries)
	return textResult(string(payload)), nil, nil
}

func handleTeamNotebookPromote(ctx context.Context, _ *mcp.CallToolRequest, args TeamNotebookPromoteArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	sourcePath := strings.TrimSpace(args.SourcePath)
	if sourcePath == "" {
		return toolError(fmt.Errorf("source_path is required")), nil, nil
	}
	expectedSourcePrefix := "agents/" + slug + "/notebook/"
	if !strings.HasPrefix(sourcePath, expectedSourcePrefix) {
		return toolError(fmt.Errorf("source_path %q must start with %s", sourcePath, expectedSourcePrefix)), nil, nil
	}
	if !strings.HasSuffix(strings.ToLower(sourcePath), ".md") {
		return toolError(fmt.Errorf("source_path must end in .md; got %q", sourcePath)), nil, nil
	}
	targetPath := strings.TrimSpace(args.TargetWikiPath)
	if targetPath == "" {
		return toolError(fmt.Errorf("target_wiki_path is required")), nil, nil
	}
	if !strings.HasPrefix(targetPath, "team/") {
		return toolError(fmt.Errorf("target_wiki_path %q must start with team/", targetPath)), nil, nil
	}
	if !strings.HasSuffix(strings.ToLower(targetPath), ".md") {
		return toolError(fmt.Errorf("target_wiki_path must end in .md; got %q", targetPath)), nil, nil
	}
	if strings.TrimSpace(args.Rationale) == "" {
		return toolError(fmt.Errorf("rationale is required")), nil, nil
	}

	var result struct {
		PromotionID  string `json:"promotion_id"`
		ReviewerSlug string `json:"reviewer_slug"`
		State        string `json:"state"`
		HumanOnly    bool   `json:"human_only"`
	}
	err = brokerPostJSON(ctx, "/notebook/promote", map[string]any{
		"my_slug":          slug,
		"source_path":      sourcePath,
		"target_wiki_path": targetPath,
		"rationale":        args.Rationale,
		"reviewer_slug":    strings.TrimSpace(args.ReviewerSlug),
	}, &result)
	if err != nil {
		return toolError(err), nil, nil
	}
	payload, _ := json.Marshal(result)
	return textResult(string(payload)), nil, nil
}

func handleTeamNotebookSearch(ctx context.Context, _ *mcp.CallToolRequest, args TeamNotebookSearchArgs) (*mcp.CallToolResult, any, error) {
	target := strings.TrimSpace(args.TargetSlug)
	if target == "" {
		return toolError(fmt.Errorf("target_slug is required")), nil, nil
	}
	pattern := strings.TrimSpace(args.Pattern)
	if pattern == "" {
		return toolError(fmt.Errorf("pattern is required")), nil, nil
	}
	q := url.Values{}
	q.Set("slug", target)
	q.Set("q", pattern)
	var result struct {
		Hits []map[string]any `json:"hits"`
	}
	if err := brokerGetJSON(ctx, "/notebook/search?"+q.Encode(), &result); err != nil {
		return toolError(err), nil, nil
	}
	if result.Hits == nil {
		result.Hits = []map[string]any{}
	}
	payload, _ := json.Marshal(result.Hits)
	return textResult(string(payload)), nil, nil
}
