package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestResolveInitialOfficeAppSupportsArtifacts(t *testing.T) {
	if got := resolveInitialOfficeApp("artifacts"); got != officeAppArtifacts {
		t.Fatalf("expected artifacts app, got %q", got)
	}
}

func TestArtifactsCommandSwitchesToArtifactsApp(t *testing.T) {
	m := newChannelModel(false)
	m.width = 120
	m.height = 30
	m.input = []rune("/artifacts")
	m.inputPos = len(m.input)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(channelModel)

	if got.activeApp != officeAppArtifacts {
		t.Fatalf("expected artifacts app, got %q", got.activeApp)
	}
	if !strings.Contains(got.notice, "execution artifacts") {
		t.Fatalf("expected artifact notice, got %q", got.notice)
	}
}

func TestCurrentArtifactSummaryUsesLogsAndWorkflows(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeTaskLog(t, home, "task-1", `{"task_id":"task-1","agent_slug":"fe","tool_name":"grep","result":"Found Bubble Tea callsites","started_at":"2026-04-07T10:00:00Z","completed_at":"2026-04-07T10:01:00Z"}`)
	writeWorkflowRun(t, home, "one", "daily-digest", `{"provider":"one","workflow_key":"daily-digest","run_id":"run-1","status":"success","started_at":"2026-04-07T10:02:00Z","finished_at":"2026-04-07T10:03:00Z"}`)

	m := newChannelModel(false)
	m.requests = []channelInterview{{ID: "req-1", Kind: "approval", Status: "pending", Title: "Approve copy", Question: "Approve copy?", From: "ceo"}}

	got := m.currentArtifactSummary()
	if !strings.Contains(got, "task run") {
		t.Fatalf("expected task runs in summary, got %q", got)
	}
	if !strings.Contains(got, "workflow run") {
		t.Fatalf("expected workflow runs in summary, got %q", got)
	}
	if !strings.Contains(got, "action trace") {
		t.Fatalf("expected action traces in summary, got %q", got)
	}
}

func TestBuildArtifactLinesShowsTaskLogsWorkflowRunsAndApprovals(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeTaskLog(t, home, "task-1", `{"task_id":"task-1","agent_slug":"fe","tool_name":"bash","result":"ok","started_at":"2026-04-07T10:00:00Z","completed_at":"2026-04-07T10:01:00Z"}`)
	writeWorkflowRun(t, home, "one", "launch-sync", `{"provider":"one","workflow_key":"launch-sync","run_id":"run-2","status":"success","started_at":"2026-04-07T10:02:00Z","finished_at":"2026-04-07T10:03:00Z"}`)

	m := newChannelModel(false)
	m.tasks = []channelTask{{ID: "task-1", Title: "Ship launch notes", WorktreePath: "/tmp/wuphf-task-1", WorktreeBranch: "codex/task-1"}}
	m.requests = []channelInterview{{
		ID:            "req-1",
		Kind:          "approval",
		Status:        "pending",
		Title:         "Approve launch copy",
		Question:      "Approve launch copy?",
		Context:       "Need final sign-off on the launch blurb.",
		From:          "ceo",
		RecommendedID: "approve",
		CreatedAt:     time.Now().Add(-time.Minute).Format(time.RFC3339),
	}}
	m.actions = []channelAction{{
		ID:        "action-1",
		Kind:      "request_answered",
		Actor:     "you",
		Channel:   "general",
		Summary:   "Approved the launch direction.",
		RelatedID: "req-1",
		CreatedAt: time.Now().Format(time.RFC3339),
	}}

	lines := m.buildArtifactLines(96)
	plain := stripANSI(joinRenderedLines(lines))

	if !strings.Contains(plain, "Task execution") {
		t.Fatalf("expected task execution section, got %q", plain)
	}
	if !strings.Contains(plain, "Review next") {
		t.Fatalf("expected review-next section, got %q", plain)
	}
	if !strings.Contains(plain, "Resume next") {
		t.Fatalf("expected resume-next section, got %q", plain)
	}
	if !strings.Contains(plain, "Workflow runs") {
		t.Fatalf("expected workflow runs section, got %q", plain)
	}
	if !strings.Contains(plain, "Requests and approvals") {
		t.Fatalf("expected request section, got %q", plain)
	}
	if !strings.Contains(plain, "Action traces") {
		t.Fatalf("expected action traces section, got %q", plain)
	}
	if !strings.Contains(plain, "Ship launch notes") {
		t.Fatalf("expected task title in artifacts view, got %q", plain)
	}
	if !strings.Contains(plain, "launch-sync") {
		t.Fatalf("expected workflow key in artifacts view, got %q", plain)
	}
	if !strings.Contains(plain, "Progress:") {
		t.Fatalf("expected lifecycle progress metadata, got %q", plain)
	}
	if !strings.Contains(plain, "Output: ok") {
		t.Fatalf("expected retained output summary, got %q", plain)
	}
	if !strings.Contains(plain, "Resume in /tmp/wuphf-task-1 on codex/task-1") {
		t.Fatalf("expected resume hint, got %q", plain)
	}
	if !strings.Contains(plain, "Branch: codex/task-1") {
		t.Fatalf("expected worktree branch metadata, got %q", plain)
	}
	if !strings.Contains(plain, "Click to open task actions and resume context.") {
		t.Fatalf("expected explicit review/resume CTA, got %q", plain)
	}
}

func writeTaskLog(t *testing.T, home, taskID, record string) {
	t.Helper()
	path := filepath.Join(home, ".wuphf", "office", "tasks", taskID)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir task log dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(path, "output.log"), []byte(record+"\n"), 0o644); err != nil {
		t.Fatalf("write task log: %v", err)
	}
}

func writeWorkflowRun(t *testing.T, home, provider, key, record string) {
	t.Helper()
	path := filepath.Join(home, ".wuphf", "workflows", provider)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir workflow dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(path, key+".runs.jsonl"), []byte(record+"\n"), 0o644); err != nil {
		t.Fatalf("write workflow run: %v", err)
	}
}
