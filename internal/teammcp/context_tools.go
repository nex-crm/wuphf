package teammcp

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/team"
)

const (
	contextDefaultLimit     = 5
	contextMaxLimit         = 20
	contextDefaultTimeout   = 5 * time.Second
	contextMaxTimeout       = 30 * time.Second
	contextSnippetMaxLength = 360
)

var (
	contextResolveMemoryBackendStatus = team.ResolveMemoryBackendStatus
	contextQuerySharedMemory          = team.QuerySharedMemory
	contextNow                        = time.Now
)

type ContextLookupArgs struct {
	Query          string `json:"query" jsonschema:"What prior context to look up"`
	TaskID         string `json:"task_id,omitempty" jsonschema:"Optional office task ID whose memory workflow should record this lookup"`
	TaskType       string `json:"task_type,omitempty" jsonschema:"Optional task type such as research, process, strategy, or implementation"`
	Limit          int    `json:"limit,omitempty" jsonschema:"Maximum citations per source (default 5, max 20)"`
	IncludePrivate bool   `json:"include_private,omitempty" jsonschema:"Include notebook/private working context when the backend supports it. Defaults to true when both include flags are omitted."`
	IncludeShared  bool   `json:"include_shared,omitempty" jsonschema:"Include shared wiki/org context when the backend supports it. Defaults to true when both include flags are omitted."`
	TimeoutMS      int    `json:"timeout_ms,omitempty" jsonschema:"Total lookup deadline in milliseconds (default 5000, max 30000)"`
	MySlug         string `json:"my_slug,omitempty" jsonschema:"Agent slug for task workflow attribution. Defaults to WUPHF_AGENT_SLUG."`
}

type ContextCaptureArgs struct {
	TaskID       string `json:"task_id,omitempty" jsonschema:"Optional office task ID whose memory workflow should record this capture"`
	MySlug       string `json:"my_slug,omitempty" jsonschema:"Agent slug writing the notebook entry. Defaults to WUPHF_AGENT_SLUG."`
	Title        string `json:"title,omitempty" jsonschema:"Short title for the captured note"`
	Content      string `json:"content,omitempty" jsonschema:"Markdown note content to save to the caller's notebook"`
	NotebookPath string `json:"notebook_path,omitempty" jsonschema:"Optional notebook path. Defaults to agents/{my_slug}/notebook/{date}-{title}.md"`
	Mode         string `json:"mode,omitempty" jsonschema:"Notebook write mode: create, replace, or append_section. Defaults to create."`
	SkipReason   string `json:"skip_reason,omitempty" jsonschema:"Explicit reason no notebook capture is needed for this task"`
}

type ContextPromoteArgs struct {
	TaskID         string `json:"task_id,omitempty" jsonschema:"Optional office task ID whose memory workflow should record this promotion decision"`
	MySlug         string `json:"my_slug,omitempty" jsonschema:"Agent slug submitting the promotion. Defaults to WUPHF_AGENT_SLUG."`
	SourcePath     string `json:"source_path,omitempty" jsonschema:"Notebook source path, e.g. agents/{my_slug}/notebook/process.md"`
	TargetWikiPath string `json:"target_wiki_path,omitempty" jsonschema:"Proposed wiki path, e.g. team/processes/passport.md"`
	Rationale      string `json:"rationale,omitempty" jsonschema:"Why this notebook entry is ready for shared wiki promotion"`
	ReviewerSlug   string `json:"reviewer_slug,omitempty" jsonschema:"Optional reviewer override"`
	SkipReason     string `json:"skip_reason,omitempty" jsonschema:"Explicit reason no wiki promotion is needed for this task"`
}

type ContextHealthArgs struct {
	TaskID string `json:"task_id,omitempty" jsonschema:"Optional office task ID whose memory workflow state should be returned"`
}

type ContextToolStatus struct {
	OK        bool   `json:"ok"`
	Code      string `json:"code"`
	Message   string `json:"message,omitempty"`
	Retryable bool   `json:"retryable,omitempty"`
}

type ContextBackendStatus struct {
	SelectedKind  string `json:"selected_kind"`
	SelectedLabel string `json:"selected_label,omitempty"`
	ActiveKind    string `json:"active_kind"`
	ActiveLabel   string `json:"active_label,omitempty"`
	Active        bool   `json:"active"`
	Detail        string `json:"detail,omitempty"`
	NextStep      string `json:"next_step,omitempty"`
}

type ContextPartialError struct {
	Source    string `json:"source"`
	Backend   string `json:"backend,omitempty"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable,omitempty"`
}

