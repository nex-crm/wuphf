package team

// broker_wiki_extract.go — HTTP surface for the extraction loop.
//
// Routes:
//   POST /wiki/extract/replay — drain the DLQ replay queue. Returns
//     { "processed": N, "retired": M }.
//
// The write-side hook (extract-on-artifact-commit) lives in WikiWorker.process
// and is wired up in ensureWikiWorker. This handler is for operator + cron
// triggers only.

import (
	"encoding/json"
	"net/http"
)

// handleWikiExtractReplay answers POST /wiki/extract/replay.
func (b *Broker) handleWikiExtractReplay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	b.mu.Lock()
	extractor := b.wikiExtractor
	b.mu.Unlock()
	if extractor == nil {
		http.Error(w, `{"error":"wiki extractor is not active"}`, http.StatusServiceUnavailable)
		return
	}
	processed, retired, err := extractor.ReplayDLQ(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int{
		"processed": processed,
		"retired":   retired,
	})
}
