package team

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// schedulerActivity is one lifecycle event for a routine: creation, an
// edit, a manual pause/resume, a triggered fire, a restored revision, etc.
// Persisted as a bounded ring buffer per slug; consumed by the Activity
// tab on the routine detail surface.
type schedulerActivity struct {
	At      string `json:"at"`
	Kind    string `json:"kind"`
	Actor   string `json:"actor,omitempty"`
	Summary string `json:"summary"`
	Detail  string `json:"detail,omitempty"`
}

// schedulerRevision snapshots the writable shape of a routine at the
// moment of an edit. Older revisions are evicted FIFO once we hit the
// per-slug cap. Versions are 1-indexed so the UI can address them by
// number without leaking ring-buffer indices.
type schedulerRevision struct {
	Version          int    `json:"version"`
	CreatedAt        string `json:"created_at"`
	Author           string `json:"author,omitempty"`
	ChangeNote       string `json:"change_note,omitempty"`
	Label            string `json:"label"`
	ScheduleExpr     string `json:"schedule_expr,omitempty"`
	IntervalMinutes  int    `json:"interval_minutes,omitempty"`
	IntervalOverride int    `json:"interval_override,omitempty"`
	TargetType       string `json:"target_type,omitempty"`
	TargetID         string `json:"target_id,omitempty"`
	Payload          string `json:"payload,omitempty"`
	Enabled          bool   `json:"enabled"`
	Channel          string `json:"channel,omitempty"`
	Kind             string `json:"kind,omitempty"`
}

const (
	schedulerActivityHistoryLimit = 50
	schedulerRevisionHistoryLimit = 20
	// minRoutineIntervalMinutes is the floor we enforce on user-created
	// routines. System-managed crons (nex-insights, etc.) self-register
	// at boot and are not subject to this rule. The cap exists to keep
	// the agent dispatcher from getting hammered by accidentally-fast
	// cadences ("every minute" is almost always a misclick).
	minRoutineIntervalMinutes = 15
)

// validateRoutineCadence enforces the minimum interval (15 min) on
// content edits. Returns a user-facing error string when the cadence
// is too tight, or "" when the schedule is acceptable. Legacy workflow
// jobs that declare cadence only via WorkflowKey + Provider (no
// schedule_expr / interval_minutes) bypass this check — the workflow
// runner owns their cadence.
func validateRoutineCadence(scheduleExpr string, intervalMinutes int) string {
	if intervalMinutes > 0 && intervalMinutes < minRoutineIntervalMinutes {
		return fmt.Sprintf(
			"interval_minutes must be at least %d (got %d)",
			minRoutineIntervalMinutes, intervalMinutes,
		)
	}
	expr := strings.TrimSpace(scheduleExpr)
	if expr == "" {
		return ""
	}
	// We can only meaningfully reason about two cron shapes: literal
	// "* * * * *" and "*/N * * * *". Anything more elaborate is allowed;
	// the floor is a guardrail against the obvious misclick, not a full
	// cron evaluator.
	if expr == "* * * * *" {
		return fmt.Sprintf(
			"routine cadence too tight: cron \"* * * * *\" fires every minute; minimum is every %d minutes",
			minRoutineIntervalMinutes,
		)
	}
	if strings.HasPrefix(expr, "*/") {
		parts := strings.Fields(expr)
		if len(parts) == 5 && strings.HasPrefix(parts[0], "*/") &&
			parts[1] == "*" && parts[2] == "*" && parts[3] == "*" && parts[4] == "*" {
			if n, err := strconv.Atoi(strings.TrimPrefix(parts[0], "*/")); err == nil && n > 0 && n < minRoutineIntervalMinutes {
				return fmt.Sprintf(
					"routine cadence too tight: cron \"%s\" fires every %d minutes; minimum is every %d minutes",
					expr, n, minRoutineIntervalMinutes,
				)
			}
		}
	}
	return ""
}

// recordSchedulerActivityLocked appends a lifecycle event. Caller MUST
// hold b.mu. Older entries are evicted FIFO once the buffer is full.
func (b *Broker) recordSchedulerActivityLocked(slug string, ev schedulerActivity) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return
	}
	if b.schedulerActivity == nil {
		b.schedulerActivity = map[string][]schedulerActivity{}
	}
	if strings.TrimSpace(ev.At) == "" {
		ev.At = time.Now().UTC().Format(time.RFC3339)
	}
	existing := b.schedulerActivity[slug]
	existing = append(existing, ev)
	if len(existing) > schedulerActivityHistoryLimit {
		existing = existing[len(existing)-schedulerActivityHistoryLimit:]
	}
	b.schedulerActivity[slug] = existing
}