type ContextCitation struct {
	Corpus     string   `json:"corpus"`
	Backend    string   `json:"backend"`
	Scope      string   `json:"scope,omitempty"`
	Identifier string   `json:"identifier,omitempty"`
	Path       string   `json:"path,omitempty"`
	Line       int      `json:"line,omitempty"`
	Title      string   `json:"title,omitempty"`
	Snippet    string   `json:"snippet,omitempty"`
	OwnerSlug  string   `json:"owner_slug,omitempty"`
	Slug       string   `json:"slug,omitempty"`
	PageID     int      `json:"page_id,omitempty"`
	ChunkID    int      `json:"chunk_id,omitempty"`
	ChunkIndex int      `json:"chunk_index,omitempty"`
	Source     string   `json:"source,omitempty"`
	Score      *float64 `json:"score,omitempty"`
	Stale      *bool    `json:"stale,omitempty"`
}

type ContextWorkflowUpdate struct {
	Attempted bool           `json:"attempted"`
	Updated   bool           `json:"updated"`
	TaskID    string         `json:"task_id,omitempty"`
	Event     string         `json:"event,omitempty"`
	Response  map[string]any `json:"response,omitempty"`
	Error     string         `json:"error,omitempty"`
	Code      string         `json:"code,omitempty"`
}

type ContextLookupResult struct {
	Tool          string                 `json:"tool"`
	Query         string                 `json:"query"`
	TaskID        string                 `json:"task_id,omitempty"`
	TaskType      string                 `json:"task_type,omitempty"`
	Limit         int                    `json:"limit"`
	Backend       ContextBackendStatus   `json:"backend"`
	Status        ContextToolStatus      `json:"status"`
	Citations     []ContextCitation      `json:"citations"`
	PartialErrors []ContextPartialError  `json:"partial_errors,omitempty"`
	Workflow      *ContextWorkflowUpdate `json:"workflow,omitempty"`
	ReadableText  string                 `json:"readable_text,omitempty"`
}

type ContextNotebookArtifact struct {
	Path         string `json:"path,omitempty"`
	CommitSHA    string `json:"commit_sha,omitempty"`
	BytesWritten int    `json:"bytes_written,omitempty"`
	Skipped      bool   `json:"skipped,omitempty"`
	SkipReason   string `json:"skip_reason,omitempty"`
}

type ContextCaptureResult struct {
	Tool          string                   `json:"tool"`
	TaskID        string                   `json:"task_id,omitempty"`
	Backend       ContextBackendStatus     `json:"backend"`
	Status        ContextToolStatus        `json:"status"`
	Notebook      *ContextNotebookArtifact `json:"notebook,omitempty"`
	Workflow      *ContextWorkflowUpdate   `json:"workflow,omitempty"`
	PartialErrors []ContextPartialError    `json:"partial_errors,omitempty"`
	ReadableText  string                   `json:"readable_text,omitempty"`
}

type ContextPromotionArtifact struct {
	PromotionID    string `json:"promotion_id,omitempty"`
	ReviewerSlug   string `json:"reviewer_slug,omitempty"`
	State          string `json:"state,omitempty"`
	HumanOnly      bool   `json:"human_only,omitempty"`
	SourcePath     string `json:"source_path,omitempty"`
	TargetWikiPath string `json:"target_wiki_path,omitempty"`
	Skipped        bool   `json:"skipped,omitempty"`
	SkipReason     string `json:"skip_reason,omitempty"`
}

type ContextPromoteResult struct {
	Tool          string                    `json:"tool"`
	TaskID        string                    `json:"task_id,omitempty"`
	Backend       ContextBackendStatus      `json:"backend"`
	Status        ContextToolStatus         `json:"status"`
	Promotion     *ContextPromotionArtifact `json:"promotion,omitempty"`
	Workflow      *ContextWorkflowUpdate    `json:"workflow,omitempty"`
	PartialErrors []ContextPartialError     `json:"partial_errors,omitempty"`
	ReadableText  string                    `json:"readable_text,omitempty"`
}

type ContextHealthResult struct {
	Tool           string                `json:"tool"`
	Backend        ContextBackendStatus  `json:"backend"`
	Status         ContextToolStatus     `json:"status"`
	TaskID         string                `json:"task_id,omitempty"`
	Task           map[string]any        `json:"task,omitempty"`
	MemoryWorkflow any                   `json:"memory_workflow,omitempty"`
	PartialErrors  []ContextPartialError `json:"partial_errors,omitempty"`
	ReadableText   string                `json:"readable_text,omitempty"`
}

