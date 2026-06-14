package team

// Regression test for F1.b (ten-out-of-ten Wave F): GET /tasks follows
// copy-under-lock-then-serialize — ListTasks snapshots the board under b.mu
// and the HTTP handler JSON-encodes the snapshot AFTER the lock is released,
// so a large board never holds the broker lock across serialization. That is
// only sound if the snapshot is a DEEP copy: with a shallow struct copy the
// returned tasks still alias Ledger/Reviewers/pointer fields, and an
// in-place mutation (AppendTaskLedgerEntry et al) races the encoder. This
// test runs the marshal concurrently with locked in-place mutations; the
// -race suite is the oracle.

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestListTasksSnapshotIsSafeToMarshalOutsideTheLock(t *testing.T) {
	b := NewBrokerAt(filepath.Join(t.TempDir(), "state.json"))
	b.mu.Lock()
	b.channels = []teamChannel{{Slug: "general", Name: "general", Members: []string{"human", "ceo", "eng"}}}
	now := time.Now().UTC().Format(time.RFC3339)
	for i := 0; i < 200; i++ {
		b.tasks = append(b.tasks, teamTask{
			ID:             fmt.Sprintf("OFFICE-%d", i+1),
			Channel:        "general",
			Title:          fmt.Sprintf("board fixture %d", i+1),
			Owner:          "eng",
			status:         "in_progress",
			CreatedBy:      "ceo",
			LifecycleState: LifecycleStateRunning,
			Reviewers:      []string{"ceo"},
			DependsOn:      []string{"OFFICE-0"},
			Ledger: []TaskLedgerEntry{{
				Agent: "eng", At: now, Outcome: "ok",
				Actions: []string{"task_updated"},
			}},
			HumanNotePending: &TaskHumanNote{From: "human", Body: "note", At: now},
			CreatedAt:        now,
			UpdatedAt:        now,
		})
	}
	b.mu.Unlock()

	result, err := b.ListTasks(TaskListRequest{Channel: "general"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tasks) != 200 {
		t.Fatalf("expected 200 tasks, got %d", len(result.Tasks))
	}

	var wg sync.WaitGroup
	wg.Add(2)
	// Writer: in-place mutations under b.mu — exactly what
	// AppendTaskLedgerEntry / reviewer routing / note stamping do while a
	// GET response is being serialized.
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			b.mu.Lock()
			for j := range b.tasks {
				task := &b.tasks[j]
				// Skip broker-seeded tasks (e.g. the system task) that
				// lack the fixture's ledger/reviewer shape.
				if len(task.Ledger) == 0 || len(task.Reviewers) == 0 || task.HumanNotePending == nil {
					continue
				}
				task.Ledger[0].Outcome = fmt.Sprintf("mutated-%d", i)
				task.Ledger[0].Actions[0] = fmt.Sprintf("action-%d", i)
				task.Reviewers[0] = fmt.Sprintf("rev-%d", i)
				task.HumanNotePending.Body = fmt.Sprintf("note-%d", i)
			}
			b.mu.Unlock()
		}
	}()
	// Reader: serialize the snapshot OUTSIDE the lock, as the handler does.
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			if _, err := json.Marshal(result); err != nil {
				t.Errorf("marshal: %v", err)
				return
			}
		}
	}()
	wg.Wait()
}
