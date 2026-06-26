package team

// wiki_handlers.go owns the broker HTTP endpoints for the team wiki. These
// handlers stay in the same package as WikiWorker but live outside
// wiki_worker.go so the worker file remains under the repo file-size budget.

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// handleWikiWrite is the broker HTTP endpoint the MCP subprocess posts to
// when an agent calls team_wiki_write. POST /wiki/write with
// {slug, path, content, mode, commit_message}. Responses: 200 {path,
// commit_sha, bytes_written}; 429 saturated; 500 generic; 503 no worker.
func (b *Broker) handleWikiWrite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.WikiWorker()
	if worker == nil {
		http.Error(w, `{"error":"wiki backend is not active"}`, http.StatusServiceUnavailable)
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
	isNew := wikiArticleIsNew(worker.Repo(), body.Path)
	sha, n, err := worker.Enqueue(r.Context(), body.Slug, body.Path, body.Content, body.Mode, body.CommitMessage)
	if err != nil {
		if errors.Is(err, ErrQueueSaturated) {
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if isNew {
		b.emitNewWikiArticleCard(body.Path, body.Content, r.Header.Get(agentRateLimitHeader))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path":          body.Path,
		"commit_sha":    sha,
		"bytes_written": n,
	})
}

// sanitizeReader validates a ?reader= query param before writing it to
// reads.jsonl. Returns the original value when it is safe, or "" to suppress
// tracking when it is not. Allowed: lowercase letters, digits, hyphens,
// underscores, max 64 chars. The special value ReaderHuman ("web") passes.
func sanitizeReader(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 64 {
		return ""
	}
	for _, ch := range raw {
		if !((ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_') {
			return ""
		}
	}
	return raw
}

// handleWikiRead returns raw article bytes.
//
//	GET /wiki/read?path=team/people/nazz.md
func (b *Broker) handleWikiRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.WikiWorker()
	if worker == nil {
		http.Error(w, `{"error":"wiki backend is not active"}`, http.StatusServiceUnavailable)
		return
	}
	relPath := strings.TrimSpace(r.URL.Query().Get("path"))
	if err := validateArticlePath(relPath); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	bytes, err := readArticle(worker.Repo(), relPath)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	// Track agent reads. The ?reader= param is set by the MCP layer using
	// WUPHF_AGENT_SLUG. Human reads go through /wiki/article, not here.
	// Return 400 if the caller passes the reserved human reader ("web"):
	// an agent slug named "web" would silently inflate human_read_count.
	if raw := r.URL.Query().Get("reader"); raw == ReaderHuman {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": `reader "web" is reserved for human browser access`})
		return
	} else if reader := sanitizeReader(raw); reader != "" {
		b.WikiReadLog().Append(relPath, reader)
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(bytes)
}

// handleWikiSearch returns literal-substring matches across team/. When a
// ?reader=<agent-slug> is supplied (set by the MCP layer from the trusted
// WUPHF_AGENT_SLUG env), the same call ALSO searches that agent's OWN
// notebook shelf, so a single retrieval spans wiki + private notes (B4).
// Permissioned boundary: only the reader's own notebooks are merged -
// cross-agent notebook access stays on the explicit notebook_read path.
//
//	GET /wiki/search?pattern=launch[&reader=eng]
func (b *Broker) handleWikiSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.WikiWorker()
	if worker == nil {
		http.Error(w, `{"error":"wiki backend is not active"}`, http.StatusServiceUnavailable)
		return
	}
	pattern := strings.TrimSpace(r.URL.Query().Get("pattern"))
	if pattern == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "pattern is required"})
		return
	}
	hits, err := searchArticles(worker.Repo(), pattern)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"hits": hits})
}

// handleWikiList returns the contents of index/all.md.
//
//	GET /wiki/list
func (b *Broker) handleWikiList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.WikiWorker()
	if worker == nil {
		http.Error(w, `{"error":"wiki backend is not active"}`, http.StatusServiceUnavailable)
		return
	}
	bytes, err := readIndexAll(worker.Repo())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(bytes)
}

// handleWikiCatalog returns the full catalog as structured JSON for the UI.
//
//	GET /wiki/catalog
//
// Response shape matches web/src/api/wiki.ts { articles: WikiCatalogEntry[] }.
// Distinct from /wiki/list (which returns raw markdown from index/all.md) -
// agents read the markdown index, the UI reads this JSON.
func (b *Broker) handleWikiCatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.WikiWorker()
	if worker == nil {
		http.Error(w, `{"error":"wiki backend is not active"}`, http.StatusServiceUnavailable)
		return
	}
	sortParam := strings.TrimSpace(r.URL.Query().Get("sort"))
	includeArchived := r.URL.Query().Get("include_archived") == "true"
	entries, err := worker.Repo().BuildCatalog(r.Context(), sortParam, b.WikiReadLog(), includeArchived)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if entries == nil {
		entries = []CatalogEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"articles": entries})
}

