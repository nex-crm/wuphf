package team

// skill_compile_endpoints.go owns the HTTP handlers for /skills/compile and
// /skills/compile/stats. Routes are registered in StartOnPort alongside the
// other /skills handlers.

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// skillCompileRequest is the JSON shape POST /skills/compile accepts. Both
// fields are optional: an empty body kicks off a full-tree non-dry-run pass.
type skillCompileRequest struct {
	DryRun    bool   `json:"dry_run,omitempty"`
	ScopePath string `json:"scope_path,omitempty"`
}

// skillCompileQueuedResponse is returned with HTTP 202 when a coalesce
// occurs.
type skillCompileQueuedResponse struct {
	Queued bool `json:"queued"`
}

// skillCompileSkippedResponse is returned with HTTP 200 when the cron path
// catches a request inside the cooldown window. We don't surface this to
// manual clicks because cooldown only applies to cron triggers — but the
// shape stays consistent for forward compatibility.
type skillCompileSkippedResponse struct {
	Skipped string `json:"skipped"`
}

// handlePostSkillCompile triggers a manual Stage A compile pass. Auth is
// applied by the registration site (requireAuth wrapper).
func (b *Broker) handlePostSkillCompile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body skillCompileRequest
	// An empty body is valid (no opts supplied). Any non-empty payload must
	// parse cleanly. io.EOF == empty body; everything else is a 400.
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	scopePath := strings.TrimSpace(body.ScopePath)
	res, err := b.compileWikiSkills(r.Context(), scopePath, body.DryRun, "manual")
	if err != nil {
		switch {
		case errors.Is(err, ErrCompileCoalesced):
			writeJSON(w, http.StatusAccepted, skillCompileQueuedResponse{Queued: true})
			return
		case errors.Is(err, ErrCompileCooldown):
			// Manual triggers are not subject to cooldown today, so this
			// path is only reachable via tests / unusual configurations.
			writeJSON(w, http.StatusOK, skillCompileSkippedResponse{Skipped: "cooldown"})
			return
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	writeJSON(w, http.StatusOK, res)
}

// skillCompileStatsResponse is the JSON shape GET /skills/compile/stats
// returns. Times are RFC3339 in UTC; durations are integer milliseconds.
type skillCompileStatsResponse struct {
	ManualClicksTotal             int64  `json:"manual_clicks_total"`
	CronTicksTotal                int64  `json:"cron_ticks_total"`
	ProposalsCreatedTotal         int64  `json:"proposals_created_total"`
	ProposalsApprovedTotal        int64  `json:"proposals_approved_total"`
	ProposalsRejectedByGuardTotal int64  `json:"proposals_rejected_by_guard_total"`
	LastTickDurationMs            int64  `json:"last_tick_duration_ms"`
	LastSkillCompilePassAt        string `json:"last_skill_compile_pass_at,omitempty"`
	StageBProposalsTotal          int64  `json:"stage_b_proposals_total"`
	// EnhancementCandidatesTotal counts proposals diverted to enhance an
	// existing skill instead of creating a new one (PR 7 task #13). Eval
	// asserts on this field as a quality signal for the proposal funnel.
	EnhancementCandidatesTotal int64 `json:"enhancement_candidates_total"`
	// EnhancementAcceptedTotal counts enhance_skill_proposal interviews
	// resolved with "enhance" (PR 7 task #15).
	EnhancementAcceptedTotal int64 `json:"enhancement_accepted_total"`
	// EnhancementOverriddenTotal counts enhance_skill_proposal interviews
	// resolved with "approve_anyway" — the human bypassed the gate (PR 7
	// task #15).
	EnhancementOverriddenTotal int64 `json:"enhancement_overridden_total"`
	// CatalogBytesPerAgentMax is the maximum byte length the prompt catalog
	// would render to across active office members for the current b.skills
	// snapshot. PR 7 surfaces it so operators can spot catalog bloat before
	// it pushes prompt size out of the cache window. Computed on each request;
	// O(members * skills) but bounded.
	CatalogBytesPerAgentMax int `json:"catalog_bytes_per_agent_max"`
	// CounterNudgesFiredTotal counts skill_review_nudge tasks fired by the
	// Hermes-style per-agent counter (#379, Stage B').
	CounterNudgesFiredTotal int64 `json:"counter_nudges_fired_total"`
	// CounterPerAgent surfaces the per-agent counter snapshot from the
	// skill counter (Stage B'). Nil when no counter is wired.
	CounterPerAgent map[string]SkillCounterMetrics `json:"counter_per_agent,omitempty"`
}

// skillCatalogSoftWarnBytes is the soft warning threshold for the per-agent
// catalog. Above this the handler emits a WARN log; the hard 8KB cap is NOT
// enforced for v1 — operators get a metric and a log, not a 500.
const skillCatalogSoftWarnBytes = 6 * 1024

// handleGetSkillCompileStats returns a snapshot of the compile metrics.
func (b *Broker) handleGetSkillCompileStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	b.mu.Lock()
	snap := snapshotSkillCompileMetrics(&b.skillCompileMetrics)
	maxBytes, maxSlug := b.maxCatalogBytesLocked()
	counter := b.skillCounter
	b.mu.Unlock()

	if maxBytes > skillCatalogSoftWarnBytes {
		slog.Warn("skills: per-agent catalog exceeds soft warning threshold",
			"agent", maxSlug, "bytes", maxBytes, "warn_bytes", skillCatalogSoftWarnBytes)
	}

	resp := skillCompileStatsResponse{
		ManualClicksTotal:             snap.ManualClicksTotal,
		CronTicksTotal:                snap.CronTicksTotal,
		ProposalsCreatedTotal:         snap.ProposalsCreatedTotal,
		ProposalsApprovedTotal:        snap.ProposalsApprovedTotal,
		ProposalsRejectedByGuardTotal: snap.ProposalsRejectedByGuardTotal,
		LastTickDurationMs:            snap.LastTickDurationMs,
		StageBProposalsTotal:          snap.StageBProposalsTotal,
		EnhancementCandidatesTotal:    snap.EnhancementCandidatesTotal,
		EnhancementAcceptedTotal:      snap.EnhancementAcceptedTotal,
		EnhancementOverriddenTotal:    snap.EnhancementOverriddenTotal,
		CatalogBytesPerAgentMax:       maxBytes,
		CounterNudgesFiredTotal:       snap.CounterNudgesFiredTotal,
	}
	if counter != nil {
		resp.CounterPerAgent = counter.Stats()
	}
	if snap.LastSkillCompilePassAtNano != 0 {
		resp.LastSkillCompilePassAt = time.Unix(0, snap.LastSkillCompilePassAtNano).UTC().Format(timeRFC3339)
	}
	writeJSON(w, http.StatusOK, resp)
}

// maxCatalogBytesLocked returns the largest per-agent catalog byte size across
// every active office member. Caller MUST hold b.mu. Returns (0, "") if no
// member ends up with a non-empty catalog. Used by /skills/compile/stats so
// operators can detect catalog bloat before it pushes prompt size out of cache.
func (b *Broker) maxCatalogBytesLocked() (int, string) {
	maxBytes := 0
	maxSlug := ""
	for _, m := range b.members {
		visible := b.listSkillsForAgentLocked(m.Slug, listSkillsOpts{activeOnly: true})
		size := len(renderSkillCatalogSection(visible))
		if size > maxBytes {
			maxBytes = size
			maxSlug = m.Slug
		}
	}
	return maxBytes, maxSlug
}

// timeRFC3339 is RFC3339 with second precision (no timezone offset suffix
// trickery). We import time elsewhere; spelling the layout as a constant
// keeps the format explicit alongside the response shape.
const timeRFC3339 = "2006-01-02T15:04:05Z07:00"
