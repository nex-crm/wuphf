package team

// broker_scheduler_routines.go — agent-registered standing automations
// (ten-out-of-ten Wave D / D1).
//
// ICP-eval v3 [18:30–18:36]: a single "weekly Monday 9am summary" ask
// produced TWO provider-session cron jobs registered by two different
// agents, each carrying a "dies in 7 days with the session" caveat, and
// the Scheduled Tasks app showed neither. The office HAD a persistent
// scheduler the whole time (b.scheduler → watchdogScheduler →
// processAgentJob) — there was simply no agent-reachable registration
// path for general standing automations, so agents reached for the
// provider's session-scoped cron instead.
//
// POST /scheduler/routines is that path. It is the broker half of the
// team_routine MCP tool:
//
//   - PERSISTENT: the job lands in b.scheduler and is saved to
//     broker-state.json like every other routine — it survives broker
//     restarts and provider session ends.
//   - VISIBLE: GET /scheduler (the Scheduled Tasks app's data source)
//     lists it as a user routine (target_type "agent", not
//     system-managed), so the human can pause, edit, or re-cadence it.
//   - DEDUPED: registering the same normalized purpose+schedule again
//     (any agent, any wording that normalizes equal) UPDATES the
//     existing job instead of minting a duplicate.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/calendar"
)

// routineRegisterRequest is the wire shape for POST /scheduler/routines.
// Additive: nothing else consumes this path yet.
type routineRegisterRequest struct {
	// Purpose is the one-line description of what the automation does.
	// It becomes the routine's label and one half of the dedupe key.
	Purpose string `json:"purpose"`
	// Schedule is a cron expression or calendar shorthand (daily, hourly,
	// 4h, "0 9 * * 1"). The other half of the dedupe key.
	Schedule string `json:"schedule"`
	// Channel the routine posts into on each fire. Empty routes to the
	// owner's DM (processAgentJob semantics).
	Channel string `json:"channel,omitempty"`
	// Owner is the agent slug tagged on each fire. Defaults to CreatedBy.
	Owner string `json:"owner,omitempty"`
	// Prompt is posted to the owner on every scheduled run (job payload).
	Prompt string `json:"prompt,omitempty"`
	// CreatedBy is the registering actor (agent slug).
	CreatedBy string `json:"created_by,omitempty"`
}

// normalizeRoutinePurpose reduces a routine purpose to its sorted,
// deduplicated lowercase token set, so "Weekly Monday 9am renewal-risk
// summary to #general!" and "weekly monday 9am renewal risk summary to
// general" normalize equal. Deliberately word-order-insensitive: two
// agents describing the same automation rarely agree on word order.
func normalizeRoutinePurpose(s string) string {
	var tokens []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			tokens = append(tokens, cur.String())
			cur.Reset()
		}
	}
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			cur.WriteRune(r)
		default:
			flush()
		}
	}
	flush()
	seen := make(map[string]struct{}, len(tokens))
	uniq := tokens[:0]
	for _, tok := range tokens {
		if _, ok := seen[tok]; ok {
			continue
		}
		seen[tok] = struct{}{}
		uniq = append(uniq, tok)
	}
	sort.Strings(uniq)
	return strings.Join(uniq, " ")
}

