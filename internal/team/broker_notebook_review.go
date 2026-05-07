package team

// broker_notebook_review.go is PR 4 of the notebook-wiki-promise design.
//
// It exposes the demand-index ranking surface to the MCP layer so the CEO
// agent can call team_notebook_review and see which notebook entries the
// rest of the team is implicitly asking for. PR 3 already records the
// signals; this file just hands them out and accepts CEO-flag writes.
//
// Lock discipline:
//   - Handlers never touch b.mu. They read b.demandIndex under that index's
//     own mutex (via TopCandidates / Record) and snippet bytes via the
//     wiki worker (its own locking). No re-entry into broker state.
//   - When b.demandIndex is nil (e.g. PR 4 lands without PR 3 wiring on a
//     reverted branch), the endpoint returns 503 instead of crashing.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// notebookReviewCandidatesDefaultN is the default page size when the caller
// does not pass ?n=. 20 matches the spec's TopCandidates(20) ceiling and
// stays well under the MCP tool's response budget.
const notebookReviewCandidatesDefaultN = 20

// notebookReviewCandidatesMaxN bounds the caller-supplied n to keep the
// response small. 100 is generous; anything above is almost certainly a
// misuse of the endpoint.
const notebookReviewCandidatesMaxN = 100

// notebookReviewFlagRequest is the POST body shape used to record CEO flag
// signals. EntryPaths is the only required field; Actor falls back to the
// X-WUPHF-Agent header when omitted.
type notebookReviewFlagRequest struct {
	EntryPaths []string `json:"entry_paths"`
	Actor      string   `json:"actor,omitempty"`
}

// handleNotebookReviewCandidates serves both:
//
//	GET  /notebook/review-candidates[?n=N]   → []DemandCandidate ranked
//	POST /notebook/review-candidates         → record DemandSignalCEOReviewFlag
//
// The endpoint is forward-compat: a broker without PR 3's demandIndex wired
// returns 503 with a stable error string, so the MCP tool can degrade
// gracefully without crashing the office.
func (b *Broker) handleNotebookReviewCandidates(w http.ResponseWriter, r *http.Request) {
	idx := b.demandIndex
	if idx == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "demand index not active"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		b.serveNotebookReviewCandidatesGET(w, r, idx)
	case http.MethodPost:
		b.serveNotebookReviewCandidatesPOST(w, r, idx)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (b *Broker) serveNotebookReviewCandidatesGET(w http.ResponseWriter, r *http.Request, idx *NotebookDemandIndex) {
	n := notebookReviewCandidatesDefaultN
	if raw := strings.TrimSpace(r.URL.Query().Get("n")); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "n must be a positive integer"})
			return
		}
		if v > notebookReviewCandidatesMaxN {
			v = notebookReviewCandidatesMaxN
		}
		n = v
	}
	candidates := idx.TopCandidates(n)
	if candidates == nil {
		candidates = []DemandCandidate{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"candidates": candidates,
		"threshold":  idx.Threshold(),
		"window":     idx.WindowDays(),
	})
}

func (b *Broker) serveNotebookReviewCandidatesPOST(w http.ResponseWriter, r *http.Request, idx *NotebookDemandIndex) {
	var req notebookReviewFlagRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body: " + err.Error()})
		return
	}
	paths := dedupeNonEmpty(req.EntryPaths)
	if len(paths) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "entry_paths is required"})
		return
	}
	actor := strings.TrimSpace(req.Actor)
	if actor == "" {
		actor = strings.TrimSpace(r.Header.Get(agentRateLimitHeader))
	}
	if actor == "" {
		actor = "ceo"
	}
	now := time.Now().UTC()
	recorded := make([]string, 0, len(paths))
	skipped := make([]map[string]string, 0)
	for _, path := range paths {
		owner, ok := ownerSlugFromNotebookPath(path)
		if !ok {
			skipped = append(skipped, map[string]string{"path": path, "reason": "owner slug not extractable from path"})
			continue
		}
		evt := PromotionDemandEvent{
			EntryPath:    path,
			OwnerSlug:    owner,
			SearcherSlug: actor,
			Signal:       DemandSignalCEOReviewFlag,
			RecordedAt:   now,
		}
		if err := idx.Record(evt); err != nil {
			if errors.Is(err, ErrPromotionDemandInvalid) {
				skipped = append(skipped, map[string]string{"path": path, "reason": err.Error()})
				continue
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("record %s: %v", path, err)})
			return
		}
		recorded = append(recorded, path)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"recorded": recorded,
		"skipped":  skipped,
	})
}

// ownerSlugFromNotebookPath extracts the owner agent slug from a path of the
// form "agents/{slug}/notebook/{file}". Returns (slug, true) only when the
// shape matches; (zero, false) otherwise so the caller can skip malformed
// entries without recording a junk event.
func ownerSlugFromNotebookPath(path string) (string, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", false
	}
	parts := strings.SplitN(path, "/", 4)
	if len(parts) < 3 {
		return "", false
	}
	if parts[0] != "agents" || parts[2] != "notebook" {
		return "", false
	}
	slug := strings.TrimSpace(parts[1])
	if slug == "" {
		return "", false
	}
	return slug, true
}

// dedupeNonEmpty trims whitespace, drops empties, and removes duplicates
// while preserving input order. Tiny helper kept local to this file; the
// few other slice-of-string dedupes in the broker have their own loops.
func dedupeNonEmpty(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, it := range items {
		v := strings.TrimSpace(it)
		if v == "" {
			continue
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