// recordSchedulerRevisionLocked snapshots a routine into the revisions
// log. Caller MUST hold b.mu. Returns the new revision (with Version
// populated) so the caller can reference it in activity events.
func (b *Broker) recordSchedulerRevisionLocked(slug string, rev schedulerRevision) schedulerRevision {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return rev
	}
	if b.schedulerRevisions == nil {
		b.schedulerRevisions = map[string][]schedulerRevision{}
	}
	if strings.TrimSpace(rev.CreatedAt) == "" {
		rev.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	existing := b.schedulerRevisions[slug]
	// Versions are monotonic per slug; pick the next number above the
	// highest we've ever seen. We deliberately don't reset versions when
	// the ring buffer rolls over so users always see a stable identifier.
	nextVersion := 1
	for _, r := range existing {
		if r.Version >= nextVersion {
			nextVersion = r.Version + 1
		}
	}
	rev.Version = nextVersion
	existing = append(existing, rev)
	if len(existing) > schedulerRevisionHistoryLimit {
		existing = existing[len(existing)-schedulerRevisionHistoryLimit:]
	}
	b.schedulerRevisions[slug] = existing
	return rev
}

// snapshotSchedulerRevision builds a revision record from the live
// schedulerJob. Pure helper so callers can stage a snapshot before
// mutating the job in place.
func snapshotSchedulerRevision(job schedulerJob) schedulerRevision {
	return schedulerRevision{
		Label:            job.Label,
		ScheduleExpr:     job.ScheduleExpr,
		IntervalMinutes:  job.IntervalMinutes,
		IntervalOverride: job.IntervalOverride,
		TargetType:       job.TargetType,
		TargetID:         job.TargetID,
		Payload:          job.Payload,
		Enabled:          job.Enabled,
		Channel:          job.Channel,
		Kind:             job.Kind,
	}
}

// SchedulerActivity returns a copy of the lifecycle event log for the
// given slug, most-recent-first. Empty slice (never nil) for unknown slug.
func (b *Broker) SchedulerActivity(slug string) []schedulerActivity {
	slug = strings.TrimSpace(slug)
	out := []schedulerActivity{}
	if slug == "" {
		return out
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	src, ok := b.schedulerActivity[slug]
	if !ok {
		return out
	}
	for i := len(src) - 1; i >= 0; i-- {
		out = append(out, src[i])
	}
	return out
}

// SchedulerRevisions returns a copy of the revisions log for the given
// slug, most-recent-first. Empty slice (never nil) for unknown slug.
func (b *Broker) SchedulerRevisions(slug string) []schedulerRevision {
	slug = strings.TrimSpace(slug)
	out := []schedulerRevision{}
	if slug == "" {
		return out
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	src, ok := b.schedulerRevisions[slug]
	if !ok {
		return out
	}
	for i := len(src) - 1; i >= 0; i-- {
		out = append(out, src[i])
	}
	return out
}

// cloneSchedulerActivity / cloneSchedulerRevisions detach state for
// safe serialization outside the broker lock.
func cloneSchedulerActivity(src map[string][]schedulerActivity) map[string][]schedulerActivity {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string][]schedulerActivity, len(src))
	for slug, evs := range src {
		if len(evs) == 0 {
			continue
		}
		cp := make([]schedulerActivity, len(evs))
		copy(cp, evs)
		out[slug] = cp
	}
	return out
}

func cloneSchedulerRevisions(src map[string][]schedulerRevision) map[string][]schedulerRevision {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string][]schedulerRevision, len(src))
	for slug, revs := range src {
		if len(revs) == 0 {
			continue
		}
		cp := make([]schedulerRevision, len(revs))
		copy(cp, revs)
		out[slug] = cp
	}
	return out
}

// ── HTTP handlers ──────────────────────────────────────────────────

// handleSchedulerActivity serves GET /scheduler/{slug}/activity.
func (b *Broker) handleSchedulerActivity(w http.ResponseWriter, r *http.Request, slug string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	events := b.SchedulerActivity(slug)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"slug":   slug,
		"events": events,
	})
}

