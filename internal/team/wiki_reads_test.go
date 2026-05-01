package team

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func newTestReadLog(t *testing.T) *ReadLog {
	t.Helper()
	dir := t.TempDir()
	return NewReadLog(dir)
}

// TestReadLog_Append_NoOp: empty reader must not create the file.
func TestReadLog_Append_NoOp(t *testing.T) {
	rl := newTestReadLog(t)
	rl.Append("team/people/alice.md", "")
	if _, err := os.Stat(rl.path); !os.IsNotExist(err) {
		t.Fatal("reads.jsonl should not be created when reader is empty")
	}
}

// TestReadLog_Append_Human: reader="web" produces IsAgent=false.
func TestReadLog_Append_Human(t *testing.T) {
	rl := newTestReadLog(t)
	rl.Append("team/people/alice.md", "web")
	s := rl.Stats("team/people/alice.md")
	if s.HumanReadCount != 1 {
		t.Errorf("HumanReadCount: want 1, got %d", s.HumanReadCount)
	}
	if s.AgentReadCount != 0 {
		t.Errorf("AgentReadCount: want 0, got %d", s.AgentReadCount)
	}
	if s.LastRead == nil {
		t.Error("LastRead should be non-nil after a read")
	}
}

// TestReadLog_Append_Agent: non-"web" reader produces IsAgent=true.
func TestReadLog_Append_Agent(t *testing.T) {
	rl := newTestReadLog(t)
	rl.Append("team/people/alice.md", "slack-agent")
	s := rl.Stats("team/people/alice.md")
	if s.AgentReadCount != 1 {
		t.Errorf("AgentReadCount: want 1, got %d", s.AgentReadCount)
	}
	if s.HumanReadCount != 0 {
		t.Errorf("HumanReadCount: want 0, got %d", s.HumanReadCount)
	}
}

// TestReadLog_Stats_NeverRead: path with no events returns zero stats.
func TestReadLog_Stats_NeverRead(t *testing.T) {
	rl := newTestReadLog(t)
	s := rl.Stats("team/people/ghost.md")
	if s.LastRead != nil {
		t.Error("LastRead should be nil for never-read article")
	}
	if s.HumanReadCount != 0 || s.AgentReadCount != 0 {
		t.Error("counts should be zero for never-read article")
	}
}

// TestReadLog_Stats_Mixed: multiple readers split counts correctly.
func TestReadLog_Stats_Mixed(t *testing.T) {
	rl := newTestReadLog(t)
	path := "team/companies/acme.md"
	rl.Append(path, "web")
	rl.Append(path, "web")
	rl.Append(path, "slack-agent")
	rl.Append(path, "research-agent")
	rl.Append(path, "research-agent")
	s := rl.Stats(path)
	if s.HumanReadCount != 2 {
		t.Errorf("HumanReadCount: want 2, got %d", s.HumanReadCount)
	}
	if s.AgentReadCount != 3 {
		t.Errorf("AgentReadCount: want 3, got %d", s.AgentReadCount)
	}
}

// TestReadLog_Stats_DaysUnread: article read recently has DaysUnread=0.
func TestReadLog_Stats_DaysUnread(t *testing.T) {
	rl := newTestReadLog(t)
	rl.Append("team/people/bob.md", "web")
	s := rl.Stats("team/people/bob.md")
	if s.DaysUnread != 0 {
		t.Errorf("DaysUnread: want 0 for today's read, got %d", s.DaysUnread)
	}
}

// TestReadLog_AllStats_MultiPath: AllStats returns correct data for all paths.
func TestReadLog_AllStats_MultiPath(t *testing.T) {
	rl := newTestReadLog(t)
	rl.Append("team/people/alice.md", "web")
	rl.Append("team/people/bob.md", "slack-agent")
	rl.Append("team/people/alice.md", "research-agent")

	all := rl.AllStats()
	if len(all) != 2 {
		t.Fatalf("AllStats: want 2 paths, got %d", len(all))
	}
	alice := all["team/people/alice.md"]
	if alice.HumanReadCount != 1 || alice.AgentReadCount != 1 {
		t.Errorf("alice: want human=1 agent=1, got human=%d agent=%d",
			alice.HumanReadCount, alice.AgentReadCount)
	}
	bob := all["team/people/bob.md"]
	if bob.AgentReadCount != 1 || bob.HumanReadCount != 0 {
		t.Errorf("bob: want agent=1 human=0, got agent=%d human=%d",
			bob.AgentReadCount, bob.HumanReadCount)
	}
}