// handleWikiArchiveSweep runs one WikiArchiver.Sweep synchronously and
// returns the SweepResult as JSON. POST is required because this endpoint
// mutates state (archives articles, commits to git).
//
//	POST /wiki/archive/sweep
func (b *Broker) handleWikiArchiveSweep(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.WikiWorker()
	if worker == nil {
		http.Error(w, `{"error":"wiki backend is not active"}`, http.StatusServiceUnavailable)
		return
	}
	b.archiveSweepMu.Lock()
	defer b.archiveSweepMu.Unlock()
	// Cap the manual sweep with the same 10-minute timeout as the scheduled
	// path so a hung git process cannot hold archiveSweepMu indefinitely
	// while the caller holds the connection open.
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	result, err := worker.EnqueueArchiveSweep(ctx, b.WikiReadLog(), 0)
	if err != nil && isHardArchiveSweepError(err) {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// startArchiveSweepLoop runs WikiArchiver.Sweep on the wiki-archive-sweep
// cron schedule (default: daily, MinFloor: 60 min).
func (b *Broker) startArchiveSweepLoop(ctx context.Context) {
	const defaultInterval = 1440 * time.Minute
	go func() {
		for {
			enabled, interval := b.SchedulerJobControl("wiki-archive-sweep", defaultInterval)
			now := time.Now().UTC()
			b.updateSchedulerHeartbeat("wiki-archive-sweep", "Wiki archive sweep",
				int(interval/time.Minute), now.Add(interval),
				disabledOrSleeping(enabled), "")
			select {
			case <-ctx.Done():
				return
			case <-time.After(interval):
			}
			if !enabled {
				continue
			}
			runStatus := b.runArchiveSweepTick()
			b.updateSchedulerHeartbeat("wiki-archive-sweep", "Wiki archive sweep",
				int(interval/time.Minute), time.Now().UTC().Add(interval),
				"sleeping", runStatus)
		}
	}()
}

func (b *Broker) runArchiveSweepTick() string {
	worker := b.WikiWorker()
	if worker == nil {
		return "inactive"
	}
	b.archiveSweepMu.Lock()
	defer b.archiveSweepMu.Unlock()
	// 10 minutes is generous for 500 eligible articles at ~1s/commit.
	// Without a timeout a hung git process holds archiveSweepMu forever,
	// blocking the POST /wiki/archive/sweep handler with no escape.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	result, err := worker.EnqueueArchiveSweep(ctx, b.WikiReadLog(), 0)
	if err != nil && isHardArchiveSweepError(err) {
		log.Printf("wiki archive: sweep error: %v", err)
		return "error"
	}
	if result.Archived > 0 || result.Errors > 0 {
		log.Printf("wiki archive: sweep complete - archived=%d skipped=%d errors=%d",
			result.Archived, result.Skipped, result.Errors)
	}
	if result.Errors > 0 {
		return "error"
	}
	return "ok"
}

// handleWikiAudit returns the cross-article commit log for audit / compliance.
// Unlike /wiki/history/<path> which scopes to one article, this feed covers
// the whole wiki and includes bootstrap + recovery + system commits so the
// lineage is complete.
//
//	GET /wiki/audit
//	GET /wiki/audit?limit=50
//	GET /wiki/audit?since=2026-04-01T00:00:00Z
//
// Response:
//
//	{
//	  "entries": [
//	    {
//	      "sha": "...", "author_slug": "...", "timestamp": "...",
//	      "message": "...", "paths": ["team/..."]
//	    },
//	    ...
//	  ],
//	  "total": N
//	}
func (b *Broker) handleWikiAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.WikiWorker()
	if worker == nil {
		http.Error(w, `{"error":"wiki backend is not active"}`, http.StatusServiceUnavailable)
		return
	}
	// Parse limit (optional, 0 = all). Default cap keeps a runaway caller
	// from dragging in 100k commits; explicit `limit=0` opts out of the cap.
	const defaultLimit = 500
	limit := defaultLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "limit must be a non-negative integer"})
			return
		}
		limit = v
	}
	var since time.Time
	if raw := strings.TrimSpace(r.URL.Query().Get("since")); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "since must be RFC3339 (e.g. 2026-04-01T00:00:00Z)"})
			return
		}
		since = t
	}
	entries, err := worker.Repo().AuditLog(r.Context(), since, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// Re-shape to snake_case for the JSON API. Same convention as
	// /wiki/catalog and /wiki/article. `paths` never serialised as null:
	// absent paths (rare, but possible for a signed-only commit) get an
	// empty array so consumers don't have to null-guard.
	type wireEntry struct {
		SHA        string   `json:"sha"`
		AuthorSlug string   `json:"author_slug"`
		Timestamp  string   `json:"timestamp"`
		Message    string   `json:"message"`
		Paths      []string `json:"paths"`
	}
	wire := make([]wireEntry, 0, len(entries))
	for _, e := range entries {
		paths := e.Paths
		if paths == nil {
			paths = []string{}
		}
		wire = append(wire, wireEntry{
			SHA:        e.SHA,
			AuthorSlug: e.Author,
			Timestamp:  e.Timestamp.UTC().Format(time.RFC3339),
			Message:    e.Message,
			Paths:      paths,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"entries": wire,
		"total":   len(wire),
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