// handleSchedulerRevisions serves GET /scheduler/{slug}/revisions.
func (b *Broker) handleSchedulerRevisions(w http.ResponseWriter, r *http.Request, slug string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	revs := b.SchedulerRevisions(slug)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"slug":      slug,
		"revisions": revs,
	})
}

// handleRestoreSchedulerRevision serves
// POST /scheduler/{slug}/revisions/{n}/restore. Snapshots the current
// state as a fresh revision (so the restore is itself reversible),
// applies the chosen revision's editable fields to the live job, and
// appends an activity event recording the restore.
func (b *Broker) handleRestoreSchedulerRevision(w http.ResponseWriter, r *http.Request, slug, versionStr string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	version, err := strconv.Atoi(strings.TrimSpace(versionStr))
	if err != nil || version <= 0 {
		http.Error(w, "invalid revision version", http.StatusBadRequest)
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	var job *schedulerJob
	for i := range b.scheduler {
		if b.scheduler[i].Slug == slug {
			job = &b.scheduler[i]
			break
		}
	}
	if job == nil {
		http.Error(w, "scheduler job not found", http.StatusNotFound)
		return
	}

	var target *schedulerRevision
	for i := range b.schedulerRevisions[slug] {
		if b.schedulerRevisions[slug][i].Version == version {
			cp := b.schedulerRevisions[slug][i]
			target = &cp
			break
		}
	}
	if target == nil {
		http.Error(w, "revision not found", http.StatusNotFound)
		return
	}

	// Snapshot the in-memory state up front so we can roll back if
	// persistence fails — broker memory must not drift from disk.
	prevJob := *job

	// 1) Apply the target revision's editable fields to the live job.
	job.Label = target.Label
	job.ScheduleExpr = target.ScheduleExpr
	job.IntervalMinutes = target.IntervalMinutes
	job.IntervalOverride = target.IntervalOverride
	job.TargetType = target.TargetType
	job.TargetID = target.TargetID
	job.Payload = target.Payload
	job.Enabled = target.Enabled
	if target.Channel != "" {
		job.Channel = target.Channel
	}
	if target.Kind != "" {
		job.Kind = target.Kind
	}

	// Recompute next_run from the restored cadence so the new schedule
	// takes effect on the next scheduler tick instead of riding the old
	// timer until something else triggers a recompute.
	nextRun := nextRoutineRun(*job, time.Now().UTC())
	job.NextRun = nextRun.Format(time.RFC3339)
	job.DueAt = job.NextRun

	// 2) Snapshot the new (restored) state as a fresh revision. The
	// previous edit (e.g. v2) stays in the history untouched, so a
	// second restore brings it back — no need to confuse the revisions
	// list with a phantom "pre-restore" row carrying the old content.
	snap := snapshotSchedulerRevision(*job)
	snap.ChangeNote = fmt.Sprintf("Restored from v%d", version)
	snap.Author = "human"
	saved := b.recordSchedulerRevisionLocked(slug, snap)

	// 3) Log the lifecycle event.
	b.recordSchedulerActivityLocked(slug, schedulerActivity{
		Kind:    "revision_restored",
		Actor:   "human",
		Summary: fmt.Sprintf("Restored from v%d", version),
		Detail:  fmt.Sprintf("Saved as v%d", saved.Version),
	})

	if err := b.saveLocked(); err != nil {
		// Roll the broker back to pre-restore: undo the live job mutation,
		// drop the revision we just appended, and drop the activity row.
		*job = prevJob
		if revs := b.schedulerRevisions[slug]; len(revs) > 0 {
			b.schedulerRevisions[slug] = revs[:len(revs)-1]
			if len(b.schedulerRevisions[slug]) == 0 {
				delete(b.schedulerRevisions, slug)
			}
		}
		if act := b.schedulerActivity[slug]; len(act) > 0 {
			b.schedulerActivity[slug] = act[:len(act)-1]
			if len(b.schedulerActivity[slug]) == 0 {
				delete(b.schedulerActivity, slug)
			}
		}
		http.Error(w, "failed to persist restore", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"restored":         true,
		"slug":             slug,
		"version":          version,
		"current_revision": saved.Version,
	})
}

// schedulerCreateRequest is the wire shape for POST /scheduler. Legacy
// callers also pass Provider / WorkflowKey / NextRun / DueAt / Status;
// we accept them so the multi-agent-harness integration tests keep
// working through this path.
type schedulerCreateRequest struct {
	Slug            string `json:"slug,omitempty"`
	Label           string `json:"label"`
	Kind            string `json:"kind,omitempty"`
	TargetType      string `json:"target_type,omitempty"`
	TargetID        string `json:"target_id,omitempty"`
	Channel         string `json:"channel,omitempty"`
	Provider        string `json:"provider,omitempty"`
	WorkflowKey     string `json:"workflow_key,omitempty"`
	ScheduleExpr    string `json:"schedule_expr,omitempty"`
	IntervalMinutes int    `json:"interval_minutes,omitempty"`
	Payload         string `json:"payload,omitempty"`
	NextRun         string `json:"next_run,omitempty"`
	DueAt           string `json:"due_at,omitempty"`
	Status          string `json:"status,omitempty"`
	Enabled         *bool  `json:"enabled,omitempty"`
}

// handleCreateSchedulerJob serves POST /scheduler. Creates a routine
// from scratch (validates schedule and uniqueness, derives a slug from
// the label if absent), records both an initial revision and a
// "Created" activity event, and returns the persisted job.
func (b *Broker) handleCreateSchedulerJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req schedulerCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	label := strings.TrimSpace(req.Label)
	if label == "" {
		http.Error(w, "label is required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.ScheduleExpr) == "" && req.IntervalMinutes <= 0 {
		// Legacy callers (One workflows) declare cadence via WorkflowKey +
		// Provider rather than schedule_expr/interval_minutes. Accept that
		// shape too — those jobs are driven by a separate workflow loop.
		if strings.TrimSpace(req.WorkflowKey) == "" || strings.TrimSpace(req.Provider) == "" {
			http.Error(w, "schedule_expr or interval_minutes is required", http.StatusBadRequest)
			return
		}
	}
	if msg := validateRoutineCadence(req.ScheduleExpr, req.IntervalMinutes); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}

	slug := strings.TrimSpace(req.Slug)
	if slug == "" {
		slug = deriveSchedulerSlugFromLabel(label)
	}
	slug = strings.TrimSpace(slug)
	if slug == "" {
		http.Error(w, "could not derive a routine slug from label", http.StatusBadRequest)
		return
	}
	// Slug becomes a path segment in /scheduler/{slug}/{runs,activity,
	// revisions}. Reject anything that isn't a safe single segment so a
	// crafted slug can't break routing or escape to a sibling endpoint.
	if !isSafeSchedulerSlug(slug) {
		http.Error(w, "slug must be lowercase alphanumeric with -, _, ., or :", http.StatusBadRequest)
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = "scheduled"
	}
	job := schedulerJob{
		Slug:            slug,
		Label:           label,
		Kind:            strings.TrimSpace(req.Kind),
		TargetType:      strings.TrimSpace(req.TargetType),
		TargetID:        strings.TrimSpace(req.TargetID),
		Channel:         strings.TrimSpace(req.Channel),
		Provider:        strings.TrimSpace(req.Provider),
		WorkflowKey:     strings.TrimSpace(req.WorkflowKey),
		ScheduleExpr:    strings.TrimSpace(req.ScheduleExpr),
		IntervalMinutes: req.IntervalMinutes,
		Payload:         req.Payload,
		NextRun:         strings.TrimSpace(req.NextRun),
		DueAt:           strings.TrimSpace(req.DueAt),
		Enabled:         enabled,
		Status:          status,
	}

	// Seed NextRun if the caller didn't supply one. Without it the scheduler
	// never sees the job as due and the routine sits dormant forever.
	// Honor an explicit DueAt when NextRun is omitted — legacy callers
	// pass only DueAt for one-shot jobs and we shouldn't overwrite their
	// chosen fire time with a freshly computed cadence.
	if job.NextRun == "" {
		if job.DueAt != "" {
			job.NextRun = job.DueAt
		} else {
			next := nextRoutineRun(job, time.Now().UTC())
			job.NextRun = next.Format(time.RFC3339)
			job.DueAt = job.NextRun
		}
	} else if job.DueAt == "" {
		job.DueAt = job.NextRun
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// Reject duplicates explicitly rather than silently overwriting.
	for _, existing := range b.scheduler {
		if existing.Slug == slug {
			http.Error(w, "a routine with that slug already exists", http.StatusConflict)
			return
		}
	}

	if err := b.scheduleJobLocked(job); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	rev := snapshotSchedulerRevision(job)
	rev.ChangeNote = "Initial revision"
	rev.Author = "human"
	saved := b.recordSchedulerRevisionLocked(slug, rev)

	b.recordSchedulerActivityLocked(slug, schedulerActivity{
		Kind:    "created",
		Actor:   "human",
		Summary: "Routine created",
		Detail:  fmt.Sprintf("Initial revision v%d", saved.Version),
	})

	if err := b.saveLocked(); err != nil {
		// Roll back every in-memory artifact we just inserted so the
		// broker doesn't ship state that's not on disk.
		for i := len(b.scheduler) - 1; i >= 0; i-- {
			if b.scheduler[i].Slug == slug {
				b.scheduler = append(b.scheduler[:i], b.scheduler[i+1:]...)
				break
			}
		}
		if revs := b.schedulerRevisions[slug]; len(revs) > 0 {
			b.schedulerRevisions[slug] = revs[:len(revs)-1]
			if len(b.schedulerRevisions[slug]) == 0 {
				delete(b.schedulerRevisions, slug)
			}
		}
		if act := b.schedulerActivity[slug]; len(act) > 0 {
			b.schedulerActivity[slug] = act[:len(act)-1]
			if len(b.schedulerActivity[slug]) == 0 {
				delete(b.schedulerActivity, slug)
			}
		}
		http.Error(w, "failed to persist routine", http.StatusInternalServerError)
		return
	}

	// Re-read after persistence so we return the canonical shape.
	var persisted schedulerJob
	for _, existing := range b.scheduler {
		if existing.Slug == slug {
			persisted = existing
			break
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"job": persisted})
}

// isSafeSchedulerSlug enforces the path-segment shape we accept on the
// /scheduler/{slug}/{runs,activity,revisions} surface. Lowercase
// alphanumeric plus a small punctuation alphabet matches every slug the
// broker auto-derives (deriveSchedulerSlugFromLabel + normalizeSchedulerSlug)
// while keeping /, ?, &, ., .., and unicode-confusable characters out.
func isSafeSchedulerSlug(s string) bool {
	if s == "" || len(s) > 200 {
		return false
	}
	if s == "." || s == ".." {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.' || r == ':':
		default:
			return false
		}
	}
	return true
}

