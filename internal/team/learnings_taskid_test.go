package team

// learnings_taskid_test.go proves the exact TaskID scope filter that the Slack
// context-packer relies on for "task-scoped learnings": a foreign bot must only
// ever receive learnings recorded against its own task, never the whole brain.

import (
	"context"
	"testing"
)

func TestLearningSearchFiltersByTaskID(t *testing.T) {
	_, _, log, teardown := newLearningFixture(t)
	defer teardown()
	ctx := context.Background()

	mustAppend := func(key, taskID string) {
		t.Helper()
		if _, err := log.Append(ctx, LearningRecord{
			Type:       LearningTypePitfall,
			Key:        key,
			Insight:    "insight for " + key,
			Confidence: 7,
			Source:     LearningSourceObserved,
			Scope:      "repo",
			CreatedBy:  "codex",
			TaskID:     taskID,
		}); err != nil {
			t.Fatalf("append %s: %v", key, err)
		}
	}
	mustAppend("learning-task-a", "task-A")
	mustAppend("learning-task-b", "task-B")
	mustAppend("learning-no-task", "")

	// Exact TaskID match returns only that task's learning.
	got, err := log.Search(LearningSearchFilters{TaskID: "task-A"})
	if err != nil {
		t.Fatalf("search by task: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("TaskID filter returned %d results, want 1: %+v", len(got), got)
	}
	if got[0].Key != "learning-task-a" {
		t.Fatalf("TaskID filter returned wrong record: %q", got[0].Key)
	}

	// A task with no learnings yields nothing — never a fuzzy fallback.
	none, err := log.Search(LearningSearchFilters{TaskID: "task-Z"})
	if err != nil {
		t.Fatalf("search empty task: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("unknown TaskID returned %d results, want 0", len(none))
	}

	// Empty TaskID must NOT filter — all three records come back.
	all, err := log.Search(LearningSearchFilters{})
	if err != nil {
		t.Fatalf("search all: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("unfiltered search returned %d, want 3", len(all))
	}
}
