package team

// packer_adapter_test.go verifies the broker-backed packer brain seam: the
// approved plan step (never raw Details), trusted task-scoped learnings only,
// explicitly task-linked wiki bodies, roster lines, the snapshot validator, and
// the injection sink.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/packer"
)

func newAdapterFixture(t *testing.T) (*Broker, func()) {
	t.Helper()
	repo, worker, log, teardown := newLearningFixture(t)
	b := newTestBroker(t)
	b.wikiWorker = worker
	b.SetTeamLearningLog(log)
	b.members = []officeMember{
		{Slug: "alice", Name: "Alice", Role: "RevOps"},
		{Slug: "pam", Name: "Pam", Role: "Librarian"},
	}

	// A linked wiki article.
	art := filepath.Join(repo.Root(), "team", "playbooks", "billing.md")
	if err := os.MkdirAll(filepath.Dir(art), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(art, []byte("---\ntitle: Billing\n---\nReconcile invoices monthly."), 0o644); err != nil {
		t.Fatalf("write article: %v", err)
	}

	// Trust is derived from Source: UserStated => trusted, Observed => untrusted.
	mustAppendLearning(t, log, "billing-trusted", "task-1", LearningSourceUserStated)
	mustAppendLearning(t, log, "billing-untrusted", "task-1", LearningSourceObserved)
	mustAppendLearning(t, log, "other-task-trusted", "task-2", LearningSourceUserStated)

	b.tasks = append(b.tasks, teamTask{
		ID:        "task-1",
		Title:     "Reconcile",
		Owner:     "alice",
		Reviewers: []string{"pam"},
		UpdatedAt: "2026-06-09T00:00:00Z",
		WikiRefs:  []string{"team/playbooks/billing.md"},
		Definition: &TaskDefinition{
			Goal:            "Reconcile June invoices",
			Deliverables:    []TaskDeliverable{{Name: "Reconciliation report", Format: "markdown"}},
			SuccessCriteria: []string{"every invoice matched to the ledger"},
			DefinedAt:       "2026-06-09T00:00:00Z",
		},
	})
	return b, teardown
}

func mustAppendLearning(t *testing.T, log *LearningLog, key, taskID string, source LearningSource) {
	t.Helper()
	if _, err := log.Append(context.Background(), LearningRecord{
		Type:       LearningTypePitfall,
		Key:        key,
		Insight:    "insight for " + key,
		Confidence: 8,
		Source:     source,
		Scope:      "repo",
		CreatedBy:  "codex",
		TaskID:     taskID,
	}); err != nil {
		t.Fatalf("append %s: %v", key, err)
	}
}

// task1 locates the fixture's task-1 (the test broker seeds other tasks, so its
// index is not stable).
func task1(t *testing.T, b *Broker) *teamTask {
	t.Helper()
	for i := range b.tasks {
		if b.tasks[i].ID == "task-1" {
			return &b.tasks[i]
		}
	}
	t.Fatal("task-1 not found in broker")
	return nil
}

func TestPackerBrain_PlanStepUsesApprovedSpecNotRawDetails(t *testing.T) {
	b, teardown := newAdapterFixture(t)
	defer teardown()
	// Raw Details must NEVER surface as the plan step.
	task1(t, b).Details = "PASTED SECRET BLOB do-not-export"
	brain := b.NewPackerBrain()

	plan, err := brain.PlanStep("task-1")
	if err != nil {
		t.Fatalf("PlanStep: %v", err)
	}
	if plan == "" {
		t.Fatal("expected a rendered plan step from the spec")
	}
	if want := "Reconcile June invoices"; !strings.Contains(plan, want) {
		t.Fatalf("plan missing %q: %q", want, plan)
	}
	if strings.Contains(plan, "do-not-export") {
		t.Fatalf("raw Details leaked into the plan step: %q", plan)
	}
}

func TestPackerBrain_PlanStepEmptyWhenNoSpec(t *testing.T) {
	b, teardown := newAdapterFixture(t)
	defer teardown()
	tk := task1(t, b)
	tk.Definition = nil
	tk.Details = "raw body only"
	plan, err := b.NewPackerBrain().PlanStep("task-1")
	if err != nil {
		t.Fatalf("PlanStep: %v", err)
	}
	if plan != "" {
		t.Fatalf("expected empty plan when no spec, got %q", plan)
	}
}

func TestPackerBrain_TaskLearningsTrustedAndScoped(t *testing.T) {
	b, teardown := newAdapterFixture(t)
	defer teardown()
	got, err := b.NewPackerBrain().TaskLearnings("task-1", 10)
	if err != nil {
		t.Fatalf("TaskLearnings: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want exactly 1 trusted task-scoped learning, got %d: %+v", len(got), got)
	}
	if got[0].Ref != "learning:billing-trusted" {
		t.Fatalf("wrong learning: %q", got[0].Ref)
	}
}

func TestPackerBrain_TaskWikiRefsResolveLinkedArticles(t *testing.T) {
	b, teardown := newAdapterFixture(t)
	defer teardown()
	got, err := b.NewPackerBrain().TaskWikiRefs("task-1")
	if err != nil {
		t.Fatalf("TaskWikiRefs: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 linked wiki article, got %d: %+v", len(got), got)
	}
	if !strings.Contains(got[0].Body, "Reconcile invoices monthly") {
		t.Fatalf("wiki body missing content: %q", got[0].Body)
	}
	if strings.Contains(got[0].Body, "title: Billing") {
		t.Fatalf("frontmatter should be stripped: %q", got[0].Body)
	}
}

func TestPackerBrain_RosterFromOwnerAndReviewers(t *testing.T) {
	b, teardown := newAdapterFixture(t)
	defer teardown()
	got, err := b.NewPackerBrain().Roster("task-1")
	if err != nil {
		t.Fatalf("Roster: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want owner + reviewer roster lines, got %d: %+v", len(got), got)
	}
	if !strings.Contains(got[0].Body, "alice") || !strings.Contains(got[0].Body, "RevOps") {
		t.Fatalf("owner roster line wrong: %q", got[0].Body)
	}
}

func TestPackerSnapshotValidator(t *testing.T) {
	b, teardown := newAdapterFixture(t)
	defer teardown()
	val := b.NewPackerSnapshotValidator()

	ok := packer.ContextRequest{TaskID: "task-1", TaskUpdatedAt: "2026-06-09T00:00:00Z"}
	if err := val.Validate(ok); err != nil {
		t.Fatalf("fresh snapshot should validate: %v", err)
	}
	stale := packer.ContextRequest{TaskID: "task-1", TaskUpdatedAt: "2020-01-01T00:00:00Z"}
	if err := val.Validate(stale); err == nil {
		t.Fatal("edited task should fail validation")
	}
	missing := packer.ContextRequest{TaskID: "nope", TaskUpdatedAt: "x"}
	if err := val.Validate(missing); err == nil {
		t.Fatal("missing task should fail validation")
	}
}

func TestPackerInjectionSink(t *testing.T) {
	s := NewPackerInjectionSink()
	if _, ok := s.Lookup("k"); ok {
		t.Fatal("empty sink should miss")
	}
	_ = s.Write(packer.InjectionRecord{IdempotencyKey: "k", Status: packer.DeliveryPending})
	_ = s.Write(packer.InjectionRecord{IdempotencyKey: "k", Status: packer.DeliverySent, MessageTS: "1.2"})
	cur, ok := s.Lookup("k")
	if !ok || cur.Status != packer.DeliverySent || cur.MessageTS != "1.2" {
		t.Fatalf("lookup should return latest sent record: %+v", cur)
	}
	if len(s.History()) != 2 {
		t.Fatalf("history should keep both transitions, got %d", len(s.History()))
	}
}
