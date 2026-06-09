package team

import (
	"fmt"
	"strings"
	"sync"

	"github.com/nex-crm/wuphf/internal/packer"
)

// packer_adapter.go is the seam that wires the inbound context-packer
// (internal/packer) to the live broker. The packer is transport- and
// brain-agnostic; this file implements its BrainHandle / SnapshotValidator /
// InjectionSink interfaces over the OSS self-hosted broker (one office = one
// brain). Nex cloud provides a per-tenant adapter over the same interfaces.

// packerBrain adapts the broker to packer.BrainHandle. Every retrieval is
// task-scoped, and only human-vetted content leaves: the approved plan step, the
// task's TRUSTED learnings, and EXPLICITLY task-linked wiki articles. Nothing is
// driven by foreign intent text.
type packerBrain struct {
	b *Broker
}

// NewPackerBrain returns a packer.BrainHandle backed by this broker.
func (b *Broker) NewPackerBrain() packer.BrainHandle {
	return &packerBrain{b: b}
}

// PlanStep renders the human-approved plan step from the task's IssueDraftSpec.
// It deliberately does NOT fall back to the raw teamTask.Details: that free-form
// field is the untrusted task body the egress policy denies, never the approved
// plan. A task with no drafted spec yields an empty plan step.
func (p *packerBrain) PlanStep(taskID string) (string, error) {
	p.b.mu.Lock()
	defer p.b.mu.Unlock()
	task := p.b.findTaskByIDLocked(taskID)
	if task == nil {
		return "", fmt.Errorf("packer brain: task %q not found", taskID)
	}
	return renderPlanStep(task), nil
}

func renderPlanStep(task *teamTask) string {
	spec := task.IssueDraftSpec
	if spec == nil {
		return ""
	}
	var sb strings.Builder
	writePlanSection(&sb, "Goal", spec.Goal)
	writePlanSection(&sb, "Context", spec.Context)
	writePlanSection(&sb, "Approach", spec.Approach)
	writePlanSection(&sb, "Acceptance", spec.Acceptance)
	return strings.TrimSpace(sb.String())
}

func writePlanSection(sb *strings.Builder, label, body string) {
	if strings.TrimSpace(body) == "" {
		return
	}
	sb.WriteString(label)
	sb.WriteString(": ")
	sb.WriteString(strings.TrimSpace(body))
	sb.WriteString("\n")
}

// TaskLearnings returns the task's TRUSTED learnings only — promoted, human-
// vetted insights. Untrusted draft learnings are unvetted and never egress to a
// foreign bot. Results are exact-scoped to the task id (no fuzzy fallback).
func (p *packerBrain) TaskLearnings(taskID string, limit int) ([]packer.BrainItem, error) {
	log := p.b.TeamLearningLog()
	if log == nil {
		return nil, nil
	}
	trusted := true
	results, err := log.Search(LearningSearchFilters{
		TaskID:  taskID,
		Trusted: &trusted,
		Limit:   limit,
	})
	if err != nil {
		return nil, fmt.Errorf("packer brain learnings: %w", err)
	}
	items := make([]packer.BrainItem, 0, len(results))
	for _, r := range results {
		body := strings.TrimSpace(r.Insight)
		if body == "" {
			continue
		}
		items = append(items, packer.BrainItem{Ref: "learning:" + r.Key, Body: body})
	}
	return items, nil
}

// TaskWikiRefs resolves the task's EXPLICITLY linked wiki articles
// (teamTask.WikiRefs) to their bodies. It never runs a free WikiIndex.Search — a
// foreign bot only ever sees articles a human linked to the task. A missing or
// invalid linked article is skipped, not fatal.
func (p *packerBrain) TaskWikiRefs(taskID string) ([]packer.BrainItem, error) {
	p.b.mu.Lock()
	task := p.b.findTaskByIDLocked(taskID)
	var refs []string
	if task != nil {
		refs = append(refs, task.WikiRefs...)
	}
	worker := p.b.wikiWorker
	p.b.mu.Unlock()

	if worker == nil || len(refs) == 0 {
		return nil, nil
	}
	items := make([]packer.BrainItem, 0, len(refs))
	for _, ref := range refs {
		raw, err := worker.ReadArticle(ref)
		if err != nil {
			continue
		}
		body := strings.TrimSpace(stripFrontmatter(string(raw)))
		if body == "" {
			continue
		}
		items = append(items, packer.BrainItem{Ref: "wiki:" + ref, Body: body})
	}
	return items, nil
}