// deriveSchedulerSlugFromLabel turns "Weekly Digest" into "weekly-digest".
// Empty or punctuation-only labels yield empty strings; the caller is
// expected to reject those cases.
func deriveSchedulerSlugFromLabel(label string) string {
	var b strings.Builder
	prevDash := true
	for _, r := range strings.ToLower(label) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case r == '_' || r == '-' || r == ' ':
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	return out
}

// listSlugSet returns the set of registered routine slugs, used by tests
// and the activity backfill loader to drop entries pointing at
// long-deleted jobs.
func (b *Broker) listSlugSet() map[string]struct{} {
	out := make(map[string]struct{}, len(b.scheduler))
	for _, j := range b.scheduler {
		out[j.Slug] = struct{}{}
	}
	return out
}

// sortRevisionsByVersionDesc — invoked by tests; in production callers
// receive a most-recent-first slice via SchedulerRevisions.
func sortRevisionsByVersionDesc(revs []schedulerRevision) {
	sort.Slice(revs, func(i, j int) bool { return revs[i].Version > revs[j].Version })
}

// healStuckRoutines auto-recovers agent-targeted routines that ended up
// in a terminal "done" status before this build's fix. Old code marked
// every fire as "done" which broke schedulerJobDue; on restart we rewrite
// those rows so the schedule resumes. One-shot routines (task / request
// follow-ups) are left alone — they're meant to terminate.
func healStuckRoutines(jobs []schedulerJob) {
	now := time.Now().UTC()
	for i := range jobs {
		job := &jobs[i]
		if strings.TrimSpace(job.TargetType) != "agent" {
			continue
		}
		if !strings.EqualFold(job.Status, "done") {
			continue
		}
		next := nextRoutineRun(*job, now)
		job.Status = "scheduled"
		job.NextRun = next.Format(time.RFC3339)
		job.DueAt = job.NextRun
	}
}