// TestReadLog_AllStats_NoFile: missing reads.jsonl returns empty map, not error.
func TestReadLog_AllStats_NoFile(t *testing.T) {
	rl := newTestReadLog(t)
	all := rl.AllStats()
	if all == nil {
		t.Error("AllStats should return non-nil map when file is missing")
	}
	if len(all) != 0 {
		t.Errorf("AllStats: want empty map, got %d entries", len(all))
	}
}

// TestReadLog_EnsureDir: Append creates the .reads directory if absent.
func TestReadLog_EnsureDir(t *testing.T) {
	dir := t.TempDir()
	// Point to a subdirectory that doesn't exist yet.
	rl := NewReadLog(filepath.Join(dir, "nonexistent"))
	rl.Append("team/people/alice.md", "web")
	if _, err := os.Stat(rl.path); err != nil {
		t.Fatalf("reads.jsonl not created: %v", err)
	}
}

// TestReadLog_MalformedLine: corrupt line is skipped, valid lines still counted.
func TestReadLog_MalformedLine(t *testing.T) {
	rl := newTestReadLog(t)
	rl.Append("team/people/alice.md", "web")

	// Inject a malformed line directly.
	if err := os.MkdirAll(filepath.Dir(rl.path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(rl.path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("{not valid json}\n")
	f.Close()

	rl.Append("team/people/alice.md", "web")

	s := rl.Stats("team/people/alice.md")
	if s.HumanReadCount != 2 {
		t.Errorf("HumanReadCount: want 2 valid reads, got %d", s.HumanReadCount)
	}
}

// TestReadLog_Concurrent: 20 goroutines appending simultaneously produce valid JSON lines.
func TestReadLog_Concurrent(t *testing.T) {
	rl := newTestReadLog(t)
	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			rl.Append("team/people/alice.md", "web")
		}()
	}
	wg.Wait()

	s := rl.Stats("team/people/alice.md")
	if s.HumanReadCount != n {
		t.Errorf("concurrent: want %d reads, got %d", n, s.HumanReadCount)
	}
}

// TestReadLog_NilSafe: nil ReadLog never panics.
func TestReadLog_NilSafe(t *testing.T) {
	var rl *ReadLog
	rl.Append("team/people/alice.md", "web") // should not panic
	s := rl.Stats("team/people/alice.md")
	if s.LastRead != nil {
		t.Error("nil ReadLog Stats should return zero")
	}
	all := rl.AllStats()
	if len(all) != 0 {
		t.Error("nil ReadLog AllStats should return empty map")
	}
}

// TestReadLog_Stats_DaysUnread_Positive: backdated event produces DaysUnread > 0.
func TestReadLog_Stats_DaysUnread_Positive(t *testing.T) {
	rl := newTestReadLog(t)
	path := "team/people/old.md"

	// Write a backdated entry directly to the file (3 days ago).
	if err := os.MkdirAll(filepath.Dir(rl.path), 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-72 * time.Hour)
	ev := ReadEvent{Path: path, Timestamp: old, Reader: "web", IsAgent: false}
	line, _ := json.Marshal(ev)
	if err := os.WriteFile(rl.path, append(line, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	s := rl.Stats(path)
	if s.DaysUnread < 2 {
		t.Errorf("DaysUnread: want >=2 for 72h-old read, got %d", s.DaysUnread)
	}
}

func TestReadLog_LastRead_IsLatest(t *testing.T) {
	rl := newTestReadLog(t)
	path := "team/people/carol.md"
	rl.Append(path, "web")
	time.Sleep(2 * time.Millisecond)
	before := time.Now()
	time.Sleep(2 * time.Millisecond)
	rl.Append(path, "slack-agent")

	s := rl.Stats(path)
	if s.LastRead == nil {
		t.Fatal("LastRead should be non-nil")
	}
	if !s.LastRead.After(before) {
		t.Error("LastRead should reflect the most recent access")
	}
}
