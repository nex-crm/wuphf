package team

// broker_wiki_dlq.go — HTTP surface for DLQ inspection.
//
// Routes:
//   GET /wiki/dlq — returns a read-only snapshot of pending + permanent
//     failures. Auth-gated like every other /wiki/* route.
//
// Writes remain gated by the single-writer WikiWorker invariant: this
// endpoint neither enqueues nor resolves. Operators who need to replay
// use POST /wiki/extract/replay; resolve actions are still out-of-band
// (commit a resolved_at tombstone via the extractor path).

import (
	"net/http"
)

// handleWikiDLQ answers GET /wiki/dlq.
//
// Returns { "pending": [...], "permanent_failures": [...] } where each
// entry carries the full DLQEntry shape (artifact_sha, retry_count,
// next_retry_not_before, last_error, error_category, etc.). The endpoint
// never mutates state — it is safe to poll from dashboards.
func (b *Broker) handleWikiDLQ(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	b.mu.Lock()
	dlq := b.wikiDLQ
	b.mu.Unlock()

	if dlq == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "wiki DLQ is not active",
		})
		return
	}

	snapshot, err := dlq.Inspect(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": err.Error(),
		})
		return
	}

	// Normalise nil slices to empty arrays so clients can render without
	// null-checks.
	if snapshot.Pending == nil {
		snapshot.Pending = []DLQEntry{}
	}
	if snapshot.PermanentFailures == nil {
		snapshot.PermanentFailures = []DLQEntry{}
	}

	writeJSON(w, http.StatusOK, snapshot)
}
