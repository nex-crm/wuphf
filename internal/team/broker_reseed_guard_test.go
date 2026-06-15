package team

import (
	"os"
	"path/filepath"
	"testing"
)

// TestHasRealOfficeStateLocked encodes the subtlety the data-loss guard turns
// on: every fresh broker carries the "Backup & Migration" SYSTEM task
// (task-general), so task presence alone cannot mean "real office". Only a
// non-system task does.
func TestHasRealOfficeStateLocked(t *testing.T) {
	b := newTestBroker(t)

	// Fresh broker: only the system task exists → not a real office.
	b.mu.Lock()
	hasSystemTask := false
	for i := range b.tasks {
		if b.tasks[i].System {
			hasSystemTask = true
		}
	}
	real := b.hasRealOfficeStateLocked()
	b.mu.Unlock()
	if !hasSystemTask {
		t.Fatal("precondition: fresh broker should carry the Backup & Migration system task")
	}
	if real {
		t.Fatal("a broker holding only the system task must NOT count as a real office")
	}

	// Add a real (non-system) task → now it is a real office.
	b.mu.Lock()
	b.tasks = append(b.tasks, teamTask{ID: "real-1", Title: "Ship the thing", Channel: "general"})
	real = b.hasRealOfficeStateLocked()
	b.mu.Unlock()
	if !real {
		t.Fatal("a broker with a non-system task must count as a real office")
	}
}

// TestOnboardingComplete_AdoptsExistingOffice is the incident regression: a
// clean-env restart can clear the onboarded marker while the on-disk office
// survives, the wizard re-appears, and completing it used to reseed a fresh
// default office — wiping the real channels and tasks. The guard must adopt the
// existing office instead. Pre-fix, seedFromBlueprintLocked wiped b.tasks and
// rebuilt members/channels from the blueprint, so "real-1" and the custom
// channel vanished.
func TestOnboardingComplete_AdoptsExistingOffice(t *testing.T) {
	// Keep wiki/getting-started materialization inside a throwaway home so the
	// adopt path's best-effort calls no-op against a temp dir.
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())

	b := newTestBroker(t)
	b.mu.Lock()
	b.tasks = append(b.tasks, teamTask{ID: "real-1", Title: "Ship the thing", Channel: "general"})
	b.channels = append(b.channels, teamChannel{Slug: "slack-wuphf-office", Name: "wuphf-office"})
	b.messages = append(b.messages, channelMessage{ID: "m-1", Channel: "slack-wuphf-office", From: "you", Content: "real history"})
	b.mu.Unlock()

	// Simulate the wizard re-appearing and the user completing it (synthesized
	// blueprint path: blueprintID="", skipTask=true).
	if err := b.onboardingCompleteFn("", true, "", nil, ""); err != nil {
		t.Fatalf("onboardingCompleteFn: %v", err)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	foundTask := false
	for i := range b.tasks {
		if b.tasks[i].ID == "real-1" {
			foundTask = true
		}
	}
	if !foundTask {
		t.Fatal("real task was wiped by an onboarding reseed — data-loss guard failed")
	}
	if b.findChannelLocked("slack-wuphf-office") == nil {
		t.Fatal("custom channel was wiped by an onboarding reseed — data-loss guard failed")
	}
	foundMsg := false
	for i := range b.messages {
		if b.messages[i].ID == "m-1" {
			foundMsg = true
		}
	}
	if !foundMsg {
		t.Fatal("channel history was wiped by an onboarding reseed — data-loss guard failed")
	}
}

// TestWriteBrokerState_RemoveBranch_PreservesSnapshot covers the secondary
// hardening: when a save takes the remove branch (the broker reverted to
// default in-memory), the .last-good recovery snapshot of the last REAL office
// must survive. A genuine Reset() removes the snapshot itself; an accidental
// default-revert must never destroy both the live state and its only backup at
// the same instant. This exercises writeBrokerState's remove branch directly.
func TestWriteBrokerState_RemoveBranch_PreservesSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broker-state.json")
	b := NewBrokerAt(path)
	snapshotPath := b.stateSnapshotPath()

	// Lay down a primary state file and a real-office .last-good snapshot.
	if err := os.WriteFile(path, []byte(`{"primary":true}`), 0o600); err != nil {
		t.Fatalf("seed primary: %v", err)
	}
	if err := os.WriteFile(snapshotPath, []byte(`{"real_office":true}`), 0o600); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}

	// A remove-branch save (in-memory reverted to default).
	write := brokerStateWrite{
		seq:          b.stateWriteSeq.Add(1),
		path:         path,
		snapshotPath: snapshotPath,
		remove:       true,
	}
	if err := b.writeBrokerState(write); err != nil {
		t.Fatalf("writeBrokerState: %v", err)
	}

	// Primary is cleared (stale default file should not linger)...
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("primary state file should be removed on the remove branch; stat err=%v", err)
	}
	// ...but the recovery snapshot of the real office MUST survive.
	if _, err := os.Stat(snapshotPath); err != nil {
		t.Fatalf("last-good snapshot was destroyed by a remove-branch save — data-loss guard failed: %v", err)
	}
}
