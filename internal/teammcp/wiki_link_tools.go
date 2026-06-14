package teammcp

// wiki_link_tools.go defines the link_task_wiki MCP tool: the CEO/Librarian
// surface for explicitly linking wiki articles to a task. The inbound Slack
// context-packer delivers ONLY these explicitly task-linked wiki refs to a
// first-party agent ("explicitly task-linked wiki refs"), never a free wiki
// search, so this tool is the deliberate, audited way an article becomes
// readable in that channel.
//
// The backing field (teamTask.WikiRefs) and its apply path (MutateTask, which
// runs dedupePaths on body.WikiRefs) already exist. This tool only computes
// the desired full ref set and routes it through the existing broker mutation:
//
//   action="link"    add the given paths to the current set (dedup, order-stable)
//   action="replace" set the linked set to exactly the given paths
//   action="unlink"  remove the given paths from the current set
//
// Every path is validated before linking by hitting the broker's /wiki/read
// endpoint, which runs the canonical validateArticlePath check (rejecting
// traversal, absolute, non-team/, non-.md paths) AND confirms the path
// resolves to a real article (404 otherwise). Invalid paths are rejected with
// a clear, per-path message and never linked; for action="unlink" a path that
// does not currently exist on the task is simply a no-op (no validation
// needed — you can always detach something).

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TeamWikiLinkArgs is the contract for link_task_wiki.
type TeamWikiLinkArgs struct {
	MySlug string   `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG env."`
	TaskID string   `json:"task_id" jsonschema:"The task (Issue) id whose linked wiki articles you are changing."`
	Action string   `json:"action" jsonschema:"One of: link (add the given paths to the current set), replace (set the linked set to exactly the given paths), unlink (remove the given paths)."`
	Paths  []string `json:"paths" jsonschema:"Wiki-relative article paths, e.g. team/playbooks/onboarding.md. Each must be a real article under team/ ending in .md. For link/replace, every path is validated; invalid or missing articles are rejected."`
}

func registerWikiLinkTool(server *mcp.Server) {
	mcp.AddTool(server, officeWriteTool(
		"link_task_wiki",
		"Link, replace, or unlink the wiki articles attached to a task. The context-packer treats these explicitly task-linked articles as the ONLY wiki content first-party agents may receive for the task, so link the canonical references an agent needs and nothing more. action=link adds paths, action=replace sets the exact set, action=unlink removes paths. Each linked path must be a real article under team/ ending in .md; invalid or missing paths are rejected. Returns the resulting linked set.",
	), handleTeamWikiLink)
}

func handleTeamWikiLink(ctx context.Context, _ *mcp.CallToolRequest, args TeamWikiLinkArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	taskID := strings.TrimSpace(args.TaskID)
	if taskID == "" {
		return toolError(fmt.Errorf("task_id is required")), nil, nil
	}
	action := strings.ToLower(strings.TrimSpace(args.Action))
	switch action {
	case "link", "replace", "unlink":
	default:
		return toolError(fmt.Errorf("action must be one of link | replace | unlink; got %q", args.Action)), nil, nil
	}

	// Normalize the requested paths up front. Empty/blank entries are dropped;
	// duplicates within the request are collapsed (order-stable).
	requested := dedupeWikiPaths(args.Paths)
	if len(requested) == 0 {
		return toolError(fmt.Errorf("paths is required (at least one wiki article path)")), nil, nil
	}

	// Validate the requested paths before doing anything that touches task
	// state. unlink never validates: detaching a stale or even malformed ref
	// must always be possible (it just won't match anything live). link and
	// replace MUST validate, because they make an article readable to agents.
	if action != "unlink" {
		for _, p := range requested {
			// Cheap structural pre-check first so a clearly-bad path fails
			// with a precise message before any network round-trip. This
			// mirrors the broker's validateArticlePath rules; the broker
			// remains the authoritative validator below.
			if structErr := wikiArticlePathStructuralError(p); structErr != nil {
				return toolError(fmt.Errorf("refusing to link %q: %w", p, structErr)), nil, nil
			}
			// Authoritative check: the broker runs validateArticlePath AND
			// confirms the article actually exists (404 otherwise). A non-2xx
			// surfaces here as an error carrying the broker's own message.
			if _, readErr := brokerGetRaw(ctx, "/wiki/read?path="+url.QueryEscape(p)); readErr != nil {
				return toolError(fmt.Errorf("refusing to link %q: not a valid, existing wiki article: %w", p, readErr)), nil, nil
			}
		}
	}

	// For link/unlink we need the task's current linked set so we can compute
	// the delta and send the full list (MutateTask uses replace semantics for
	// WikiRefs). replace ignores the current set entirely.
	var current []string
	if action != "replace" {
		current, err = fetchTaskWikiRefs(ctx, taskID)
		if err != nil {
			return toolError(err), nil, nil
		}
	}

	next := computeWikiRefSet(current, requested, action)

	// Known v1 limitation: the existing MutateTask apply path only writes
	// body.WikiRefs when len > 0 (it treats an empty list as "leave the set
	// unchanged", not "clear it"). So a link tool call that would empty the set
	// — an unlink of the last ref, or a replace with everything removed —
	// cannot take effect through the path we are told to call and must not
	// re-edit. Rather than silently report an empty set the broker did not
	// actually store, fail loudly with the workaround. This keeps the tool
	// honest: it never claims a result the broker did not apply.
	if len(next) == 0 {
		return toolError(fmt.Errorf(
			"this would leave task %s with no linked wiki articles, which the broker cannot apply (an empty set is treated as \"unchanged\"); leave at least one linked article, or clear the last link from the Issue detail surface",
			taskID,
		)), nil, nil
	}

	// Route through the existing MutateTask path. action="comment" is open to
	// every actor (no CEO-only scope gate) and applies body.WikiRefs
	// independently of the comment itself; an empty details string is a no-op
	// for packet feedback, so this is a pure wiki-ref update with no spurious
	// comment. MutateTask runs dedupePaths on the list we send.
	payload := map[string]any{
		"action":     "comment",
		"id":         taskID,
		"created_by": slug,
		"wiki_refs":  next,
	}

	var result struct {
		Task struct {
			ID       string   `json:"id"`
			WikiRefs []string `json:"wiki_refs"`
		} `json:"task"`
	}
	if err := brokerPostJSON(ctx, "/tasks", payload, &result); err != nil {
		return toolError(err), nil, nil
	}

	linked := result.Task.WikiRefs
	if linked == nil {
		linked = []string{}
	}
	out, _ := json.Marshal(map[string]any{
		"task_id":      taskID,
		"action":       action,
		"wiki_refs":    linked,
		"linked_count": len(linked),
	})
	return textResult(string(out)), nil, nil
}