func registerContextTools(server *mcp.Server) {
	mcp.AddTool(server, readOnlyTool(
		"context_lookup",
		"Backend-neutral context lookup. Returns typed citations plus a readable summary from the active WUPHF context backend.",
	), handleContextLookup)
	mcp.AddTool(server, officeWriteTool(
		"context_capture",
		"Capture durable task context. On markdown backend this writes a notebook entry and records task memory workflow progress.",
	), handleContextCapture)
	mcp.AddTool(server, officeWriteTool(
		"context_promote",
		"Submit a captured notebook entry through the existing notebook-to-wiki promotion flow and record task memory workflow progress.",
	), handleContextPromote)
	mcp.AddTool(server, readOnlyTool(
		"context_health",
		"Report the active context backend and, when task_id is supplied, the task memory workflow state known by the broker.",
	), handleContextHealth)
}

func handleContextLookup(ctx context.Context, _ *mcp.CallToolRequest, args ContextLookupArgs) (*mcp.CallToolResult, any, error) {
	query := strings.TrimSpace(args.Query)
	if query == "" {
		return toolError(fmt.Errorf("query is required")), nil, nil
	}
	limit := normalizeContextLimit(args.Limit)
	includePrivate, includeShared := normalizeContextLookupScopes(args.IncludePrivate, args.IncludeShared)
	lookupCtx, cancel := context.WithTimeout(ctx, normalizeContextTimeout(args.TimeoutMS))
	defer cancel()

	backend := currentContextBackendStatus()
	result := ContextLookupResult{
		Tool:      "context_lookup",
		Query:     query,
		TaskID:    strings.TrimSpace(args.TaskID),
		TaskType:  strings.TrimSpace(args.TaskType),
		Limit:     limit,
		Backend:   backend,
		Status:    ContextToolStatus{OK: true, Code: "ok"},
		Citations: []ContextCitation{},
	}

	switch backend.ActiveKind {
	case config.MemoryBackendMarkdown:
		if includePrivate {
			citations, partials := lookupMarkdownNotebooks(lookupCtx, query, limit)
			result.Citations = append(result.Citations, citations...)
			result.PartialErrors = append(result.PartialErrors, partials...)
		}
		if includeShared {
			citations, partials := lookupMarkdownWiki(lookupCtx, query, limit)
			result.Citations = append(result.Citations, citations...)
			result.PartialErrors = append(result.PartialErrors, partials...)
		}
	case config.MemoryBackendNex, config.MemoryBackendGBrain:
		if includeShared {
			hits, err := contextQuerySharedMemory(lookupCtx, query, limit)
			if err != nil {
				result.PartialErrors = append(result.PartialErrors, partialError("shared", backend.ActiveKind, "backend_error", err, true))
			} else {
				for _, hit := range hits {
					result.Citations = append(result.Citations, citationFromScopedMemoryHit(hit))
				}
			}
		}
	case config.MemoryBackendNone:
		result.Status = inactiveContextStatus(backend)
		result.PartialErrors = append(result.PartialErrors, ContextPartialError{
			Source:  "backend",
			Backend: backend.SelectedKind,
			Code:    result.Status.Code,
			Message: result.Status.Message,
		})
	default:
		result.Status = ContextToolStatus{OK: false, Code: "unsupported_backend", Message: "context backend is not supported by context_lookup"}
		result.PartialErrors = append(result.PartialErrors, ContextPartialError{
			Source:  "backend",
			Backend: backend.ActiveKind,
			Code:    "unsupported_backend",
			Message: result.Status.Message,
		})
	}

	if result.Status.OK && len(result.Citations) == 0 && len(result.PartialErrors) == 0 {
		result.Status.Message = "No relevant context found."
	}
	if taskID := strings.TrimSpace(args.TaskID); taskID != "" {
		result.Workflow = recordContextWorkflowEvent(lookupCtx, contextWorkflowEvent{
			TaskID:    taskID,
			Actor:     strings.TrimSpace(resolveSlugOptional(args.MySlug)),
			Event:     "lookup",
			Query:     query,
			TaskType:  strings.TrimSpace(args.TaskType),
			Citations: result.Citations,
			Backend:   backend,
		})
		appendWorkflowPartial(&result.PartialErrors, result.Workflow, backend.ActiveKind)
	}
	result.ReadableText = formatContextLookupText(result)
	return textResult(result.ReadableText), result, nil
}

