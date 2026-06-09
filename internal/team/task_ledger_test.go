package team

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAppendTaskLedgerEntryCapsAndPersists(t *testing.T) {
	b := newVerificationTestBroker(t)
	task, _, err := b.EnsureTask("general", "Long-running build", "many turns", "eng", "ceo", "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < taskLedgerMaxEntries+5; i++ {
		b.AppendTaskLedgerEntry(task.ID, TaskLedgerEntry{Agent: "eng", Outcome: "ok"})
	}
	got := b.TaskByID(task.ID)
	if len(got.Ledger) != taskLedgerMaxEntries {
		t.Fatalf("want cap %d; got %d", taskLedgerMaxEntries, len(got.Ledger))
	}
}

func TestRecordTaskLedgerEntryAssemblesFromBrokerFacts(t *testing.T) {
	b := newVerificationTestBroker(t)
	task, _, err := b.EnsureTask("general", "Instrumented work", "watch the journal", "eng", "ceo", "")
	if err != nil {
		t.Fatal(err)
	}
	l := launcherForBrokerFixture(b)
	startedAt := time.Now().UTC().Add(-1 * time.Second)

	if _, err := b.PostMessage("eng", task.Channel, "Tried the flag approach; tests still red on auth_test.go", nil, ""); err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	b.appendActionLocked("task_updated", "office", task.Channel, "eng", "marked blocked on flaky fixture", task.ID)
	b.mu.Unlock()

	l.recordTaskLedgerEntry("eng", headlessCodexTurn{TaskID: task.ID, Channel: task.Channel}, startedAt, errors.New("turn timed out after 20m"))

	got := b.TaskByID(task.ID)
	if len(got.Ledger) != 1 {
		t.Fatalf("want 1 entry; got %d", len(got.Ledger))
	}
	e := got.Ledger[0]
	if e.Agent != "eng" || !strings.Contains(e.Outcome, "timed out") {
		t.Fatalf("entry agent/outcome wrong: %+v", e)
	}
	if !strings.Contains(e.Said, "flag approach") {
		t.Fatalf("entry must carry the agent's last message; got %q", e.Said)
	}
	if len(e.Actions) == 0 || !strings.Contains(e.Actions[0], "flaky fixture") {
		t.Fatalf("entry must carry the task mutations; got %v", e.Actions)
	}

	packet := l.notifyCtx().BuildTaskExecutionPacket("eng", officeActionLog{Actor: "ceo"}, *got, "Next attempt.")
	if !strings.Contains(packet, "TASK JOURNAL") || !strings.Contains(packet, "flag approach") {
		t.Fatalf("packet must carry the journal; got:\n%s", packet)
	}
}

func TestRecordTaskLedgerEntrySkipsTasklessTurns(t *testing.T) {
	b := newVerificationTestBroker(t)
	l := launcherForBrokerFixture(b)
	l.recordTaskLedgerEntry("eng", headlessCodexTurn{Channel: "general"}, time.Now(), nil)
	// Nothing to assert on a specific task — the contract is simply that a
	// task-less turn must not panic or write anywhere.
}
