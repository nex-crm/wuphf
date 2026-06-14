package team

import (
	"encoding/json"
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

// TestRecordTaskLedgerEntryCarriesContextUsed pins the B4 transparency
// contract: the packet-build manifest stamped on the turn reaches the
// ledger entry verbatim and survives the JSON wire (additive field).
func TestRecordTaskLedgerEntryCarriesContextUsed(t *testing.T) {
	b := newVerificationTestBroker(t)
	task, _, err := b.EnsureTask("general", "Context audit work", "track what was injected", "eng", "ceo", "")
	if err != nil {
		t.Fatal(err)
	}
	l := launcherForBrokerFixture(b)
	manifest := []string{"learning:l-1", "wiki:companies/acme", "upstream:OFFICE-2"}
	l.recordTaskLedgerEntry("eng", headlessCodexTurn{TaskID: task.ID, Channel: task.Channel, ContextUsed: manifest}, time.Now().UTC(), nil)

	got := b.TaskByID(task.ID)
	if len(got.Ledger) != 1 {
		t.Fatalf("want 1 entry; got %d", len(got.Ledger))
	}
	if strings.Join(got.Ledger[0].ContextUsed, ",") != strings.Join(manifest, ",") {
		t.Fatalf("context manifest lost: %v", got.Ledger[0].ContextUsed)
	}

	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(blob), `"context_used"`) {
		t.Fatalf("ledger entry wire must carry context_used: %s", blob)
	}
	var roundTripped teamTask
	if err := json.Unmarshal(blob, &roundTripped); err != nil {
		t.Fatal(err)
	}
	if strings.Join(roundTripped.Ledger[0].ContextUsed, ",") != strings.Join(manifest, ",") {
		t.Fatalf("context manifest lost on round-trip: %v", roundTripped.Ledger[0].ContextUsed)
	}
}

// TestIssueActivityCarriesTurnEvents pins the Activity-rail surface: every
// settled turn appears as a kind="turn" event carrying its context manifest.
func TestIssueActivityCarriesTurnEvents(t *testing.T) {
	b := newVerificationTestBroker(t)
	task, _, err := b.EnsureTask("general", "Activity rail work", "surface the turns", "eng", "ceo", "")
	if err != nil {
		t.Fatal(err)
	}
	b.AppendTaskLedgerEntry(task.ID, TaskLedgerEntry{
		Agent: "eng", Outcome: "ok", Said: "shipped the slice",
		ContextUsed: []string{"learning:l-9", "wiki:people/sam"},
	})
	b.mu.Lock()
	events := b.collectIssueActivityLocked(task.ID)
	b.mu.Unlock()
	var turn *IssueActivityEvent
	for i := range events {
		if events[i].Kind == IssueActivityKindTurn {
			turn = &events[i]
		}
	}
	if turn == nil {
		t.Fatalf("no turn event in activity feed: %+v", events)
	}
	if turn.Actor != "eng" || turn.Detail != "shipped the slice" {
		t.Fatalf("turn event shape wrong: %+v", turn)
	}
	if strings.Join(turn.ContextUsed, ",") != "learning:l-9,wiki:people/sam" {
		t.Fatalf("turn event lost the context manifest: %v", turn.ContextUsed)
	}
}
