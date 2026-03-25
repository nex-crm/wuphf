package agent_test

import (
	"os"
	"testing"

	"github.com/nex-crm/wuphf/internal/agent"
)

func newTempStore(t *testing.T) *agent.SessionStore {
	t.Helper()
	dir, err := os.MkdirTemp("", "nex-session-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return agent.NewSessionStoreAt(dir)
}

func TestSessionCreateAppendRead(t *testing.T) {
	store := newTempStore(t)

	id, err := store.Create("planner")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty session ID")
	}

	e1, err := store.Append(id, agent.SessionEntry{Type: "user", Content: "hello"})
	if err != nil {
		t.Fatalf("Append user: %v", err)
	}
	e2, err := store.Append(id, agent.SessionEntry{Type: "assistant", Content: "world"})
	if err != nil {
		t.Fatalf("Append assistant: %v", err)
	}

	entries, err := store.GetHistory(id, 0, "")
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].ID != e1.ID || entries[0].Content != "hello" {
		t.Errorf("entry[0] mismatch: %+v", entries[0])
	}
	if entries[1].ID != e2.ID || entries[1].Content != "world" {
		t.Errorf("entry[1] mismatch: %+v", entries[1])
	}
}

func TestSessionGetHistoryLimit(t *testing.T) {
	store := newTempStore(t)
	id, _ := store.Create("planner")

	for i := 0; i < 5; i++ {
		store.Append(id, agent.SessionEntry{Type: "user", Content: "msg"})
	}

	entries, err := store.GetHistory(id, 3, "")
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries with limit, got %d", len(entries))
	}
}

func TestSessionGetHistoryFromID(t *testing.T) {
	store := newTempStore(t)
	id, _ := store.Create("planner")

	e1, _ := store.Append(id, agent.SessionEntry{Type: "user", Content: "a"})
	store.Append(id, agent.SessionEntry{Type: "assistant", Content: "b"})
	store.Append(id, agent.SessionEntry{Type: "user", Content: "c"})

	// fromID is exclusive: entries AFTER e1
	entries, err := store.GetHistory(id, 0, e1.ID)
	if err != nil {
		t.Fatalf("GetHistory fromID: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries after fromID, got %d", len(entries))
	}
}

func TestSessionBranch(t *testing.T) {
	store := newTempStore(t)
	id, _ := store.Create("planner")

	e1, _ := store.Append(id, agent.SessionEntry{Type: "user", Content: "a"})
	store.Append(id, agent.SessionEntry{Type: "assistant", Content: "b"})

	branchID, err := store.Branch(id, e1.ID)
	if err != nil {
		t.Fatalf("Branch: %v", err)
	}

	entries, err := store.GetHistory(branchID, 0, "")
	if err != nil {
		t.Fatalf("GetHistory branch: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry in branch (up to e1), got %d", len(entries))
	}
	if entries[0].Content != "a" {
		t.Errorf("unexpected branch content: %q", entries[0].Content)
	}
}

func TestSessionListSessions(t *testing.T) {
	store := newTempStore(t)

	id1, _ := store.Create("planner")
	id2, _ := store.Create("planner")
	store.Create("researcher") // different slug

	ids, err := store.ListSessions("planner")
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 sessions for planner, got %d: %v", len(ids), ids)
	}
	has := func(target string) bool {
		for _, id := range ids {
			if id == target {
				return true
			}
		}
		return false
	}
	if !has(id1) || !has(id2) {
		t.Errorf("missing expected IDs: got %v", ids)
	}
}
