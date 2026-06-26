package team

// task_artifact_delta_test.go — the resubmission artifact-delta gate
// (done-integrity fix family; ICP-eval v2 [00:30]: after a human
// request-changes, the agent announced "revised and back in review" while
// the artifact on disk was byte-identical):
//
//  1. request_changes stamps the artifact's content hash on the objection.
//  2. An agent resubmission (submit_for_review/complete) with an unchanged
//     artifact is refused; changing the bytes clears the gate.
//  3. Human actors are exempt — the human knows what they reviewed.
//  4. An unreadable artifact (external/visual-artifact reference) degrades
//     to an action-log audit stamp instead of a block.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newArtifactDeltaBroker seeds a broker with one in-review task whose
// wiki-relative artifact is readable under the broker's wiki root (the
// production resolution path for team/ artifacts — task worktrees are
// rewritten by the worktree manager on every mutation, so they are not a
// stable root for tests). Returns the broker and the artifact's absolute
// path.
func newArtifactDeltaBroker(t *testing.T) (*Broker, string) {
	t.Helper()
	b := newHumanNoteTestBroker(t)
	wikiRoot := filepath.Join(t.TempDir(), "wiki")
	// The worker is only wired for its Repo().Root(); it is never started
	// and no writes are enqueued — the artifact file is written directly.
	b.wikiWorker = NewWikiWorker(NewRepoAt(wikiRoot, filepath.Join(t.TempDir(), "wiki.bak")), b)
	artifactRel := "team/accounts/launch-onepager.md"
	abs := filepath.Join(wikiRoot, filepath.FromSlash(artifactRel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte("# One-pager v1\nchampion: Jordan Park\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b.tasks = append(b.tasks, teamTask{
		ID:          "task-delta-1",
		Channel:     "general",
		Title:       "Build the Acme one-pager",
		Owner:       "eng",
		status:      "review",
		reviewState: "ready_for_review",
		Artifact:    artifactRel,
	})
	return b, abs
}

func TestArtifactDelta_ByteIdenticalResubmissionBlocked(t *testing.T) {
	t.Parallel()
	b, abs := newArtifactDeltaBroker(t)

	if _, err := b.MutateTask(TaskPostRequest{
		Action: "request_changes", ID: "task-delta-1", Channel: "general",
		CreatedBy: "human", Details: "Use Dana as the champion, not a fabricated contact.",
	}); err != nil {
		t.Fatalf("request_changes: %v", err)
	}
	bounced := b.TaskByID("task-delta-1")
	if bounced.ChangesRequested == nil || !strings.HasPrefix(bounced.ChangesRequested.ArtifactHash, "sha256:") {
		t.Fatalf("request_changes must stamp the artifact content hash; got %+v", bounced.ChangesRequested)
	}

	// Byte-identical resubmission: refused, naming the contract.
	for _, action := range []string{"submit_for_review", "complete"} {
		_, err := b.MutateTask(TaskPostRequest{Action: action, ID: "task-delta-1", Channel: "general", CreatedBy: "eng", Details: "revised"})
		var mutationErr *TaskMutationError
		if !errors.As(err, &mutationErr) || mutationErr.Kind != TaskMutationInvalid {
			t.Fatalf("agent %s with an unchanged artifact must be invalid; got %v", action, err)
		}
		if !strings.Contains(mutationErr.Message, "byte-identical") {
			t.Errorf("%s error must name the byte-identical artifact; got %q", action, mutationErr.Message)
		}
	}

	// A real edit clears the gate.
	if err := os.WriteFile(abs, []byte("# One-pager v2\nchampion: Dana Whitfield\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := b.MutateTask(TaskPostRequest{Action: "submit_for_review", ID: "task-delta-1", Channel: "general", CreatedBy: "eng", Details: "revised with Dana"}); err != nil {
		t.Fatalf("resubmission after a real artifact change must succeed; got %v", err)
	}
}

func TestArtifactDelta_HumanActorExempt(t *testing.T) {
	t.Parallel()
	b, _ := newArtifactDeltaBroker(t)
	if _, err := b.MutateTask(TaskPostRequest{
		Action: "request_changes", ID: "task-delta-1", Channel: "general",
		CreatedBy: "human", Details: "Champion is wrong.",
	}); err != nil {
		t.Fatalf("request_changes: %v", err)
	}
	// The human completing their own bounced task is never delta-gated (and
	// also clears the objection on the locked path).
	if _, err := b.MutateTask(TaskPostRequest{Action: "complete", ID: "task-delta-1", Channel: "general", CreatedBy: "human"}); err != nil {
		t.Fatalf("human complete must bypass the delta gate; got %v", err)
	}
}

func TestArtifactDelta_UnreadableArtifactAllowsAndStamps(t *testing.T) {
	t.Parallel()
	b := newHumanNoteTestBroker(t)
	b.tasks = append(b.tasks, teamTask{
		ID:          "task-delta-2",
		Channel:     "general",
		Title:       "Ship the exec one-pager",
		Owner:       "eng",
		status:      "review",
		reviewState: "ready_for_review",
		Artifact:    "ra_0099c69dde6f6ad3", // visual-artifact id — no file anywhere
	})
	if _, err := b.MutateTask(TaskPostRequest{
		Action: "request_changes", ID: "task-delta-2", Channel: "general",
		CreatedBy: "human", Details: "Re-draft to Dana.",
	}); err != nil {
		t.Fatalf("request_changes: %v", err)
	}
	if obj := b.TaskByID("task-delta-2").ChangesRequested; obj == nil || obj.ArtifactHash != "" {
		t.Fatalf("unreadable artifact must leave ArtifactHash empty; got %+v", obj)
	}
	if _, err := b.MutateTask(TaskPostRequest{Action: "submit_for_review", ID: "task-delta-2", Channel: "general", CreatedBy: "eng", Details: "revised"}); err != nil {
		t.Fatalf("unverifiable delta must allow the resubmission; got %v", err)
	}
	stamped := false
	for _, a := range b.Actions() {
		if a.Kind == taskResubmitUnverifiedActionKind && a.RelatedID == "task-delta-2" &&
			strings.Contains(a.Summary, "resubmitted without verifiable artifact delta") {
			stamped = true
		}
	}
	if !stamped {
		t.Errorf("unverifiable resubmission must stamp the audit action-log line")
	}
}

func TestArtifactDelta_NoObjectionNoGate(t *testing.T) {
	t.Parallel()
	b, _ := newArtifactDeltaBroker(t)
	// No request_changes on record: a plain submit passes untouched.
	if _, err := b.MutateTask(TaskPostRequest{Action: "submit_for_review", ID: "task-delta-1", Channel: "general", CreatedBy: "eng", Details: "first submission"}); err != nil {
		t.Fatalf("submit without an open objection must not be gated; got %v", err)
	}
}