func handleContextCapture(ctx context.Context, _ *mcp.CallToolRequest, args ContextCaptureArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	backend := currentContextBackendStatus()
	result := ContextCaptureResult{Tool: "context_capture", TaskID: strings.TrimSpace(args.TaskID), Backend: backend, Status: ContextToolStatus{OK: true, Code: "ok"}}
	if skip := strings.TrimSpace(args.SkipReason); skip != "" {
		result.Notebook = &ContextNotebookArtifact{Skipped: true, SkipReason: skip}
		result.Workflow = recordContextWorkflowEvent(ctx, contextWorkflowEvent{TaskID: strings.TrimSpace(args.TaskID), Actor: slug, Event: "capture_skipped", SkipReason: skip, Backend: backend})
		appendWorkflowPartial(&result.PartialErrors, result.Workflow, backend.ActiveKind)
		result.ReadableText = formatContextCaptureText(result)
		return textResult(result.ReadableText), result, nil
	}
	content := strings.TrimSpace(args.Content)
	if content == "" {
		return toolError(fmt.Errorf("content is required unless skip_reason is supplied")), nil, nil
	}
	if backend.ActiveKind != config.MemoryBackendMarkdown {
		result.Status = unsupportedContextWriteStatus(backend, "context_capture writes notebook entries only on the markdown backend")
		result.PartialErrors = append(result.PartialErrors, ContextPartialError{Source: "notebook", Backend: backend.SelectedKind, Code: result.Status.Code, Message: result.Status.Message})
		result.ReadableText = formatContextCaptureText(result)
		return textResult(result.ReadableText), result, nil
	}

	path := strings.TrimSpace(args.NotebookPath)
	if path == "" {
		path = defaultContextNotebookPath(slug, args.Title, content)
	}
	if err := validateContextNotebookPath(slug, path); err != nil {
		return toolError(err), nil, nil
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

	var writeResult struct {
		Path         string `json:"path"`
		CommitSHA    string `json:"commit_sha"`
		BytesWritten int    `json:"bytes_written"`
	}
	if err := brokerPostJSON(ctx, "/notebook/write", map[string]any{
		"slug":           slug,
		"path":           path,
		"mode":           mode,
		"content":        renderContextNotebookContent(args.Title, content),
		"commit_message": firstNonEmptyString(strings.TrimSpace(args.Title), "Capture task context"),
	}, &writeResult); err != nil {
		result.Status = ContextToolStatus{OK: false, Code: "backend_error", Message: err.Error(), Retryable: true}
		result.PartialErrors = append(result.PartialErrors, partialError("notebook", backend.ActiveKind, "backend_error", err, true))
		result.ReadableText = formatContextCaptureText(result)
		return textResult(result.ReadableText), result, nil
	}
	result.Notebook = &ContextNotebookArtifact{Path: firstNonEmptyString(writeResult.Path, path), CommitSHA: strings.TrimSpace(writeResult.CommitSHA), BytesWritten: writeResult.BytesWritten}
	result.Workflow = recordContextWorkflowEvent(ctx, contextWorkflowEvent{
		TaskID: strings.TrimSpace(args.TaskID),
		Actor:  slug,
		Event:  "capture",
		Artifact: map[string]any{
			"type":          "notebook",
			"path":          result.Notebook.Path,
			"commit_sha":    result.Notebook.CommitSHA,
			"bytes_written": result.Notebook.BytesWritten,
		},
		Backend: backend,
	})
	appendWorkflowPartial(&result.PartialErrors, result.Workflow, backend.ActiveKind)
	result.ReadableText = formatContextCaptureText(result)
	return textResult(result.ReadableText), result, nil
}

func handleContextPromote(ctx context.Context, _ *mcp.CallToolRequest, args ContextPromoteArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	backend := currentContextBackendStatus()
	result := ContextPromoteResult{Tool: "context_promote", TaskID: strings.TrimSpace(args.TaskID), Backend: backend, Status: ContextToolStatus{OK: true, Code: "ok"}}
	if skip := strings.TrimSpace(args.SkipReason); skip != "" {
		result.Promotion = &ContextPromotionArtifact{Skipped: true, SkipReason: skip}
		result.Workflow = recordContextWorkflowEvent(ctx, contextWorkflowEvent{TaskID: strings.TrimSpace(args.TaskID), Actor: slug, Event: "promote_skipped", SkipReason: skip, Backend: backend})
		appendWorkflowPartial(&result.PartialErrors, result.Workflow, backend.ActiveKind)
		result.ReadableText = formatContextPromoteText(result)
		return textResult(result.ReadableText), result, nil
	}
	if backend.ActiveKind != config.MemoryBackendMarkdown {
		result.Status = unsupportedContextWriteStatus(backend, "context_promote uses notebook promotion and is only available on the markdown backend")
		result.PartialErrors = append(result.PartialErrors, ContextPartialError{Source: "promotion", Backend: backend.SelectedKind, Code: result.Status.Code, Message: result.Status.Message})
		result.ReadableText = formatContextPromoteText(result)
		return textResult(result.ReadableText), result, nil
	}

	sourcePath := strings.TrimSpace(args.SourcePath)
	targetPath := strings.TrimSpace(args.TargetWikiPath)
	rationale := strings.TrimSpace(args.Rationale)
	if err := validateContextPromotion(slug, sourcePath, targetPath, rationale); err != nil {
		return toolError(err), nil, nil
	}
	var promoteResult struct {
		PromotionID  string `json:"promotion_id"`
		ReviewerSlug string `json:"reviewer_slug"`
		State        string `json:"state"`
		HumanOnly    bool   `json:"human_only"`
	}
	if err := brokerPostJSON(ctx, "/notebook/promote", map[string]any{
		"my_slug":          slug,
		"source_path":      sourcePath,
		"target_wiki_path": targetPath,
		"rationale":        rationale,
		"reviewer_slug":    strings.TrimSpace(args.ReviewerSlug),
	}, &promoteResult); err != nil {
		result.Status = ContextToolStatus{OK: false, Code: "backend_error", Message: err.Error(), Retryable: true}
		result.PartialErrors = append(result.PartialErrors, partialError("promotion", backend.ActiveKind, "backend_error", err, true))
		result.ReadableText = formatContextPromoteText(result)
		return textResult(result.ReadableText), result, nil
	}
	result.Promotion = &ContextPromotionArtifact{
		PromotionID:    strings.TrimSpace(promoteResult.PromotionID),
		ReviewerSlug:   strings.TrimSpace(promoteResult.ReviewerSlug),
		State:          strings.TrimSpace(promoteResult.State),
		HumanOnly:      promoteResult.HumanOnly,
		SourcePath:     sourcePath,
		TargetWikiPath: targetPath,
	}
	result.Workflow = recordContextWorkflowEvent(ctx, contextWorkflowEvent{
		TaskID: strings.TrimSpace(args.TaskID),
		Actor:  slug,
		Event:  "promote",
		Artifact: map[string]any{
			"type":             "promotion",
			"promotion_id":     result.Promotion.PromotionID,
			"state":            result.Promotion.State,
			"source_path":      sourcePath,
			"target_wiki_path": targetPath,
		},
		Backend: backend,
	})
	appendWorkflowPartial(&result.PartialErrors, result.Workflow, backend.ActiveKind)
	result.ReadableText = formatContextPromoteText(result)
	return textResult(result.ReadableText), result, nil
}

func handleContextHealth(ctx context.Context, _ *mcp.CallToolRequest, args ContextHealthArgs) (*mcp.CallToolResult, any, error) {
	backend := currentContextBackendStatus()
	result := ContextHealthResult{Tool: "context_health", Backend: backend, Status: ContextToolStatus{OK: true, Code: "ok"}, TaskID: strings.TrimSpace(args.TaskID)}
	if result.TaskID != "" {
		task, workflow, err := fetchContextTaskWorkflow(ctx, result.TaskID)
		if err != nil {
			result.PartialErrors = append(result.PartialErrors, partialError("task_workflow", backend.ActiveKind, "backend_error", err, true))
		} else {
			result.Task = task
			result.MemoryWorkflow = workflow
		}
	}
	result.ReadableText = formatContextHealthText(result)
	return textResult(result.ReadableText), result, nil
}

func lookupMarkdownWiki(ctx context.Context, query string, limit int) ([]ContextCitation, []ContextPartialError) {
	var result struct {
		Hits []struct {
			Path    string `json:"path"`
			Line    int    `json:"line"`
			Snippet string `json:"snippet"`
		} `json:"hits"`
	}
	if err := brokerGetJSON(ctx, "/wiki/search?pattern="+url.QueryEscape(query), &result); err != nil {
		return nil, []ContextPartialError{partialError("wiki", config.MemoryBackendMarkdown, contextErrorCode(err), err, true)}
	}
	if limit > len(result.Hits) {
		limit = len(result.Hits)
	}
	citations := make([]ContextCitation, 0, limit)
	for _, hit := range result.Hits[:limit] {
		path := strings.TrimSpace(hit.Path)
		citations = append(citations, ContextCitation{
			Corpus:     "wiki",
			Backend:    config.MemoryBackendMarkdown,
			Scope:      "shared",
			Identifier: path,
			Path:       path,
			Line:       hit.Line,
			Title:      titleFromContextPath(path),
			Snippet:    truncate(strings.TrimSpace(hit.Snippet), contextSnippetMaxLength),
		})
	}
	return citations, nil
}

func lookupMarkdownNotebooks(ctx context.Context, query string, limit int) ([]ContextCitation, []ContextPartialError) {
	var catalog struct {
		Agents []struct {
			AgentSlug string `json:"agent_slug"`
		} `json:"agents"`
	}
	if err := brokerGetJSON(ctx, "/notebook/catalog", &catalog); err != nil {
		return nil, []ContextPartialError{partialError("notebook", config.MemoryBackendMarkdown, contextErrorCode(err), err, true)}
	}
	slugs := make([]string, 0, len(catalog.Agents))
	seen := map[string]bool{}
	for _, agent := range catalog.Agents {
		slug := strings.TrimSpace(agent.AgentSlug)
		if slug == "" || seen[slug] {
			continue
		}
		seen[slug] = true
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)

	citations := make([]ContextCitation, 0, limit)
	var partials []ContextPartialError
	for _, slug := range slugs {
		if len(citations) >= limit {
			break
		}
		values := url.Values{}
		values.Set("slug", slug)
		values.Set("q", query)
		var search struct {
			Hits []struct {
				Path    string `json:"path"`
				Line    int    `json:"line"`
				Snippet string `json:"snippet"`
			} `json:"hits"`
		}
		if err := brokerGetJSON(ctx, "/notebook/search?"+values.Encode(), &search); err != nil {
			partials = append(partials, partialError("notebook:"+slug, config.MemoryBackendMarkdown, contextErrorCode(err), err, true))
			if ctx.Err() != nil {
				break
			}
			continue
		}
		for _, hit := range search.Hits {
			if len(citations) >= limit {
				break
			}
			path := strings.TrimSpace(hit.Path)
			citations = append(citations, ContextCitation{
				Corpus:     "notebook",
				Backend:    config.MemoryBackendMarkdown,
				Scope:      "private",
				Identifier: path,
				Path:       path,
				Line:       hit.Line,
				Title:      titleFromContextPath(path),
				Snippet:    truncate(strings.TrimSpace(hit.Snippet), contextSnippetMaxLength),
				OwnerSlug:  slug,
			})
		}
	}
	return citations, partials
}

func citationFromScopedMemoryHit(hit team.ScopedMemoryHit) ContextCitation {
	identifier := strings.TrimSpace(hit.Identifier)
	return ContextCitation{
		Corpus:     "shared",
		Backend:    strings.TrimSpace(hit.Backend),
		Scope:      strings.TrimSpace(hit.Scope),
		Identifier: identifier,
		Title:      strings.TrimSpace(hit.Title),
		Snippet:    truncate(strings.TrimSpace(hit.Snippet), contextSnippetMaxLength),
		OwnerSlug:  strings.TrimSpace(hit.OwnerSlug),
		Slug:       firstNonEmptyString(strings.TrimSpace(hit.Slug), identifier),
		PageID:     hit.PageID,
		ChunkID:    hit.ChunkID,
		ChunkIndex: hit.ChunkIndex,
		Source:     strings.TrimSpace(hit.Source),
		Score:      hit.Score,
		Stale:      hit.Stale,
	}
}

type contextWorkflowEvent struct {
	TaskID     string
	Actor      string
	Event      string
	Query      string
	TaskType   string
	Citations  []ContextCitation
	Artifact   map[string]any
	Backend    ContextBackendStatus
	SkipReason string
}

func recordContextWorkflowEvent(ctx context.Context, event contextWorkflowEvent) *ContextWorkflowUpdate {
	taskID := strings.TrimSpace(event.TaskID)
	if taskID == "" {
		return nil
	}
	update := &ContextWorkflowUpdate{Attempted: true, TaskID: taskID, Event: strings.TrimSpace(event.Event)}
	var response map[string]any
	if err := brokerPostJSON(ctx, "/tasks/memory-workflow", map[string]any{
		"task_id":     taskID,
		"actor":       strings.TrimSpace(event.Actor),
		"event":       strings.TrimSpace(event.Event),
		"query":       strings.TrimSpace(event.Query),
		"task_type":   strings.TrimSpace(event.TaskType),
		"citations":   event.Citations,
		"artifact":    event.Artifact,
		"backend":     event.Backend,
		"skip_reason": strings.TrimSpace(event.SkipReason),
	}, &response); err != nil {
		update.Code = workflowUpdateErrorCode(err)
		update.Error = err.Error()
		return update
	}
	update.Updated = true
	update.Response = response
	return update
}

func fetchContextTaskWorkflow(ctx context.Context, taskID string) (map[string]any, any, error) {
	var response struct {
		Tasks []map[string]any `json:"tasks"`
	}
	if err := brokerGetJSON(ctx, "/tasks?all_channels=true&include_done=true", &response); err != nil {
		return nil, nil, err
	}
	for _, task := range response.Tasks {
		if fmt.Sprint(task["id"]) == taskID {
			return task, task["memory_workflow"], nil
		}
	}
	return nil, nil, fmt.Errorf("task %q not found", taskID)
}

func currentContextBackendStatus() ContextBackendStatus {
	status := contextResolveMemoryBackendStatus()
	return ContextBackendStatus{
		SelectedKind:  strings.TrimSpace(status.SelectedKind),
		SelectedLabel: strings.TrimSpace(status.SelectedLabel),
		ActiveKind:    strings.TrimSpace(status.ActiveKind),
		ActiveLabel:   strings.TrimSpace(status.ActiveLabel),
		Active:        strings.TrimSpace(status.ActiveKind) != "" && strings.TrimSpace(status.ActiveKind) != config.MemoryBackendNone,
		Detail:        strings.TrimSpace(status.Detail),
		NextStep:      strings.TrimSpace(status.NextStep),
	}
}

func normalizeContextLimit(limit int) int {
	if limit <= 0 {
		return contextDefaultLimit
	}
	if limit > contextMaxLimit {
		return contextMaxLimit
	}
	return limit
}

func normalizeContextTimeout(timeoutMS int) time.Duration {
	if timeoutMS <= 0 {
		return contextDefaultTimeout
	}
	if timeoutMS > int(contextMaxTimeout/time.Millisecond) {
		return contextMaxTimeout
	}
	return time.Duration(timeoutMS) * time.Millisecond
}

func normalizeContextLookupScopes(includePrivate, includeShared bool) (bool, bool) {
	if !includePrivate && !includeShared {
		return true, true
	}
	return includePrivate, includeShared
}

func inactiveContextStatus(backend ContextBackendStatus) ContextToolStatus {
	if backend.SelectedKind == config.MemoryBackendNone {
		return ContextToolStatus{OK: false, Code: "backend_disabled", Message: firstNonEmptyString(backend.Detail, "External context memory is disabled for this run.")}
	}
	return ContextToolStatus{OK: false, Code: "backend_inactive", Message: firstNonEmptyString(backend.Detail, "Selected context backend is not active.")}
}

func unsupportedContextWriteStatus(backend ContextBackendStatus, message string) ContextToolStatus {
	if backend.ActiveKind == config.MemoryBackendNone {
		status := inactiveContextStatus(backend)
		status.Message = firstNonEmptyString(message, status.Message)
		return status
	}
	return ContextToolStatus{OK: false, Code: "unsupported_backend", Message: message}
}

func partialError(source, backend, code string, err error, retryable bool) ContextPartialError {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return ContextPartialError{Source: source, Backend: backend, Code: code, Message: msg, Retryable: retryable}
}

func appendWorkflowPartial(partials *[]ContextPartialError, update *ContextWorkflowUpdate, backend string) {
	if update == nil || update.Error == "" {
		return
	}
	*partials = append(*partials, ContextPartialError{Source: "task_workflow", Backend: backend, Code: update.Code, Message: update.Error, Retryable: update.Code != "workflow_endpoint_unavailable"})
}

func contextErrorCode(err error) string {
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "context deadline exceeded") {
		return "timeout"
	}
	return "backend_error"
}

