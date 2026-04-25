package team

// broker_notebook.go wires the notebook write surface onto the broker:
//   - /notebook/{write,read,list,search} HTTP handlers (auth-gated)
//   - SSE publisher + subscribe seam for "notebook:write" events
//
// The handlers route all writes through WikiWorker.NotebookWrite so the same
// single-writer guarantee that protects the wiki also protects notebooks.

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

// SubscribeNotebookEvents returns a channel of notebook commit notifications
// plus an unsubscribe func. The /events SSE loop uses this to emit
// "notebook:write" events distinct from "wiki:write".
func (b *Broker) SubscribeNotebookEvents(buffer int) (<-chan notebookWriteEvent, func()) {
	if buffer <= 0 {
		buffer = 64
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.notebookSubscribers == nil {
		b.notebookSubscribers = make(map[int]chan notebookWriteEvent)
	}
	id := b.nextSubscriberID
	b.nextSubscriberID++
	ch := make(chan notebookWriteEvent, buffer)
	b.notebookSubscribers[id] = ch
	return ch, func() {
		b.mu.Lock()
		if existing, ok := b.notebookSubscribers[id]; ok {
			delete(b.notebookSubscribers, id)
			close(existing)
		}
		b.mu.Unlock()
	}
}

// PublishNotebookEvent fans out a commit notification to all SSE subscribers.
// Implements the notebookEventPublisher interface consumed by WikiWorker.
func (b *Broker) PublishNotebookEvent(evt notebookWriteEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.notebookSubscribers {
		select {
		case ch <- evt:
		default:
		}
	}
}

// handleNotebookWrite is the broker HTTP endpoint the MCP subprocess posts to
// when an agent calls notebook_write.
//
//	POST /notebook/write
//	{ "slug":..., "path":..., "content":..., "mode":..., "commit_message":... }
//
// Response: 200 { "path":..., "commit_sha":..., "bytes_written":... }
//
//	400 { "error":"..." }                    invalid JSON or validation failure
//	403 { "error":"notebook_path_not_author_owned..." } slug mismatch
//	429 { "error":"wiki queue saturated, retry on next turn" }
//	500 { "error":"..." }
//	503 { "error":"..." } when worker is not running
func (b *Broker) handleNotebookWrite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.WikiWorker()
	if worker == nil {
		http.Error(w, `{"error":"notebook backend is not active"}`, http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Slug          string `json:"slug"`
		Path          string `json:"path"`
		Content       string `json:"content"`
		Mode          string `json:"mode"`
		CommitMessage string `json:"commit_message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	sha, n, err := worker.NotebookWrite(r.Context(), body.Slug, body.Path, body.Content, body.Mode, body.CommitMessage)
	if err != nil {
		switch {
		case errors.Is(err, ErrQueueSaturated):
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": err.Error()})
		case errors.Is(err, ErrNotebookPathNotAuthorOwned):
			writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		case errors.Is(err, ErrWorkerStopped):
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		default:
			// Validation errors surface as 400 rather than 500 so callers can
			// distinguish caller-fault from server-fault. Git errors stay 500.
			if isNotebookValidationError(err) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path":          body.Path,
		"commit_sha":    sha,
		"bytes_written": n,
	})
}

// handleNotebookRead returns raw entry bytes for any agent's notebook. Cross-
// agent reads are intentional — the write side is author-owned, reads are not.
//
//	GET /notebook/read?slug={slug}&path={path}
//
// `slug` is optional (the path already carries the owner slug); when present
// it MUST match the slug embedded in `path`, otherwise the request is
// rejected so a misdirected client can't silently read the wrong entry.
func (b *Broker) handleNotebookRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.WikiWorker()
	if worker == nil {
		http.Error(w, `{"error":"notebook backend is not active"}`, http.StatusServiceUnavailable)
		return
	}
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path is required"})
		return
	}
	if slugHint := strings.TrimSpace(r.URL.Query().Get("slug")); slugHint != "" {
		if err := validateNotebookSlug(slugHint); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		expectedPrefix := "agents/" + slugHint + "/notebook/"
		if !strings.HasPrefix(path, expectedPrefix) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "slug does not match path owner"})
			return
		}
	}
	bytes, err := worker.NotebookRead(path)
	if err != nil {
		if isNotebookValidationError(err) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(bytes)
}

// handleNotebookList returns a reverse-chron JSON list of entries for one
// agent's notebook.
//
//	GET /notebook/list?slug={slug}
//
// Empty-slug lists are rejected — callers must name the agent whose
// notebook they want. The MCP layer defaults this to the caller's own slug
// when target_slug is omitted.
func (b *Broker) handleNotebookList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.WikiWorker()
	if worker == nil {
		http.Error(w, `{"error":"notebook backend is not active"}`, http.StatusServiceUnavailable)
		return
	}
	slug := strings.TrimSpace(r.URL.Query().Get("slug"))
	if slug == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "slug is required"})
		return
	}
	entries, err := worker.NotebookList(slug)
	if err != nil {
		if isNotebookValidationError(err) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

// handleNotebookCatalog returns a team-wide rollup for the bookshelf view:
// every agent that has a notebook dir, their most-recent entries, and the
// count of pending promotions so the sidebar badge reflects reality.
//
//	GET /notebook/catalog
//
// Response envelope matches NotebookCatalogSummary in web/src/api/notebook.ts:
//
//	{
//	  "agents":            [ NotebookAgentSummary, ... ],
//	  "total_agents":      <int>,
//	  "total_entries":     <int>,
//	  "pending_promotion": <int>
//	}
//
// Per-agent entry status is derived by cross-referencing the ReviewLog: any
// active review whose SourcePath matches the entry flips the status from
// 'draft' to the review's state; approved promotions surface as 'promoted'.
func (b *Broker) handleNotebookCatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.WikiWorker()
	if worker == nil {
		http.Error(w, `{"error":"notebook backend is not active"}`, http.StatusServiceUnavailable)
		return
	}

	slugs, err := worker.AgentsWithNotebooks()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Build a slug → officeMember lookup so we can fill name/role. Agents
	// whose notebook dir exists but who are no longer on the roster still
	// appear (marked as "former" via the role fallback) — their entries
	// remain readable even after offboarding.
	rosterBySlug := map[string]officeMember{}
	for _, m := range b.OfficeMembers() {
		rosterBySlug[m.Slug] = m
	}

	// Cross-reference active reviews so entry status reflects what's happening
	// right now. One List call, scan-once, reused across all agents.
	reviewsBySource := map[string]string{}
	if rl := b.ReviewLog(); rl != nil {
		for _, p := range rl.List("all") {
			// Only the freshest state wins for any given source path — the
			// List() method already returns the current state, so overwriting
			// on duplicate keys is intentional and cheap.
			reviewsBySource[p.SourcePath] = string(p.State)
		}
	}

	type entrySummary struct {
		EntrySlug    string `json:"entry_slug"`
		Title        string `json:"title"`
		LastEditedTS string `json:"last_edited_ts"`
		Status       string `json:"status"`
	}
	type agentSummary struct {
		AgentSlug     string         `json:"agent_slug"`
		Name          string         `json:"name"`
		Role          string         `json:"role"`
		Entries       []entrySummary `json:"entries"`
		Total         int            `json:"total"`
		PromotedCount int            `json:"promoted_count"`
		LastUpdatedTS string         `json:"last_updated_ts"`
	}

	const recentLimit = 5
	agents := make([]agentSummary, 0, len(slugs))
	totalEntries := 0
	totalPending := 0
	for _, slug := range slugs {
		entries, err := worker.NotebookList(slug)
		if err != nil {
			// Skip agents whose dir is malformed rather than fail the whole
			// catalog — partial data beats a 500 for the bookshelf.
			continue
		}
		if len(entries) == 0 {
			continue
		}

		member, known := rosterBySlug[slug]
		name := slug
		role := ""
		if known {
			if strings.TrimSpace(member.Name) != "" {
				name = member.Name
			}
			role = member.Role
		} else {
			role = "former teammate"
		}

		mapped := make([]entrySummary, 0, len(entries))
		promoted := 0
		var lastEdited string
		for _, e := range entries {
			entrySlug := strings.TrimSuffix(filepath.Base(e.Path), ".md")
			status := "draft"
			if state, ok := reviewsBySource[e.Path]; ok {
				switch state {
				case "pending", "in-review":
					status = "in-review"
				case "changes-requested":
					status = "changes-requested"
				case "approved", "archived":
					status = "promoted"
				case "rejected", "expired":
					status = "discarded"
				}
			}
			if status == "promoted" {
				promoted++
			}
			ts := e.Modified.UTC().Format(time.RFC3339)
			if lastEdited == "" || ts > lastEdited {
				lastEdited = ts
			}
			mapped = append(mapped, entrySummary{
				EntrySlug:    entrySlug,
				Title:        e.Title,
				LastEditedTS: ts,
				Status:       status,
			})
		}
		// Newest-first already applied by NotebookList; trim to recentLimit.
		recent := mapped
		if len(recent) > recentLimit {
			recent = recent[:recentLimit]
		}
		totalEntries += len(mapped)
		agents = append(agents, agentSummary{
			AgentSlug:     slug,
			Name:          name,
			Role:          role,
			Entries:       recent,
			Total:         len(mapped),
			PromotedCount: promoted,
			LastUpdatedTS: lastEdited,
		})
	}

	// pending_promotion is global, not per-agent — counts open promotions
	// regardless of source so the sidebar badge reflects queue depth.
	if rl := b.ReviewLog(); rl != nil {
		for _, p := range rl.List("all") {
			switch string(p.State) {
			case "pending", "in-review", "changes-requested":
				totalPending++
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"agents":            agents,
		"total_agents":      len(agents),
		"total_entries":     totalEntries,
		"pending_promotion": totalPending,
	})
}

// handleNotebookSearch runs a literal substring search scoped to one agent.
//
//	GET /notebook/search?slug={slug}&q={pattern}
func (b *Broker) handleNotebookSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.WikiWorker()
	if worker == nil {
		http.Error(w, `{"error":"notebook backend is not active"}`, http.StatusServiceUnavailable)
		return
	}
	slug := strings.TrimSpace(r.URL.Query().Get("slug"))
	if slug == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "slug is required"})
		return
	}
	pattern := strings.TrimSpace(r.URL.Query().Get("q"))
	if pattern == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "q is required"})
		return
	}
	hits, err := worker.NotebookSearch(slug, pattern)
	if err != nil {
		if isNotebookValidationError(err) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"hits": hits})
}

// isNotebookValidationError returns true for errors produced by the notebook
// path/slug validators. These map to HTTP 400 rather than 500 because they
// indicate caller fault, not server fault.
func isNotebookValidationError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// Validator functions prefix all caller-fault messages with "notebook:".
	// Git/IO errors are wrapped with their own prefixes (e.g. "notebook: git
	// commit:"). Distinguish by content — the specific strings below are
	// stable because they live in the validator and commit functions we own.
	validatorMarkers := []string{
		"path is required",
		"path must be relative",
		"path must not contain",
		"path must be within agents/",
		"path must match agents/",
		"path must end with .md",
		"slug is required",
		"invalid slug",
		"contains invalid characters",
		"my_slug is required",
		"entries must live directly under",
		"already exists at",
		"does not exist at",
		"unknown write mode",
		"content is required",
		"search pattern is required",
	}
	for _, m := range validatorMarkers {
		if strings.Contains(msg, m) {
			return true
		}
	}
	return false
}