// Roster returns brief who-is-who lines for the task: its owner and assigned
// reviewers, resolved to office-member slug + role. Display names are not a
// trust input anywhere, but a roster line is advisory context only.
func (p *packerBrain) Roster(taskID string) ([]packer.BrainItem, error) {
	p.b.mu.Lock()
	defer p.b.mu.Unlock()
	task := p.b.findTaskByIDLocked(taskID)
	if task == nil {
		return nil, nil
	}
	seen := make(map[string]struct{})
	var items []packer.BrainItem
	add := func(slug string) {
		slug = strings.TrimSpace(slug)
		if slug == "" {
			return
		}
		if _, ok := seen[slug]; ok {
			return
		}
		seen[slug] = struct{}{}
		m := p.b.findMemberLocked(slug)
		if m == nil {
			return
		}
		line := m.Slug
		if strings.TrimSpace(m.Role) != "" {
			line += " — " + strings.TrimSpace(m.Role)
		}
		items = append(items, packer.BrainItem{Ref: "roster:" + m.Slug, Body: line})
	}
	add(task.Owner)
	for _, r := range task.Reviewers {
		add(r)
	}
	return items, nil
}

// packerSnapshotValidator re-checks, at delivery time, that the task is still in
// the state the delegation was built against. v1 validates task freshness
// (UpdatedAt); per-bot trust-tier liveness is enforced by the bridge's profile
// store plus the packer's own snapshotMatches binding (which refuses a delivery
// whose packed trust tier differs from the delivering request).
type packerSnapshotValidator struct {
	b *Broker
}

// NewPackerSnapshotValidator returns a packer.SnapshotValidator over this broker.
func (b *Broker) NewPackerSnapshotValidator() packer.SnapshotValidator {
	return &packerSnapshotValidator{b: b}
}

// Validate returns a non-nil error if the task no longer exists or has been
// edited since the delegation was packed.
func (v *packerSnapshotValidator) Validate(req packer.ContextRequest) error {
	v.b.mu.Lock()
	defer v.b.mu.Unlock()
	task := v.b.findTaskByIDLocked(req.TaskID)
	if task == nil {
		return fmt.Errorf("task %q no longer exists", req.TaskID)
	}
	if task.UpdatedAt != req.TaskUpdatedAt {
		return fmt.Errorf("task edited since pack (updated_at %q != %q)", task.UpdatedAt, req.TaskUpdatedAt)
	}
	return nil
}

// PackerInjectionSink is an append-only egress audit store for InjectionRecords.
// It keys current state by idempotency key (for the packer's dedup) and keeps the
// full transition history. Lookup/Write are individually mutex-guarded; the
// bridge serializes a whole Deliver under the broker mutex, which satisfies the
// cross-call atomicity the packer's Deliver assumes. A persistent on-disk store
// is a follow-up.
type PackerInjectionSink struct {
	mu   sync.Mutex
	recs map[string]packer.InjectionRecord
	hist []packer.InjectionRecord
}

// NewPackerInjectionSink returns an empty in-memory sink.
func NewPackerInjectionSink() *PackerInjectionSink {
	return &PackerInjectionSink{recs: make(map[string]packer.InjectionRecord)}
}

// Lookup returns the current record for an idempotency key.
func (s *PackerInjectionSink) Lookup(key string) (packer.InjectionRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.recs[key]
	return r, ok
}

// Write upserts the current record and appends to the audit history.
func (s *PackerInjectionSink) Write(rec packer.InjectionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recs[rec.IdempotencyKey] = rec
	s.hist = append(s.hist, rec)
	return nil
}

// History returns a copy of the append-only audit log.
func (s *PackerInjectionSink) History() []packer.InjectionRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]packer.InjectionRecord, len(s.hist))
	copy(out, s.hist)
	return out
}