func workflowUpdateErrorCode(err error) string {
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "404") || strings.Contains(msg, "405") {
		return "workflow_endpoint_unavailable"
	}
	if strings.Contains(msg, "context deadline exceeded") {
		return "timeout"
	}
	return "workflow_update_failed"
}

func defaultContextNotebookPath(slug, title, content string) string {
	base := normalizeMemoryKey(firstNonEmptyString(title, content))
	if base == "" {
		base = "context-note"
	}
	base = trimContextFilename(base, 72)
	return fmt.Sprintf("agents/%s/notebook/%s-%s.md", slug, contextNow().UTC().Format("2006-01-02"), base)
}

func validateContextNotebookPath(slug, path string) error {
	expectedPrefix := "agents/" + slug + "/notebook/"
	if !strings.HasPrefix(path, expectedPrefix) {
		return fmt.Errorf("notebook_path_not_author_owned: path %q must start with %s", path, expectedPrefix)
	}
	if !strings.HasSuffix(strings.ToLower(path), ".md") {
		return fmt.Errorf("notebook_path must end in .md; got %q", path)
	}
	if strings.Contains(path, "..") || strings.Contains(path, "\\") {
		return fmt.Errorf("notebook_path must not contain path traversal; got %q", path)
	}
	return nil
}

func validateContextPromotion(slug, sourcePath, targetPath, rationale string) error {
	if sourcePath == "" {
		return fmt.Errorf("source_path is required")
	}
	expectedSourcePrefix := "agents/" + slug + "/notebook/"
	if !strings.HasPrefix(sourcePath, expectedSourcePrefix) {
		return fmt.Errorf("source_path %q must start with %s", sourcePath, expectedSourcePrefix)
	}
	if !strings.HasSuffix(strings.ToLower(sourcePath), ".md") {
		return fmt.Errorf("source_path must end in .md; got %q", sourcePath)
	}
	if targetPath == "" {
		return fmt.Errorf("target_wiki_path is required")
	}
	if !strings.HasPrefix(targetPath, "team/") {
		return fmt.Errorf("target_wiki_path %q must start with team/", targetPath)
	}
	if !strings.HasSuffix(strings.ToLower(targetPath), ".md") {
		return fmt.Errorf("target_wiki_path must end in .md; got %q", targetPath)
	}
	if rationale == "" {
		return fmt.Errorf("rationale is required")
	}
	return nil
}

