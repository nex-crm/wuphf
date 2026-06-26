package team

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func bookendTestTask() teamTask {
	return teamTask{
		ID:      "OFFICE-7",
		Title:   "Publish the onboarding teardown brief",
		Channel: "general",
		Owner:   "eng",
		Definition: &TaskDefinition{
			Goal:            "Ship the onboarding teardown brief to the wiki",
			Deliverables:    []TaskDeliverable{{Name: "teardown brief", Format: "markdown in the wiki"}},
			SuccessCriteria: []string{"Brief published to the wiki"},
		},
		Artifact: "team/briefs/onboarding-teardown.md",
		VerificationResult: &TaskVerificationResult{
			Kind: "command", Pass: true, Detail: "exit 0",
		},
		Ledger: []TaskLedgerEntry{
			{Agent: "eng", At: "2026-06-10T00:00:00Z", Outcome: "ok", Said: "Drafted the brief outline."},
		},
	}
}

func TestRenderTaskNotebookPreSection(t *testing.T) {
	task := bookendTestTask()
	body := renderTaskNotebookPreSection(task, "eng", "RELEVANT TEAM KNOWLEDGE:\n- [learning:l-1] use the template", []string{"learning:l-1"})
	for _, want := range []string{
		"pre-task research",
		"- task: OFFICE-7",
		"## Definition",
		"Ship the onboarding teardown brief to the wiki",
		"## Retrieved context",
		"learning:l-1",
		"context ids: learning:l-1",
		"## Research",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("pre section missing %q:\n%s", want, body)
		}
	}
	empty := renderTaskNotebookPreSection(task, "eng", "", nil)
	if !strings.Contains(empty, "- none matched this task") {
		t.Fatalf("pre section with no knowledge must say so:\n%s", empty)
	}
}

func TestRenderTaskNotebookPostSection(t *testing.T) {
	task := bookendTestTask()
	body := renderTaskNotebookPostSection(task, "Verified outcome: brief shipped.")
	for _, want := range []string{
		"## Post-task",
		taskNotebookPostMarker,
		"[team/briefs/onboarding-teardown.md](team/briefs/onboarding-teardown.md)",
		"verified (command): exit 0",
		"### Learnings distilled",
		"Verified outcome: brief shipped.",
		"### Ledger highlights",
		"Drafted the brief outline.",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("post section missing %q:\n%s", want, body)
		}
	}
}

// TestAppendTaskNotebookPostBookendIdempotent pins the replay contract:
// distillation replays append the post-task section exactly once.
func TestAppendTaskNotebookPostBookendIdempotent(t *testing.T) {
	worker, repo, _, cancel := newStartedNotebookWorker(t)
	t.Cleanup(cancel)
	task := bookendTestTask()
	ctx := context.Background()

	appendTaskNotebookPostBookend(ctx, worker, task, "Verified outcome: brief shipped.")
	appendTaskNotebookPostBookend(ctx, worker, task, "Verified outcome: brief shipped.")

	raw, err := os.ReadFile(filepath.Join(repo.Root(), "agents", "eng", "notebook", task.ID+".md"))
	if err != nil {
		t.Fatalf("read note: %v", err)
	}
	if got := strings.Count(string(raw), "## Post-task"); got != 1 {
		t.Fatalf("post-task section appended %d times, want 1:\n%s", got, raw)
	}
}

// TestAppendTaskNotebookPostBookendGates pins the skip conditions: no
// Definition, system tasks, and invalid owners write nothing.
func TestAppendTaskNotebookPostBookendGates(t *testing.T) {
	worker, repo, _, cancel := newStartedNotebookWorker(t)
	t.Cleanup(cancel)
	ctx := context.Background()

	noDef := bookendTestTask()
	noDef.Definition = nil
	appendTaskNotebookPostBookend(ctx, worker, noDef, "x")

	system := bookendTestTask()
	system.System = true
	appendTaskNotebookPostBookend(ctx, worker, system, "x")

	badOwner := bookendTestTask()
	badOwner.Owner = "../evil"
	appendTaskNotebookPostBookend(ctx, worker, badOwner, "x")

	if _, err := os.Stat(filepath.Join(repo.Root(), "agents", "eng", "notebook")); err == nil {
		entries, _ := os.ReadDir(filepath.Join(repo.Root(), "agents", "eng", "notebook"))
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".md") {
				t.Fatalf("gated bookend wrote %s", e.Name())
			}
		}
	}
}

// TestWriteTaskNotebookPreBookendCreateOnly pins the race contract: an
// existing agent-authored note is never overwritten by the bookend.
func TestWriteTaskNotebookPreBookendCreateOnly(t *testing.T) {
	worker, repo, _, cancel := newStartedNotebookWorker(t)
	t.Cleanup(cancel)

	b := NewBrokerAt(filepath.Join(t.TempDir(), "broker-state.json"))
	b.mu.Lock()
	b.wikiWorker = worker
	task := bookendTestTask()
	task.LifecycleState = LifecycleStateRunning
	b.tasks = append(b.tasks, task)
	b.mu.Unlock()
	l := launcherForBrokerFixture(b)

	// The agent got there first.
	agentNote := "# my own research\n\nagent-authored content\n"
	if _, _, err := worker.NotebookWrite(context.Background(), "eng",
		taskNotebookEntryPath("eng", task.ID), agentNote, "create", "agent note"); err != nil {
		t.Fatalf("seed agent note: %v", err)
	}

	l.writeTaskNotebookPreBookend("eng", task.ID)

	raw, err := os.ReadFile(filepath.Join(repo.Root(), "agents", "eng", "notebook", task.ID+".md"))
	if err != nil {
		t.Fatalf("read note: %v", err)
	}
	if string(raw) != agentNote {
		t.Fatalf("bookend overwrote the agent's note:\n%s", raw)
	}
}

// TestQueueTaskNotebookPreBookendDedupes pins once-per-(agent, task): the
// second enqueue for the same pair never queues a second write goroutine.
func TestQueueTaskNotebookPreBookendDedupes(t *testing.T) {
	l := &Launcher{} // nil broker → the queued goroutine is never spawned
	l.queueTaskNotebookPreBookend("eng", "OFFICE-9")
	if l.notebookBookendSeen != nil {
		t.Fatal("nil-broker launcher must not record bookend state")
	}

	b := NewBrokerAt(filepath.Join(t.TempDir(), "broker-state.json"))
	l = launcherForBrokerFixture(b)
	l.queueTaskNotebookPreBookend("eng", "OFFICE-9")
	l.queueTaskNotebookPreBookend("eng", "OFFICE-9")
	l.queueTaskNotebookPreBookend("eng", "../traversal")
	l.notebookBookendMu.Lock()
	seen := len(l.notebookBookendSeen)
	l.notebookBookendMu.Unlock()
	if seen != 1 {
		t.Fatalf("expected exactly 1 deduped bookend key, got %d", seen)
	}
}
