package team

// broker_sources.go owns the broker HTTP endpoints for the immutable source
// layer (the raw material the Karpathy-style wiki is compiled FROM; see
// wiki_source.go). Writes ride the single-writer WikiWorker via EnqueueSource;
// reads scan sources/ via the wiki_source_store.go helpers.
//
//	GET  /sources/list             -> {sources: [<metadata>...]}  (Content omitted)
//	GET  /sources/read?kind=&id=   -> full SourceRecord JSON (404 if missing)
//	POST /sources/ingest           -> {kind,title,origin,content} => {id,path,sha}

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"strings"
	"time"
)

// explicitIngestKinds is the set of source kinds a client may POST to
// /sources/ingest. The auto-capture kinds (task/decision/chat) are produced by
// internal office hooks in a later slice — accepting them over HTTP would let a
// caller forge captured office activity, so they are rejected here.
var explicitIngestKinds = map[SourceKind]struct{}{
	SourceKindDoc:  {},
	SourceKindURL:  {},
	SourceKindNote: {},
}

// sourceMetadata is the list-payload shape: every field of a SourceRecord
// except the (potentially large) Content body, which /sources/read serves.
type sourceMetadata struct {
	ID          string     `json:"id"`
	Kind        SourceKind `json:"kind"`
	Title       string     `json:"title"`
	Origin      string     `json:"origin,omitempty"`
	CapturedAt  string     `json:"captured_at"`
	ContentHash string     `json:"content_hash"`
}

func sourceMetadataOf(rec SourceRecord) sourceMetadata {
	return sourceMetadata{
		ID:          rec.ID,
		Kind:        rec.Kind,
		Title:       rec.Title,
		Origin:      rec.Origin,
		CapturedAt:  rec.CapturedAt.UTC().Format(time.RFC3339),
		ContentHash: rec.ContentHash,
	}
}

// handleSourcesList returns metadata for every captured source, newest first.
//
//	GET /sources/list
func (b *Broker) handleSourcesList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.requireWikiWorker(w, "sources")
	if worker == nil {
		return
	}
	records, err := ListSources(worker.Repo())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	out := make([]sourceMetadata, 0, len(records))
	for _, rec := range records {
		out = append(out, sourceMetadataOf(rec))
	}
	writeJSON(w, http.StatusOK, map[string]any{"sources": out})
}

// handleSourcesRead returns the full record (including Content) for one source.
//
//	GET /sources/read?kind=note&id=note-foo-1a2b3c4d
func (b *Broker) handleSourcesRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.requireWikiWorker(w, "sources")
	if worker == nil {
		return
	}
	kind := SourceKind(strings.TrimSpace(r.URL.Query().Get("kind")))
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if !kind.IsValid() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "kind is required and must be a valid source kind"})
		return
	}
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
		return
	}
	rec, err := ReadSource(worker.Repo(), kind, id)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "source not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":           rec.ID,
		"kind":         rec.Kind,
		"title":        rec.Title,
		"origin":       rec.Origin,
		"captured_at":  rec.CapturedAt.UTC().Format(time.RFC3339),
		"content_hash": rec.ContentHash,
		"content":      rec.Content,
	})
}

// handleSourcesIngest captures one explicit-ingest source (doc | url | note).
//
//	POST /sources/ingest  {kind, title, origin, content} -> {id, path, sha}
func (b *Broker) handleSourcesIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.requireWikiWorker(w, "sources")
	if worker == nil {
		return
	}
	var body struct {
		Kind    string `json:"kind"`
		Title   string `json:"title"`
		Origin  string `json:"origin"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	kind := SourceKind(strings.TrimSpace(body.Kind))
	if _, ok := explicitIngestKinds[kind]; !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "kind must be one of doc, url, note; task/decision/chat are captured by office hooks, not ingest",
		})
		return
	}

	id := DeriveSourceID(kind, body.Origin, body.Title, body.Content)
	rec, err := NewSourceRecord(id, kind, body.Title, body.Origin, body.Content, time.Now().UTC())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	sha, _, err := worker.EnqueueSource(r.Context(), rec)
	if err != nil {
		if errors.Is(err, ErrQueueSaturated) {
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":   rec.ID,
		"path": rec.RelPath(),
		"sha":  sha,
	})
}