// normalizeRoutineScheduleExpr canonicalizes a schedule expression for
// dedupe comparison: lowercase, single-space field separation.
func normalizeRoutineScheduleExpr(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

// isAgentRoutineJob reports whether a scheduler row is an agent-registered
// (or human-created agent-targeted) standing automation — the dedupe
// population. System crons and per-instance lifecycle jobs (task/request
// follow-ups, rechecks) are never dedupe candidates.
func isAgentRoutineJob(job schedulerJob) bool {
	if job.SystemManaged {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(job.TargetType), "agent")
}

// findMatchingRoutineLocked returns the index of an existing agent routine
// that the candidate registration should update, or -1. Caller holds b.mu.
// A match is: identical slug, OR identical normalized purpose+schedule.
func (b *Broker) findMatchingRoutineLocked(slug, normPurpose, normSchedule string) int {
	for i := range b.scheduler {
		job := b.scheduler[i]
		if !isAgentRoutineJob(job) {
			continue
		}
		if job.Slug == slug {
			return i
		}
		if normPurpose != "" &&
			normalizeRoutinePurpose(job.Label) == normPurpose &&
			normalizeRoutineScheduleExpr(job.ScheduleExpr) == normSchedule {
			return i
		}
	}
	return -1
}

// handleRegisterRoutine serves POST /scheduler/routines — the persistent,
// deduplicating registration path for agent standing automations. Returns
// 201 + {"job":…,"updated":false} on create and 200 + {"updated":true}
// when the registration converged onto an existing routine.
func (b *Broker) handleRegisterRoutine(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req routineRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	purpose := strings.TrimSpace(req.Purpose)
	if purpose == "" {
		http.Error(w, "purpose is required", http.StatusBadRequest)
		return
	}
	scheduleExpr := strings.TrimSpace(req.Schedule)
	if scheduleExpr == "" {
		http.Error(w, "schedule is required", http.StatusBadRequest)
		return
	}
	sched, err := calendar.ParseCron(scheduleExpr)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid schedule %q: %v", scheduleExpr, err), http.StatusBadRequest)
		return
	}
	if msg := validateRoutineCadence(scheduleExpr, 0); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	now := time.Now().UTC()
	nextRun := sched.Next(now)
	if nextRun.IsZero() {
		http.Error(w, fmt.Sprintf("could not compute next run for %q", scheduleExpr), http.StatusBadRequest)
		return
	}
	createdBy := strings.TrimSpace(req.CreatedBy)
	owner := strings.TrimSpace(req.Owner)
	if owner == "" {
		owner = createdBy
	}
	if owner == "" {
		http.Error(w, "owner agent slug is required (owner or created_by)", http.StatusBadRequest)
		return
	}
	actor := createdBy
	if actor == "" {
		actor = owner
	}
	slug := deriveSchedulerSlugFromLabel(purpose)
	if slug == "" {
		http.Error(w, "could not derive a routine slug from purpose", http.StatusBadRequest)
		return
	}
	channel := ""
	if strings.TrimSpace(req.Channel) != "" {
		channel = normalizeChannelSlug(req.Channel)
	}
	normPurpose := normalizeRoutinePurpose(purpose)
	normSchedule := normalizeRoutineScheduleExpr(scheduleExpr)
	nextRunStr := nextRun.Format(time.RFC3339)

	b.mu.Lock()
	defer b.mu.Unlock()

	if idx := b.findMatchingRoutineLocked(slug, normPurpose, normSchedule); idx >= 0 {
		job := &b.scheduler[idx]
		snapshot := *job
		job.Label = purpose
		job.ScheduleExpr = scheduleExpr
		job.TargetType = "agent"
		job.TargetID = owner
		if channel != "" {
			job.Channel = channel
		}
		if strings.TrimSpace(req.Prompt) != "" {
			job.Payload = strings.TrimSpace(req.Prompt)
		}
		// A re-registration re-arms the cadence but deliberately does NOT
		// flip Enabled: if the human paused the routine from Scheduled
		// Tasks, an agent must not silently resurrect it.
		job.NextRun = nextRunStr
		job.DueAt = nextRunStr
		if strings.EqualFold(job.Status, "done") || strings.EqualFold(job.Status, "canceled") {
			job.Status = "scheduled"
		}
		rev := snapshotSchedulerRevision(*job)
		rev.ChangeNote = "Re-registered by @" + actor + " (same purpose + schedule)"
		rev.Author = actor
		saved := b.recordSchedulerRevisionLocked(job.Slug, rev)
		b.recordSchedulerActivityLocked(job.Slug, schedulerActivity{
			Kind:    "edited",
			Actor:   actor,
			Summary: "Routine re-registered (deduped onto the existing job)",
			Detail:  fmt.Sprintf("Saved as v%d", saved.Version),
		})
		if err := b.saveLocked(); err != nil {
			*job = snapshot
			if revs := b.schedulerRevisions[job.Slug]; len(revs) > 0 {
				b.schedulerRevisions[job.Slug] = revs[:len(revs)-1]
				if len(b.schedulerRevisions[job.Slug]) == 0 {
					delete(b.schedulerRevisions, job.Slug)
				}
			}
			if act := b.schedulerActivity[job.Slug]; len(act) > 0 {
				b.schedulerActivity[job.Slug] = act[:len(act)-1]
				if len(b.schedulerActivity[job.Slug]) == 0 {
					delete(b.schedulerActivity, job.Slug)
				}
			}
			http.Error(w, "failed to persist routine update", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"job": *job, "updated": true})
		return
	}

	// No dedupe match — create. The derived slug may still collide with a
	// non-routine row (system cron, lifecycle follow-up); uniquify rather
	// than overwrite a job we deliberately excluded from dedupe.
	taken := func(s string) bool {
		// Reserved sub-resource path segments under /scheduler/ — a job
		// with one of these slugs would shadow the endpoint itself.
		if s == "routines" || s == "system-specs" {
			return true
		}
		for i := range b.scheduler {
			if b.scheduler[i].Slug == s {
				return true
			}
		}
		return false
	}
	unique := slug
	for n := 2; taken(unique); n++ {
		unique = fmt.Sprintf("%s-%d", slug, n)
	}
	job := schedulerJob{
		Slug:         unique,
		Kind:         "agent_routine",
		Label:        purpose,
		TargetType:   "agent",
		TargetID:     owner,
		Channel:      channel,
		ScheduleExpr: scheduleExpr,
		Payload:      strings.TrimSpace(req.Prompt),
		NextRun:      nextRunStr,
		DueAt:        nextRunStr,
		Status:       "scheduled",
		Enabled:      true,
	}
	b.scheduler = append(b.scheduler, job)
	rev := snapshotSchedulerRevision(job)
	rev.ChangeNote = "Registered by @" + actor
	rev.Author = actor
	saved := b.recordSchedulerRevisionLocked(unique, rev)
	b.recordSchedulerActivityLocked(unique, schedulerActivity{
		Kind:    "created",
		Actor:   actor,
		Summary: "Standing automation registered",
		Detail:  fmt.Sprintf("Initial revision v%d", saved.Version),
	})
	if err := b.saveLocked(); err != nil {
		for i := len(b.scheduler) - 1; i >= 0; i-- {
			if b.scheduler[i].Slug == unique {
				b.scheduler = append(b.scheduler[:i], b.scheduler[i+1:]...)
				break
			}
		}
		if revs := b.schedulerRevisions[unique]; len(revs) > 0 {
			b.schedulerRevisions[unique] = revs[:len(revs)-1]
			if len(b.schedulerRevisions[unique]) == 0 {
				delete(b.schedulerRevisions, unique)
			}
		}
		if act := b.schedulerActivity[unique]; len(act) > 0 {
			b.schedulerActivity[unique] = act[:len(act)-1]
			if len(b.schedulerActivity[unique]) == 0 {
				delete(b.schedulerActivity, unique)
			}
		}
		http.Error(w, "failed to persist routine", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"job": job, "updated": false})
}