func renderContextNotebookContent(title, content string) string {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "# ") || strings.HasPrefix(content, "---\n") {
		return content + "\n"
	}
	title = firstNonEmptyString(title, "Context capture")
	return "# " + title + "\n\n" + content + "\n"
}

func formatContextLookupText(result ContextLookupResult) string {
	lines := []string{fmt.Sprintf("Context lookup: %s backend (%s active)", result.Backend.SelectedKind, result.Backend.ActiveKind)}
	if len(result.Citations) == 0 {
		lines = append(lines, "No context citations found.")
	} else {
		lines = append(lines, fmt.Sprintf("Citations (%d):", len(result.Citations)))
		for _, c := range result.Citations {
			ref := firstNonEmptyString(c.Path, c.Identifier, c.Slug)
			if c.Line > 0 {
				ref = fmt.Sprintf("%s:%d", ref, c.Line)
			}
			lines = append(lines, fmt.Sprintf("- %s [%s/%s] %s", firstNonEmptyString(c.Title, ref), c.Backend, c.Corpus, truncate(c.Snippet, 180)))
		}
	}
	if len(result.PartialErrors) > 0 {
		lines = append(lines, "Partial errors:")
		for _, pe := range result.PartialErrors {
			lines = append(lines, fmt.Sprintf("- %s: %s", pe.Source, pe.Message))
		}
	}
	return strings.Join(lines, "\n")
}

