package team

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNotebookSignalScanner_MissingAgentsDirSilent verifies that scanning a
// wiki that has no team/agents/ directory yet — the steady state on a fresh
// install before any agents are seeded — returns cleanly without emitting a
// walk-error log. Regression for #982.
func TestNotebookSignalScanner_MissingAgentsDirSilent(t *testing.T) {
	b, root, teardown := newNotebookScannerHarness(t)
	defer teardown()

	// Belt-and-suspenders: explicitly ensure the agents dir does NOT exist.
	// Repo.Init seeds team/{people,companies,…} but not team/agents/.
	agentsDir := filepath.Join(root, "team", "agents")
	if _, err := os.Stat(agentsDir); err == nil {
		if err := os.RemoveAll(agentsDir); err != nil {
			t.Fatalf("remove agents dir: %v", err)
		}
	}

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	scanner := NewNotebookSignalScanner(b)
	cands, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(cands) != 0 {
		t.Fatalf("expected 0 candidates from missing agents dir, got %d", len(cands))
	}
	if got := buf.String(); strings.Contains(got, "walk error") {
		t.Fatalf("expected no walk-error log line, got:\n%s", got)
	}
	if got := buf.String(); strings.Contains(got, "no such file or directory") {
		t.Fatalf("expected no ENOENT log line, got:\n%s", got)
	}
}
