package main

import (
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
	"github.com/nex-crm/wuphf/internal/tui"
)

// Epicenter tests for the picker option builders extracted in
// channel_pickers.go. Each test exercises one branch of one builder so a
// failure points at exactly one decision in the projection. No setup
// outside the model under test, no shared fixtures, no time.Sleep.

func TestBuildTaskActionPickerOptions_DefaultTask(t *testing.T) {
	m := channelModel{}
	task := channelui.Task{ID: "t1", Status: "open"}

	opts := m.buildTaskActionPickerOptions(task)

	wantValues := []string{
		"claim:t1",
		"release:t1",
		"complete:t1",
		"block:t1",
	}
	gotValues := pickerValues(opts)
	if !equalStringSlice(gotValues, wantValues) {
		t.Fatalf("unexpected option values:\n got  %v\n want %v", gotValues, wantValues)
	}
}

func TestBuildTaskActionPickerOptions_ReadyForReviewShowsApprove(t *testing.T) {
	m := channelModel{}
	task := channelui.Task{ID: "t2", Status: "open", ReviewState: "ready_for_review"}

	opts := m.buildTaskActionPickerOptions(task)

	if !containsValue(opts, "approve:t2") {
		t.Fatalf("ready_for_review task should offer approve, got %v", pickerValues(opts))
	}
	if containsValue(opts, "complete:t2") {
		t.Fatalf("ready_for_review task should not offer plain complete, got %v", pickerValues(opts))
	}
}

func TestBuildTaskActionPickerOptions_LocalWorktreeShowsReadyForReview(t *testing.T) {
	m := channelModel{}
	task := channelui.Task{ID: "t3", Status: "open", ExecutionMode: "local_worktree"}

	opts := m.buildTaskActionPickerOptions(task)

	for _, opt := range opts {
		if opt.Value == "complete:t3" && opt.Label == "Ready for review" {
			return
		}
	}
	t.Fatalf("local_worktree task should offer Ready-for-review under value complete:t3, got %v", pickerValues(opts))
}

func TestBuildTaskActionPickerOptions_DoneTaskOmitsBlock(t *testing.T) {
	m := channelModel{}
	task := channelui.Task{ID: "t4", Status: "done"}

	opts := m.buildTaskActionPickerOptions(task)

	if containsValue(opts, "block:t4") {
		t.Fatalf("done task should not offer block, got %v", pickerValues(opts))
	}
}

func TestBuildTaskActionPickerOptions_ThreadIDAddsOpenThread(t *testing.T) {
	m := channelModel{}
	task := channelui.Task{ID: "t5", Status: "open", ThreadID: "thr-42"}

	opts := m.buildTaskActionPickerOptions(task)

	if !containsValue(opts, "open:t5") {
		t.Fatalf("task with ThreadID should offer Open thread, got %v", pickerValues(opts))
	}
}

func TestBuildRequestActionPickerOptions_BlockingRequestShowsUnblockHint(t *testing.T) {
	m := channelModel{}
	req := channelui.Interview{ID: "r1", Blocking: true}

	opts := m.buildRequestActionPickerOptions(req)

	for _, opt := range opts {
		if opt.Value == "dismiss:r1" {
			if !strings.Contains(opt.Description, "unblock") {
				t.Fatalf("blocking request dismiss should mention unblock, got %q", opt.Description)
			}
			return
		}
	}
	t.Fatalf("dismiss option missing for blocking request: %v", pickerValues(opts))
}

func TestBuildRequestActionPickerOptions_RequiredRequestShowsUnblockHint(t *testing.T) {
	m := channelModel{}
	req := channelui.Interview{ID: "r2", Required: true}

	opts := m.buildRequestActionPickerOptions(req)

	for _, opt := range opts {
		if opt.Value == "dismiss:r2" && strings.Contains(opt.Description, "unblock") {
			return
		}
	}
	t.Fatalf("required request dismiss should mention unblock: %v", pickerValues(opts))
}

func TestBuildRequestActionPickerOptions_ReplyToAddsOpenThread(t *testing.T) {
	m := channelModel{}
	req := channelui.Interview{ID: "r3", ReplyTo: "msg-1"}

	opts := m.buildRequestActionPickerOptions(req)

	if !containsValue(opts, "open:r3") {
		t.Fatalf("request with ReplyTo should offer Open thread, got %v", pickerValues(opts))
	}
}

func TestBuildTaskPickerOptions_FiltersByActiveChannel(t *testing.T) {
	m := channelModel{
		activeChannel: "engineering",
		tasks: []channelui.Task{
			{ID: "a", Title: "in scope", Channel: "engineering"},
			{ID: "b", Title: "elsewhere", Channel: "design"},
			{ID: "c", Title: "default channel", Channel: ""}, // empty -> "general"
		},
	}

	opts := m.buildTaskPickerOptions()

	got := pickerValues(opts)
	want := []string{"a"}
	if !equalStringSlice(got, want) {
		t.Fatalf("expected only engineering tasks, got %v want %v", got, want)
	}
}

func TestBuildRequestPickerOptions_FiltersByStatusAndChannel(t *testing.T) {
	m := channelModel{
		activeChannel: "general",
		requests: []channelui.Interview{
			{ID: "open-here", Status: "pending", Channel: "general", Question: "q"},
			{ID: "closed", Status: "resolved", Channel: "general", Question: "q"},
			{ID: "elsewhere", Status: "pending", Channel: "design", Question: "q"},
			{ID: "default-channel", Status: "open", Channel: "", Question: "q"},
		},
	}

	opts := m.buildRequestPickerOptions()

	got := pickerValues(opts)
	want := []string{"open-here", "default-channel"}
	if !equalStringSlice(got, want) {
		t.Fatalf("status+channel filter wrong, got %v want %v", got, want)
	}
}

func TestBuildRequestPickerOptions_EmptyChannelOnlyAppearsInGeneral(t *testing.T) {
	m := channelModel{
		activeChannel: "engineering",
		requests: []channelui.Interview{
			{ID: "default-channel", Status: "open", Channel: "", Question: "q"},
			{ID: "engineering", Status: "open", Channel: "engineering", Question: "q"},
		},
	}

	opts := m.buildRequestPickerOptions()

	got := pickerValues(opts)
	want := []string{"engineering"}
	if !equalStringSlice(got, want) {
		t.Fatalf("empty channel should be scoped to general only, got %v want %v", got, want)
	}
}

func TestBuildThreadPickerOptions_OnlyRootsWithReplies(t *testing.T) {
	m := channelModel{
		messages: []channelui.BrokerMessage{
			{ID: "1", From: "alice", Content: "root with reply"},
			{ID: "2", From: "bob", Content: "reply", ReplyTo: "1"},
			{ID: "3", From: "carol", Content: "lonely root"},
		},
		expandedThreads: map[string]bool{},
	}

	opts := m.buildThreadPickerOptions()

	got := pickerValues(opts)
	want := []string{"1"}
	if !equalStringSlice(got, want) {
		t.Fatalf("expected only root with replies, got %v want %v", got, want)
	}
}

// Test helpers — kept tiny so the test file stays epicenter-focused.

func pickerValues(opts []tui.PickerOption) []string {
	out := make([]string, 0, len(opts))
	for _, o := range opts {
		out = append(out, o.Value)
	}
	return out
}

func containsValue(opts []tui.PickerOption, want string) bool {
	for _, o := range opts {
		if o.Value == want {
			return true
		}
	}
	return false
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