// fetchTaskWikiRefs reads the task's current linked wiki refs from the broker's
// GET /tasks/{id} detail endpoint, whose "task" snapshot carries wiki_refs.
func fetchTaskWikiRefs(ctx context.Context, taskID string) ([]string, error) {
	var resp struct {
		Task *struct {
			WikiRefs []string `json:"wiki_refs"`
		} `json:"task"`
	}
	if err := brokerGetJSON(ctx, "/tasks/"+url.PathEscape(taskID), &resp); err != nil {
		return nil, fmt.Errorf("read task %s: %w", taskID, err)
	}
	if resp.Task == nil {
		return nil, fmt.Errorf("task %s not found", taskID)
	}
	return resp.Task.WikiRefs, nil
}

// computeWikiRefSet returns the new linked set given the current set, the
// requested paths, and the action. The result is order-stable and deduped:
//   - link:    current followed by any requested paths not already present.
//   - replace: exactly the requested paths.
//   - unlink:  current minus any requested paths.
//
// Inputs are normalized (trimmed, slash-cleaned, blanks dropped) so the
// comparison is on the same canonical form MutateTask's dedupePaths produces.
func computeWikiRefSet(current, requested []string, action string) []string {
	cur := dedupeWikiPaths(current)
	req := dedupeWikiPaths(requested)
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "replace":
		return req
	case "unlink":
		remove := make(map[string]struct{}, len(req))
		for _, p := range req {
			remove[p] = struct{}{}
		}
		out := make([]string, 0, len(cur))
		for _, p := range cur {
			if _, drop := remove[p]; drop {
				continue
			}
			out = append(out, p)
		}
		return out
	default: // link
		seen := make(map[string]struct{}, len(cur)+len(req))
		out := make([]string, 0, len(cur)+len(req))
		for _, p := range cur {
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			out = append(out, p)
		}
		for _, p := range req {
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			out = append(out, p)
		}
		return out
	}
}

// dedupeWikiPaths trims, slash-normalizes, and dedups wiki paths preserving
// first-seen order. It mirrors the broker-side dedupePaths so the set the tool
// computes matches what MutateTask stores.
func dedupeWikiPaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		p = filepath.ToSlash(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

// wikiArticlePathStructuralError mirrors the broker's validateArticlePath so a
// clearly-malformed path fails fast with a precise message before any network
// call. validateArticlePath itself is unexported in internal/team; the broker
// remains the authoritative validator (re-run server-side on /wiki/read), so
// this is a best-effort, fail-fast pre-check and intentionally conservative —
// anything it passes is still checked by the broker.
func wikiArticlePathStructuralError(relPath string) error {
	relPath = strings.TrimSpace(relPath)
	if relPath == "" {
		return fmt.Errorf("article path is required")
	}
	if filepath.IsAbs(relPath) {
		return fmt.Errorf("article path must be relative")
	}
	clean := filepath.ToSlash(filepath.Clean(relPath))
	if strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") || clean == ".." {
		return fmt.Errorf("article path must not contain a parent-directory (..) segment")
	}
	if !strings.HasPrefix(clean, "team/") {
		return fmt.Errorf("article path must be within team/")
	}
	if !strings.HasSuffix(strings.ToLower(clean), ".md") {
		return fmt.Errorf("article path must end with .md")
	}
	return nil
}
