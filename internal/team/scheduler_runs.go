package team

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"
)

// schedulerRun is one fire of a scheduler job. The broker keeps a bounded
// ring buffer per slug so the Routines UI can answer "what just happened?"
// without standing up a separate audit store.
type schedulerRun struct {
	Slug          string   `json:"slug"`
	StartedAt     string   `json:"started_at"`
	FinishedAt    string   `json:"finished_at,omitempty"`
	Status        string   `json:"status"`
	Message       string   `json:"message,omitempty"`
	TriggeredBy   string   `json:"triggered_by,omitempty"`
	OutputSummary string   `json:"output_summary,omitempty"`
	Events        []string `json:"events,omitempty"`
	ErrorDetail   string   `json:"error,omitempty"`
	TargetType    string   `json:"target_type,omitempty"`
	TargetID      string   `json:"target_id,omitempty"`
}

// schedulerRunHistoryLimit caps the per-slug ring buffer. Twenty entries is
// roughly an hour of a 3-minute cron and a week of an hourly one — enough
// for "what just happened" without dragging persisted state weight up.
const schedulerRunHistoryLimit = 20

// cloneSchedulerRuns returns a deep copy suitable for serialization. The
// persistence path runs outside the lock once the snapshot is taken, so
// the snapshot must be fully detached from the live map — `copy` only
// shallow-copies the struct slice, leaving each Events slice aliasing
// the original backing array. Clone Events per run so caller-visible
// history can't be mutated through shared slice state.
func cloneSchedulerRuns(src map[string][]schedulerRun) map[string][]schedulerRun {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string][]schedulerRun, len(src))
	for slug, runs := range src {
		if len(runs) == 0 {
			continue
		}
		cp := make([]schedulerRun, len(runs))
		for i, r := range runs {
			if len(r.Events) > 0 {
				events := make([]string, len(r.Events))
				copy(events, r.Events)
				r.Events = events
			}
			cp[i] = r
		}
		out[slug] = cp
	}
	return out
}

// recordSchedulerRunLocked appends a fire record to the per-slug ring
// buffer. Caller MUST hold b.mu. Older entries are evicted FIFO once the
// buffer is full.
func (b *Broker) recordSchedulerRunLocked(run schedulerRun) {
	slug := strings.TrimSpace(run.Slug)
	if slug == "" {
		return
	}
	if b.schedulerRuns == nil {
		b.schedulerRuns = map[string][]schedulerRun{}
	}
	run.Slug = slug
	if run.StartedAt == "" {
		run.StartedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if run.Status == "" {
		run.Status = "ok"
	}
	existing := b.schedulerRuns[slug]
	existing = append(existing, run)
	if len(existing) > schedulerRunHistoryLimit {
		existing = existing[len(existing)-schedulerRunHistoryLimit:]
	}
	b.schedulerRuns[slug] = existing
}

// SchedulerRuns returns a copy of the run history for the given slug,
// most-recent-first. Empty slug or unknown slug yields an empty slice
// (never nil) so JSON encoding produces `[]` instead of `null`.
func (b *Broker) SchedulerRuns(slug string) []schedulerRun {
	slug = strings.TrimSpace(slug)
	out := []schedulerRun{}
	if slug == "" {
		return out
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	src, ok := b.schedulerRuns[slug]
	if !ok {
		return out
	}
	// Reverse-copy so callers receive most-recent-first.
	for i := len(src) - 1; i >= 0; i-- {
		out = append(out, src[i])
	}
	return out
}

// handleSchedulerRuns serves GET /scheduler/{slug}/runs.
func (b *Broker) handleSchedulerRuns(w http.ResponseWriter, r *http.Request, slug string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	runs := b.SchedulerRuns(slug)
	// Stable secondary sort by StartedAt desc — defends against any caller
	// that injects runs out of order.
	sort.SliceStable(runs, func(i, j int) bool {
		return runs[i].StartedAt > runs[j].StartedAt
	})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"slug": slug,
		"runs": runs,
	})
}
