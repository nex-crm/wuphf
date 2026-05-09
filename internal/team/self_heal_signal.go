package team

// self_heal_signal.go is a Stage B signal source. Resolved self-heal
// incidents (PipelineID == "incident", Status == "done", title prefixed
// with selfHealingTaskTitlePrefix) describe a recovery the team learned
// the hard way — exactly the kind of memory worth surfacing as a candidate
// skill. The synthesizer (PR 2-B) is the LLM gate that decides whether
// to materialise the candidate.

import (
	"context"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// SelfHealSignalScanner scans broker tasks for resolved self-heal incidents
// and emits one SkillCandidate per incident.
type SelfHealSignalScanner struct {
	broker *Broker

	mu            sync.Mutex
	minResolvedAt time.Time

	maxCandidatesPerPass int
}

// NewSelfHealSignalScanner constructs a scanner. The first pass surfaces
// every resolved incident; subsequent passes are incremental from
// minResolvedAt = time.Now() at the end of the previous pass.
func NewSelfHealSignalScanner(b *Broker) *SelfHealSignalScanner {
	return &SelfHealSignalScanner{
		broker:               b,
		maxCandidatesPerPass: envIntDefault("WUPHF_STAGE_B_SELFHEAL_MAX_PER_PASS", 5),
	}
}

// selfHealTitleRE extracts the agent slug + reason from titles like
//
//	"Self-heal @deploy-bot on task-7"
//	"Self-heal @deploy-bot runtime failure"
//
// The selfHealingTaskTitle helper builds these — see self_healing.go.
var selfHealTitleRE = regexp.MustCompile(`^Self-heal\s+@?(?P<agent>[\w\-]+)(?:\s+on\s+(?P<task>[\w\-]+))?(?:\s+(?P<rest>.*))?$`)

// reasonRE pulls "Trigger: <reason>" out of the incident details payload
// produced by selfHealingTaskDetails.
var reasonRE = regexp.MustCompile(`(?m)^-\s*Trigger:\s*(?P<reason>[^\n]+)$`)

// detailRE pulls "Detail: <reason>" — the one-line human/agent description
// of the blocker.
var detailRE = regexp.MustCompile(`(?m)^-\s*Detail:\s*(?P<detail>[^\n]+)$`)

// Scan returns a SkillCandidate for every resolved self-heal incident
// whose UpdatedAt is strictly greater than s.minResolvedAt. After a
// successful pass, minResolvedAt is advanced to time.Now() so subsequent
// passes are incremental. Returns up to maxCandidatesPerPass candidates
// ordered by UpdatedAt desc.
func (s *SelfHealSignalScanner) Scan(ctx context.Context) ([]SkillCandidate, error) {
	if s == nil || s.broker == nil {
		return nil, nil
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.Lock()
	cutoff := s.minResolvedAt
	s.mu.Unlock()

	tasks := s.snapshotResolvedSelfHealTasks(cutoff)
	if len(tasks) == 0 {
		s.mu.Lock()
		s.minResolvedAt = time.Now().UTC()
		s.mu.Unlock()
		return nil, nil
	}

	// Newest first so the per-pass cap surfaces the most recent learnings.
	sort.SliceStable(tasks, func(i, j int) bool {
		return tasks[i].UpdatedAt > tasks[j].UpdatedAt
	})

	if s.maxCandidatesPerPass > 0 && len(tasks) > s.maxCandidatesPerPass {
		tasks = tasks[:s.maxCandidatesPerPass]
	}

	candidates := make([]SkillCandidate, 0, len(tasks))
	for _, task := range tasks {
		candidate := s.candidateFromTask(ctx, task)
		candidates = append(candidates, candidate)
		slog.Info("self_heal_candidate_emitted",
			"task_id", task.ID,
			"name", candidate.SuggestedName,
			"owner", task.Owner,
		)
	}

	s.mu.Lock()
	s.minResolvedAt = time.Now().UTC()
	s.mu.Unlock()

	return candidates, nil
}

// snapshotResolvedSelfHealTasks returns the resolved self-heal incidents
// the broker currently holds, filtered by cutoff. We copy under b.mu so
// concurrent broker mutations can't tear the slice.
func (s *SelfHealSignalScanner) snapshotResolvedSelfHealTasks(cutoff time.Time) []teamTask {
	s.broker.mu.Lock()
	defer s.broker.mu.Unlock()

	var out []teamTask
	for _, task := range s.broker.tasks {
		if task.PipelineID != "incident" {
			continue
		}
		if task.status != "done" {
			continue
		}
		if !isSelfHealingTaskTitle(task.Title) {
			continue
		}
		updated, err := time.Parse(time.RFC3339, task.UpdatedAt)
		if err == nil && !cutoff.IsZero() && !updated.After(cutoff) {
			continue
		}
		// Defensive copy — task is a value type but the slice is the
		// broker's; appending here keeps us decoupled.
		out = append(out, task)
	}
	return out
}

// candidateFromTask builds a SkillCandidate from a single resolved
// self-heal incident.
func (s *SelfHealSignalScanner) candidateFromTask(ctx context.Context, task teamTask) SkillCandidate {
	agent, reason := parseSelfHealTitle(task.Title)
	detail := parseSelfHealDetail(task.Details)
	if reason == "" {
		reason = parseSelfHealReason(task.Details)
	}
	if reason == "" {
		reason = "unknown"
	}

	updatedAt, _ := time.Parse(time.RFC3339, task.UpdatedAt)
	createdAt, _ := time.Parse(time.RFC3339, task.CreatedAt)
	if createdAt.IsZero() {
		createdAt = updatedAt
	}

	excerpt := SkillCandidateExcerpt{
		Path:      task.ID,
		Snippet:   truncateSnippet(task.Details, 1200),
		Author:    task.Owner,
		CreatedAt: updatedAt,
	}

	return SkillCandidate{
		Source:               SourceSelfHealResolved,
		SuggestedName:        skillSlug("handle-" + reasonSlug(reason)),
		SuggestedDescription: selfHealDescription(reason, detail, agent),
		Excerpts:             []SkillCandidateExcerpt{excerpt},
		RelatedWikiPaths:     s.relatedWikiPaths(ctx, reason, detail),
		SignalCount:          1,
		FirstSeenAt:          createdAt,
		LastSeenAt:           updatedAt,
	}
}

// relatedWikiPaths consults the broker's wiki index using the reason +
// detail tokens. Empty if the index is unavailable — the synthesizer will
// degrade gracefully.
func (s *SelfHealSignalScanner) relatedWikiPaths(ctx context.Context, reason, detail string) []string {
	idx := s.broker.WikiIndex()
	if idx == nil {
		// TODO(stage-b): when the wiki index is offline we cannot ground the
		// synthesizer with related context. The synthesizer must tolerate
		// an empty slice.
		return nil
	}
	query := strings.TrimSpace(reason + " " + detail)
	if query == "" {
		return nil
	}
	hits, err := idx.Search(ctx, query, 5)
	if err != nil {
		slog.Warn("self_heal_signal_search_failed", "err", err, "query", query)
		return nil
	}
	seen := map[string]bool{}
	var paths []string
	for _, h := range hits {
		key := h.Entity
		if key == "" {
			key = h.FactID
		}
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		paths = append(paths, key)
	}
	return paths
}

// parseSelfHealTitle returns (agentSlug, reasonHint). The reason hint is
// the trailing free-form fragment when the title carries one
// ("runtime failure", "blocked by capability_gap"). Both values may be
// empty if the title doesn't match the canonical layout.
func parseSelfHealTitle(title string) (string, string) {
	matches := selfHealTitleRE.FindStringSubmatch(strings.TrimSpace(title))
	if matches == nil {
		return "", ""
	}
	agent := matches[selfHealTitleRE.SubexpIndex("agent")]
	rest := strings.TrimSpace(matches[selfHealTitleRE.SubexpIndex("rest")])
	rest = strings.TrimPrefix(rest, "runtime failure")
	rest = strings.TrimPrefix(rest, "blocked by ")
	rest = strings.TrimSpace(rest)
	return agent, rest
}

// parseSelfHealReason extracts the trigger from the incident details,
// matching the "- Trigger: <reason>" line written by selfHealingTaskDetails.
func parseSelfHealReason(details string) string {
	matches := reasonRE.FindStringSubmatch(details)
	if matches == nil {
		return ""
	}
	return strings.TrimSpace(matches[reasonRE.SubexpIndex("reason")])
}

// parseSelfHealDetail returns the one-line "Detail:" field if present.
func parseSelfHealDetail(details string) string {
	matches := detailRE.FindStringSubmatch(details)
	if matches == nil {
		return ""
	}
	return strings.TrimSpace(matches[detailRE.SubexpIndex("detail")])
}

// reasonSlug normalises a reason (e.g. "capability_gap" or "Capability Gap")
// into a kebab fragment safe for skill slugs. Collapses runs of separators
// into a single dash and strips leading/trailing dashes.
func reasonSlug(reason string) string {
	cleaned := strings.ToLower(strings.TrimSpace(reason))
	// First pass: replace any non [a-z0-9] rune with a dash.
	var b strings.Builder
	for _, r := range cleaned {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		b.WriteRune('-')
	}
	// Second pass: collapse runs of dashes to one.
	parts := strings.Split(b.String(), "-")
	kept := parts[:0]
	for _, p := range parts {
		if p == "" {
			continue
		}
		kept = append(kept, p)
	}
	out := strings.Join(kept, "-")
	if out == "" {
		return "self-heal"
	}
	return out
}

// selfHealDescription builds a one-line description hint. The synthesizer
// may overwrite it after consulting the LLM.
func selfHealDescription(reason, detail, agent string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "an unknown blocker"
	}
	if detail = strings.TrimSpace(detail); detail != "" {
		return truncateSnippet("How to resolve when "+reason+" blocks an agent ("+detail+")", 200)
	}
	if agent != "" {
		return truncateSnippet("How to resolve when "+reason+" blocks @"+agent, 200)
	}
	return truncateSnippet("How to resolve when "+reason+" blocks an agent", 200)
}
