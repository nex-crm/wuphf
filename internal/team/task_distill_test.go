package team

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func newDistillTestBroker(t *testing.T) *Broker {
	t.Helper()
	b := newVerificationTestBroker(t)
	root := filepath.Join(t.TempDir(), "wiki")
	repo := NewRepoAt(root, filepath.Join(t.TempDir(), "wiki.bak"))
	if err := repo.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	worker := NewWikiWorker(repo, b)
	ctx, cancel := context.WithCancel(context.Background())
	worker.Start(ctx)
	t.Cleanup(func() {
		cancel()
		<-worker.Done()
	})
	b.mu.Lock()
	b.wikiWorker = worker
	b.mu.Unlock()
	b.ensureTeamLearningLog()
	return b
}

func TestDistillCompletedTaskWritesVerifiedLearning(t *testing.T) {
	b := newDistillTestBroker(t)
	id := createVerifiedTask(t, b, "exit 0")
	if _, err := b.MutateTask(TaskPostRequest{Action: "complete", ID: id, Channel: "general", CreatedBy: "eng"}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.MutateTask(TaskPostRequest{Action: "approve", ID: id, Channel: "general", CreatedBy: "ceo"}); err != nil {
		t.Fatal(err)
	}
	// The mutation queues distillation async; call the worker directly for
	// a deterministic assertion (idempotency makes the double-run safe).
	b.distillCompletedTask(id)

	recs, err := b.TeamLearningLog().Search(LearningSearchFilters{Limit: MaxLearningLimit})
	if err != nil {
		t.Fatal(err)
	}
	var hit *LearningSearchResult
	for i := range recs {
		if recs[i].TaskID == id {
			hit = &recs[i]
			break
		}
	}
	if hit == nil {
		t.Fatalf("verified done task must distill into a learning; got %d records, none for task %s", len(recs), id)
	}
	if !hit.Trusted || hit.Source != "execution" || !strings.Contains(hit.Insight, "Verified outcome") {
		t.Fatalf("distilled learning shape wrong: %+v", hit.LearningRecord)
	}

	// Idempotent: a second distill (approve replay, watchdog) is a no-op.
	b.distillCompletedTask(id)
	recs2, _ := b.TeamLearningLog().Search(LearningSearchFilters{Limit: MaxLearningLimit})
	count := 0
	for _, r := range recs2 {
		if r.TaskID == id {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("distillation must be idempotent per task; got %d records", count)
	}
}

func TestDistillSkipsUnverifiedDone(t *testing.T) {
	b := newDistillTestBroker(t)
	task, _, err := b.EnsureTask("general", "Unverified chore", "no definition of done", "eng", "ceo", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.MutateTask(TaskPostRequest{Action: "complete", ID: task.ID, Channel: "general", CreatedBy: "eng"}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.MutateTask(TaskPostRequest{Action: "approve", ID: task.ID, Channel: "general", CreatedBy: "ceo"}); err != nil {
		t.Fatal(err)
	}
	b.distillCompletedTask(task.ID)
	recs, _ := b.TeamLearningLog().Search(LearningSearchFilters{Limit: MaxLearningLimit})
	for _, r := range recs {
		if r.TaskID == task.ID {
			t.Fatalf("unverified done must NOT auto-distill; got %+v", r.LearningRecord)
		}
	}
}
