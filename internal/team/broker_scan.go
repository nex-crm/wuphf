package team

// broker_scan.go hosts the /scan HTTP endpoints. Split from broker.go to
// keep that file from growing further.
//
// Endpoints
// =========
//
//	POST /scan/start
//	    body: { "root": "...", "confirm": true|false }
//	    200:  { "id": "...", "result": ScanResult }   — scan executed
//	    202:  { "preview": PreviewResult }            — confirmation needed
//	    400:  { "error": "..." }                      — bad input
//	    413:  { "error": "..." }                      — size cap exceeded
//	    503:  { "error": "..." }                      — wiki backend unavailable
//
//	GET /scan/status?id={id}
//	    Returns the last ScanResult keyed by id.
//
// v1.1 is synchronous: POST /scan/start blocks until the walker, redactor,
// and commit all complete. Callers that need async should wrap in a goroutine.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
)

// scanStatusTracker tracks the most recent ScanResult per root-hash. The
// broker holds one of these and evicts old entries on a simple cap.
type scanStatusTracker struct {
	mu      sync.Mutex
	results map[string]*ScanResult
}

func newScanStatusTracker() *scanStatusTracker {
	return &scanStatusTracker{results: make(map[string]*ScanResult)}
}

func (t *scanStatusTracker) set(id string, res *ScanResult) {
	t.mu.Lock()
	defer t.mu.Unlock()
	// Simple cap — drop one arbitrary entry once we hit 64. v1.1 workloads
	// never come close; LRU accuracy is not worth the extra state.
	if len(t.results) >= 64 {
		for k := range t.results {
			delete(t.results, k)
			break
		}
	}
	t.results[id] = res
}

func (t *scanStatusTracker) get(id string) (*ScanResult, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	res, ok := t.results[id]
	return res, ok
}

// scanIDFromRoot produces a deterministic id from a scan root. Hashed so
// the id is path-safe and fixed-length in the URL.
func scanIDFromRoot(root string) string {
	sum := sha256.Sum256([]byte(root))
	return hex.EncodeToString(sum[:8])
}

// handleScanStart is the POST endpoint that kicks off a scan.
func (b *Broker) handleScanStart(w http.ResponseWriter, r *http.Request) {
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
		Root    string `json:"root"`
		Confirm bool   `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	root := strings.TrimSpace(body.Root)
	if root == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "root is required"})
		return
	}

	detector, err := NewMtimeChangeDetector()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	commit := func(ctx context.Context, author, message string) (string, error) {
		_ = author // scanner slug is constant; reserved for future override
		return worker.Repo().CommitScanStaged(ctx, message)
	}

	result, preview, err := Scan(r.Context(), ScanOptions{
		Root:    root,
		Confirm: body.Confirm,
	}, detector, worker.Repo().Root(), commit)

	switch {
	case errors.Is(err, ErrScanConfirmationRequired):
		writeJSON(w, http.StatusAccepted, map[string]any{"preview": preview})
		return
	case errors.Is(err, ErrScanFileCountExceeded),
		errors.Is(err, ErrScanFileTooLarge),
		errors.Is(err, ErrScanTotalTooLarge):
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": err.Error()})
		return
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	id := scanIDFromRoot(result.Root)
	b.ensureScanTracker().set(id, result)
	writeJSON(w, http.StatusOK, map[string]any{
		"id":     id,
		"result": result,
	})
}

// handleScanStatus returns the most recent ScanResult for an id.
func (b *Broker) handleScanStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
		return
	}
	res, ok := b.ensureScanTracker().get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown scan id"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": res})
}

// ensureScanTracker lazily initialises the in-memory status tracker.
func (b *Broker) ensureScanTracker() *scanStatusTracker {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.scanTracker == nil {
		b.scanTracker = newScanStatusTracker()
	}
	return b.scanTracker
}

// CommitScanStaged stages every untracked/modified path under team/ and
// commits the whole pile as author `scanner`. It is idempotent: if nothing
// is dirty, returns ("", nil) without creating an empty commit.
//
// This mirrors CommitBootstrap's contract but with the scanner identity so
// audit tools can distinguish scanner-ingested content from manually
// authored articles and from skeleton bootstrap commits.
func (r *Repo) CommitScanStaged(ctx context.Context, message string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	status, err := r.runGitLocked(ctx, "system", "status", "--porcelain")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(status) == "" {
		return "", nil
	}

	if out, err := r.runGitLocked(ctx, scannerSlug, "add", "-A"); err != nil {
		return "", scanCommitErr("git add -A", err, out)
	}
	msg := strings.TrimSpace(message)
	if msg == "" {
		msg = "scanner: ingest"
	}
	if out, err := r.runGitLocked(ctx, scannerSlug, "commit", "-q", "-m", msg); err != nil {
		return "", scanCommitErr("git commit", err, out)
	}
	sha, err := r.runGitLocked(ctx, "system", "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(sha), nil
}

func scanCommitErr(stage string, err error, out string) error {
	return errors.New("scanner: " + stage + ": " + err.Error() + ": " + strings.TrimSpace(out))
}
