package team

import (
	"path/filepath"
	"testing"
)

func TestTurnManifestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")

	events := []HeadlessEvent{
		{
			Type: HeadlessEventTypeManifest, TaskID: "OFFICE-14", TurnID: "t1",
			Agent: "revops", StartedAt: "2026-06-16T00:00:00Z",
			ToolCalls: []HeadlessManifestEntry{
				{ToolName: "slack_lookup", Count: 1},
				{ToolName: "slack_send", Count: 2},
			},
		},
		{
			Type: HeadlessEventTypeManifest, TaskID: "OFFICE-15", TurnID: "t1",
			Agent:     "revops",
			ToolCalls: []HeadlessManifestEntry{{ToolName: "slack_send", Count: 1}},
		},
	}
	for _, e := range events {
		m, ok := turnManifestFromEvent(e)
		if !ok {
			t.Fatalf("expected usable manifest from %+v", e)
		}
		if err := appendTurnManifest(path, m); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	got, err := ReadTurnManifests(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 records, got %d", len(got))
	}
	first := got[0]
	if first.TaskID != "OFFICE-14" || first.Agent != "revops" {
		t.Fatalf("bad attribution: %+v", first)
	}
	if len(first.Tools) != 2 || first.Tools[1].Name != "slack_send" || first.Tools[1].Count != 2 {
		t.Fatalf("bad tools: %+v", first.Tools)
	}
}

func TestTurnManifestFromEventSkips(t *testing.T) {
	cases := map[string]HeadlessEvent{
		"not a manifest": {Type: HeadlessEventTypeToolUse, TaskID: "x", ToolCalls: []HeadlessManifestEntry{{ToolName: "a", Count: 1}}},
		"no task id":     {Type: HeadlessEventTypeManifest, TaskID: " ", ToolCalls: []HeadlessManifestEntry{{ToolName: "a", Count: 1}}},
		"no tools":       {Type: HeadlessEventTypeManifest, TaskID: "x"},
		"blank tool":     {Type: HeadlessEventTypeManifest, TaskID: "x", ToolCalls: []HeadlessManifestEntry{{ToolName: "  ", Count: 1}}},
	}
	for name, e := range cases {
		if _, ok := turnManifestFromEvent(e); ok {
			t.Fatalf("%s: expected skip", name)
		}
	}
}

func TestReadTurnManifestsMissingFileIsEmpty(t *testing.T) {
	got, err := ReadTurnManifests(filepath.Join(t.TempDir(), "absent.jsonl"))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty, got %d", len(got))
	}
}

func TestAppendTurnManifestEmptyPath(t *testing.T) {
	if err := appendTurnManifest("", TurnManifest{TaskID: "x"}); err == nil {
		t.Fatal("expected error on empty path")
	}
}