func formatContextCaptureText(result ContextCaptureResult) string {
	if result.Notebook != nil && result.Notebook.Skipped {
		return "Context capture skipped: " + result.Notebook.SkipReason
	}
	if result.Status.OK && result.Notebook != nil {
		text := "Captured context in notebook " + result.Notebook.Path
		if result.Workflow != nil && result.Workflow.Updated {
			text += " and updated task memory workflow."
		}
		if len(result.PartialErrors) > 0 {
			text += " Partial errors: " + joinPartialErrorMessages(result.PartialErrors)
		}
		return text
	}
	return "Context capture unavailable: " + firstNonEmptyString(result.Status.Message, result.Status.Code)
}

func formatContextPromoteText(result ContextPromoteResult) string {
	if result.Promotion != nil && result.Promotion.Skipped {
		return "Context promotion skipped: " + result.Promotion.SkipReason
	}
	if result.Status.OK && result.Promotion != nil {
		text := fmt.Sprintf("Submitted notebook promotion %s (%s) for %s.", result.Promotion.PromotionID, result.Promotion.State, result.Promotion.TargetWikiPath)
		if result.Workflow != nil && result.Workflow.Updated {
			text += " Updated task memory workflow."
		}
		if len(result.PartialErrors) > 0 {
			text += " Partial errors: " + joinPartialErrorMessages(result.PartialErrors)
		}
		return text
	}
	return "Context promotion unavailable: " + firstNonEmptyString(result.Status.Message, result.Status.Code)
}

