package team

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

// handleNotebookVisualArtifacts owns the collection route:
//
//	GET  /notebook/visual-artifacts?slug=&source_path=
//	POST /notebook/visual-artifacts
func (b *Broker) handleNotebookVisualArtifacts(w http.ResponseWriter, r *http.Request) {
	worker := b.requireWikiWorker(w, "visual artifact")
	if worker == nil {
		return
	}
	switch r.Method {
	case http.MethodGet:
		filter := RichArtifactFilter{
			CreatedBy:          strings.TrimSpace(r.URL.Query().Get("slug")),
			SourceMarkdownPath: strings.TrimSpace(r.URL.Query().Get("source_path")),
		}
		if filter.CreatedBy != "" {
			if err := validateNotebookSlug(filter.CreatedBy); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
		}
		if filter.SourceMarkdownPath != "" {
			if err := validateNotebookPath(filter.SourceMarkdownPath); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
		}
		artifacts, err := worker.ListRichArtifacts(filter)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"artifacts": artifacts})
	case http.MethodPost:
		var body RichArtifactCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
		artifact, html, err := newRichArtifact(body, time.Now())
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		sha, n, err := worker.CreateRichArtifact(r.Context(), artifact, html, body.CommitMessage)
		if err != nil {
			writeRichArtifactError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"artifact":      artifact,
			"commit_sha":    sha,
			"bytes_written": n,
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleNotebookVisualArtifactSubpath owns:
//
//	GET  /notebook/visual-artifacts/{id}
//	POST /notebook/visual-artifacts/{id}/promote
func (b *Broker) handleNotebookVisualArtifactSubpath(w http.ResponseWriter, r *http.Request) {
	worker := b.requireWikiWorker(w, "visual artifact")
	if worker == nil {
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/notebook/visual-artifacts/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "visual artifact not found"})
		return
	}
	id := parts[0]
	if err := validateRichArtifactID(id); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		artifact, html, err := worker.RichArtifact(id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"artifact": artifact, "html": html})
		return
	}
	if len(parts) == 2 && parts[1] == "promote" {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body RichArtifactPromoteRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
		actorSlug := strings.TrimSpace(body.ActorSlug)
		if actorSlug == "" {
			actorSlug = strings.TrimSpace(r.Header.Get(agentRateLimitHeader))
		}
		if actor, ok := requestActorFromContext(r.Context()); ok && actor.Kind == requestActorKindHuman {
			actorSlug = humanIdentityFromActor(actor).Slug
		}
		if actorSlug == "" {
			actorSlug = "human"
		}
		artifact, sha, n, err := worker.PromoteRichArtifact(
			r.Context(),
			actorSlug,
			id,
			strings.TrimSpace(body.TargetWikiPath),
			body.MarkdownSummary,
			strings.TrimSpace(body.Mode),
			body.CommitMessage,
		)
		if err != nil {
			writeRichArtifactError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"artifact":      artifact,
			"commit_sha":    sha,
			"bytes_written": n,
		})
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "visual artifact not found"})
}

// handleWikiVisualArtifact returns the promoted visual artifact associated with
// a wiki article, when one exists.
//
//	GET /wiki/visual?path=team/decisions/foo.md
func (b *Broker) handleWikiVisualArtifact(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.requireWikiWorker(w, "visual artifact")
	if worker == nil {
		return
	}
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if err := validateArticlePath(path); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	artifacts, err := worker.ListRichArtifacts(RichArtifactFilter{PromotedWikiPath: path})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if len(artifacts) == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "visual artifact not found"})
		return
	}
	artifact, html, err := worker.RichArtifact(artifacts[0].ID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"artifact": artifact, "html": html})
}

func writeRichArtifactError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrQueueSaturated):
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": err.Error()})
	case errors.Is(err, ErrWorkerStopped):
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
	case isRichArtifactCallerError(err):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
}

func isRichArtifactCallerError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	markers := []string{
		"visual artifact: invalid id",
		"visual artifact: title is required",
		"visual artifact: html",
		"visual artifact: content hash mismatch",
		"visual artifact: unsupported",
		"visual artifact: createdBy is required",
		"visual artifact: timestamps are required",
		"visual artifact: actor slug is required",
		"visual artifact: markdown_summary is required",
		"notebook:",
		"wiki: article",
		"wiki: unknown write mode",
	}
	for _, marker := range markers {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}
