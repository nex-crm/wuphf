package team

// wiki_history.go — Slice 5 of the cabinet wiki port: per-article version
// history, per-commit diff, and append-only restore.
//
// Surface
// =======
//
//	GET  /wiki/history/<path>   → {commits: [{sha, author_slug, msg, date}, ...]}
//	GET  /wiki/diff?path=&sha=  → {diff, sha, path}
//	POST /wiki/restore          {path, sha} → {path, commit_sha}
//
// The history shape (sha / author_slug / msg / date) matches the
// WikiHistoryCommit interface the web client already expects in
// web/src/api/wiki.ts (fetchHistory → GET /wiki/history/<path>).
//
// Design
// ======
//
//   - history reuses Repo.Log (already validated, already newest-first). A path
//     that does not exist yields an empty commit list with a 200 — git log on a
//     never-committed path is empty, not an error — so the editor renders an
//     empty timeline rather than an error banner.
//   - diff and restore validate the path (resolveTeamRelPath / validateArticlePath)
//     AND the sha (validateCommitSHA) before any git call. No caller-supplied
//     ref ever reaches git unscreened.
//   - restore is append-only: Repo.RestoreToCommit writes the historical bytes
//     back and records a NEW commit authored by the requesting human. History is
//     never rewritten; the prior commits stay reachable.
//   - identity is resolved server-side via resolvePageIdentity, exactly like the
//     page-ops and /wiki/write-human routes — clients cannot forge attribution.

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
)

// wikiHistoryCommit is the JSON wire shape for one entry in the per-article
// history response. The field names MUST stay byte-identical to the
// WikiHistoryCommit TypeScript interface (sha / author_slug / msg / date) the
// web client decodes in fetchHistory.
type wikiHistoryCommit struct {
	SHA        string `json:"sha"`
	AuthorSlug string `json:"author_slug"`
	Msg        string `json:"msg"`
	Date       string `json:"date"`
}

// handleWikiHistory serves GET /wiki/history/<path>. The path is the suffix
// after the /wiki/history/ prefix and is a repo-root-relative article path
// (e.g. team/people/nazz.md). Response: {"commits": [...]} newest-first.
//
//	200 {"commits": [...]}        — including [] for a never-committed path
//	400 {"error":"..."}           — empty/invalid path
//	405                           — non-GET
func (b *Broker) handleWikiHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.requireWikiWorker(w, "wiki history")
	if worker == nil {
		return
	}

	relPath := strings.TrimPrefix(r.URL.Path, "/wiki/history/")
	relPath = strings.TrimSpace(relPath)
	if relPath == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path is required"})
		return
	}
	if err := validateArticlePath(relPath); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	refs, err := worker.Repo().Log(r.Context(), relPath)
	if err != nil {
		// Log can only fail on a git invocation error here (path was already
		// validated). Never forward raw git stderr; log it, surface a fixed
		// string. Mirrors the wiki_fs_handlers.go pattern.
		log.Printf("wiki history: Log %s failed: %v", relPath, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "history lookup failed"})
		return
	}

	commits := make([]wikiHistoryCommit, 0, len(refs))
	for _, ref := range refs {
		commits = append(commits, wikiHistoryCommit{
			SHA:        ref.SHA,
			AuthorSlug: ref.Author,
			Msg:        ref.Message,
			Date:       ref.Timestamp.UTC().Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"commits": commits})
}

// handleWikiDiff serves GET /wiki/diff?path=<relpath>&sha=<hex>. It returns the
// unified diff that the named commit introduced for that single article.
//
//	200 {"diff":..., "sha":..., "path":...}
//	400 {"error":"..."}   — bad path / bad sha
//	404 {"error":"..."}   — sha does not resolve to a commit
//	405                   — non-GET
func (b *Broker) handleWikiDiff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.requireWikiWorker(w, "wiki diff")
	if worker == nil {
		return
	}

	relPath := strings.TrimSpace(r.URL.Query().Get("path"))
	sha := strings.TrimSpace(r.URL.Query().Get("sha"))
	if relPath == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path is required"})
		return
	}

	diff, err := worker.Repo().Diff(r.Context(), relPath, sha)
	if err != nil {
		writeWikiHistoryError(w, err)
		return
	}
	// Echo the normalized inputs, not the raw caller values: the cleaned
	// slash path and the lower-cased SHA git actually resolved. Diff already
	// validated both, so re-normalizing here cannot fail — it just keeps the
	// 200 body free of caller-controlled formatting (mixed-case sha, trailing
	// whitespace, backslashes, redundant ./ segments).
	writeJSON(w, http.StatusOK, map[string]any{
		"diff": diff,
		"sha":  strings.ToLower(strings.TrimSpace(sha)),
		"path": cleanPagePath(relPath),
	})
}

// handleWikiRestore serves POST /wiki/restore. Body: {"path":"team/..md","sha":"<hex>"}.
// It restores the article body to the content at sha by recording a NEW commit
// (append-only — history is never rewritten). The committing identity is
// resolved server-side; clients cannot forge attribution.
//
//	200 {"path":..., "commit_sha":...}
//	400 {"error":"..."}   — invalid json / oversized body / bad path / bad sha
//	404 {"error":"..."}   — sha or path absent at that revision
//	409 {"error":"..."}   — nothing to restore (already current)
//	405                   — non-POST
//	503 {"error":"..."}   — wiki backend not active (shared requireWikiWorker path)
func (b *Broker) handleWikiRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.requireWikiWorker(w, "wiki restore")
	if worker == nil {
		return
	}
	// Bound the request body before decoding so a hostile or buggy client
	// cannot stream an unbounded payload into the broker. The body is only a
	// {path, sha} pair; 4 KiB is generous headroom. An oversized body trips the
	// MaxBytesReader limit and surfaces through Decode as a 400.
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var body struct {
		Path string `json:"path"`
		SHA  string `json:"sha"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}

	identity := b.resolvePageIdentity(r)
	commitSHA, err := worker.Repo().RestoreToCommit(r.Context(), body.Path, body.SHA, identity)
	if err != nil {
		writeWikiHistoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path":       cleanPagePath(body.Path),
		"commit_sha": commitSHA,
	})
}

// writeWikiHistoryError maps a diff/restore error to the right status + JSON
// body. Sentinel errors are mapped explicitly; anything else is logged and
// returned as a fixed string so raw git stderr / filesystem layout never leaks.
func writeWikiHistoryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrWikiCommitNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, ErrWikiRestoreNoop):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	case errors.Is(err, errWikiCallerInput), errors.Is(err, errWikiFSBadPath), errors.Is(err, errWikiPageMissing):
		// validateArticlePath / validateCommitSHA wrap errWikiCallerInput and
		// resolveTeamRelPath wraps errWikiFSBadPath — both are 400-class caller
		// errors routed via errors.Is (no fragile message-prefix matching). The
		// wrapped messages carry no git stderr or filesystem layout, so echoing
		// them is safe.
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	default:
		log.Printf("wiki history: internal error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "wiki operation failed"})
	}
}