func formatContextHealthText(result ContextHealthResult) string {
	lines := []string{fmt.Sprintf("Context backend: selected=%s active=%s", result.Backend.SelectedKind, result.Backend.ActiveKind)}
	if result.Backend.Detail != "" {
		lines = append(lines, result.Backend.Detail)
	}
	if result.TaskID != "" {
		switch {
		case result.Task == nil:
			lines = append(lines, "Task workflow: unavailable")
		case result.MemoryWorkflow == nil:
			lines = append(lines, "Task workflow: not present")
		default:
			lines = append(lines, "Task workflow: present")
		}
	}
	if len(result.PartialErrors) > 0 {
		lines = append(lines, "Partial errors: "+joinPartialErrorMessages(result.PartialErrors))
	}
	return strings.Join(lines, "\n")
}

func joinPartialErrorMessages(partials []ContextPartialError) string {
	messages := make([]string, 0, len(partials))
	for _, pe := range partials {
		messages = append(messages, pe.Source+": "+pe.Message)
	}
	return strings.Join(messages, "; ")
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func titleFromContextPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	base := path
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		base = base[idx+1:]
	}
	base = strings.TrimSuffix(base, ".md")
	base = strings.ReplaceAll(base, "-", " ")
	base = strings.ReplaceAll(base, "_", " ")
	return strings.TrimSpace(base)
}

func trimContextFilename(value string, max int) string {
	value = strings.Trim(value, "-")
	if len(value) <= max {
		return value
	}
	value = value[:max]
	return strings.Trim(value[:max], "-")
}
