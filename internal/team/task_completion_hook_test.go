package team

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func newCompletionHookBroker(t *testing.T) *Broker {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	b := NewBrokerAt(filepath.Join(t.TempDir(), "state.json"))
	b.mu.Lock()
	b.members = []officeMember{
		{Slug: "ceo", Name: "CEO"},
		{Slug: "eng", Name: "Engineer"},
	}
	b.channels = []teamChannel{
		{Slug: "general", Name: "general", Members: []string{"human", "ceo", "eng"}},
	}
	b.mu.Unlock()
	return b
}

func completionHookCreateDefinedTask(t *testing.T, b *Broker) string {
	t.Helper()
	created, err := b.MutateTask(TaskPostRequest{
		Action: "create", Channel: "general", Title: "Close the Acme Corp renewal",
		Details: "Coordinate with @eng on the Acme Corp renewal brief.",
		Owner:   "eng", CreatedBy: "ceo",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := b.MutateTask(TaskPostRequest{
		Action: "define", ID: created.Task.ID, Channel: "general", CreatedBy: "ceo",
		Definition: &TaskDefinition{
			Goal:            "Renew Acme Corp for 12 months",
			Deliverables:    []TaskDeliverable{{Name: "renewal brief", Format: "markdown in the wiki"}},
			SuccessCriteria: []string{"Renewal brief published to the wiki"},
		},
	}); err != nil {
		t.Fatalf("define: %v", err)
	}
	approveAndStartAsHuman(t, b, created.Task.ID)
	return created.Task.ID
}

// approveAndStartAsHuman activates a Drafting task the way the live FE
// does (human Approve & Start → drafting→running). Tasks created via
// MutateTask action=create land in Drafting, and completion from a
// pre-start state is impossible by contract (ICP-eval v3 fix family #1),
// so test flows must pass the human gate before driving work.
func approveAndStartAsHuman(t *testing.T, b *Broker, taskID string) {
	t.Helper()
	if _, err := b.MutateTask(TaskPostRequest{
		Action: "approve", ID: taskID, Channel: "general", CreatedBy: "human",
	}); err != nil {
		t.Fatalf("approve & start %s: %v", taskID, err)
	}
}

// finishTask drives a task to done through whichever path its review
// template demands (complete may route through review → approve).
func finishTask(t *testing.T, b *Broker, taskID, artifactPath string) error {
	t.Helper()
	if _, err := b.MutateTask(TaskPostRequest{
		Action: "complete", ID: taskID, Channel: "general", CreatedBy: "eng",
		ArtifactPath: artifactPath,
	}); err != nil {
		return err
	}
	if cur := b.TaskByID(taskID); cur != nil && !strings.EqualFold(strings.TrimSpace(cur.status), "done") {
		_, err := b.MutateTask(TaskPostRequest{
			Action: "approve", ID: taskID, Channel: "general", CreatedBy: "ceo",
			ArtifactPath: artifactPath,
		})
		return err
	}
	return nil
}

// A task with a Definition cannot reach done without an artifact; the gate
// names the field to pass and rolls the mutation back.
func TestArtifactGate_BlocksDefinedTaskWithoutArtifact(t *testing.T) {
	b := newCompletionHookBroker(t)
	taskID := completionHookCreateDefinedTask(t, b)

	err := finishTask(t, b, taskID, "")
	var mutationErr *TaskMutationError
	if !errors.As(err, &mutationErr) || mutationErr.Kind != TaskMutationArtifactRequired {
		t.Fatalf("expected TaskMutationArtifactRequired, got %v", err)
	}
	if !strings.Contains(mutationErr.Message, "artifact_path") {
		t.Fatalf("gate error must name artifact_path: %q", mutationErr.Message)
	}
	if task := b.TaskByID(taskID); strings.EqualFold(strings.TrimSpace(task.status), "done") {
		t.Fatalf("gate must roll back the done transition, got status=%q", task.status)
	}
}

// Passing artifact_path on the completing mutation clears the gate, stores
// the artifact on the wire shape, and fires the deterministic done-post +
// Inbox notice.
func TestArtifactGate_DonePostAndNoticeWithArtifact(t *testing.T) {
	b := newCompletionHookBroker(t)
	taskID := completionHookCreateDefinedTask(t, b)

	const artifact = "team/playbooks/acme-renewal.md"
	if err := finishTask(t, b, taskID, artifact); err != nil {
		t.Fatalf("finish with artifact: %v", err)
	}
	task := b.TaskByID(taskID)
	if !strings.EqualFold(strings.TrimSpace(task.status), "done") || task.Artifact != artifact {
		t.Fatalf("expected done with artifact, got status=%q artifact=%q", task.status, task.Artifact)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	donePost := ""
	for _, msg := range b.messages {
		if msg.Kind == taskDeliveredMessageKind && msg.SourceTaskID == taskID {
			donePost = msg.Content
		}
	}
	if !strings.Contains(donePost, "delivered: Renewal brief published to the wiki") ||
		!strings.Contains(donePost, "artifact: "+artifact) {
		t.Fatalf("done-post missing summary/artifact: %q", donePost)
	}
	noticeCount := 0
	for _, req := range b.requests {
		if req.Kind != "notice" || strings.TrimSpace(req.IssueID) != taskID {
			continue
		}
		// The drafting-entry "waiting on you" notice (N5) shares the
		// kind=notice primitive — this test pins the DELIVERY notice only.
		if req.Title == awaitingStartNoticeTitle(taskID) {
			continue
		}
		noticeCount++
		if req.Blocking || req.Required {
			t.Fatalf("delivery notice must be non-blocking/non-required: %+v", req)
		}
		if !requestIsActive(req) {
			t.Fatalf("delivery notice must be active: %+v", req)
		}
		if req.ReminderAt != "" || req.FollowUpAt != "" {
			t.Fatalf("delivery notice must not schedule reminders: %+v", req)
		}
	}
	if noticeCount != 1 {
		t.Fatalf("expected exactly one delivery notice, got %d", noticeCount)
	}
}

// Tasks WITHOUT a Definition keep legacy behavior: no artifact gate, but the
// done-post still fires (without the artifact segment).
func TestArtifactGate_LegacyTaskWithoutDefinitionUnaffected(t *testing.T) {
	b := newCompletionHookBroker(t)
	created, err := b.MutateTask(TaskPostRequest{
		Action: "create", Channel: "general", Title: "Send the weekly digest",
		Details: "Send it to the list.", Owner: "eng", CreatedBy: "ceo",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	approveAndStartAsHuman(t, b, created.Task.ID)
	if err := finishTask(t, b, created.Task.ID, ""); err != nil {
		t.Fatalf("legacy task must complete without artifact: %v", err)
	}
	task := b.TaskByID(created.Task.ID)
	if !strings.EqualFold(strings.TrimSpace(task.status), "done") {
		t.Fatalf("expected done, got %q", task.status)
	}
}

// Invalid artifact references are rejected at the boundary.
func TestArtifactGate_RejectsInvalidArtifactPath(t *testing.T) {
	b := newCompletionHookBroker(t)
	taskID := completionHookCreateDefinedTask(t, b)
	for _, bad := range []string{"/etc/passwd", "../../secrets.md"} {
		_, err := b.MutateTask(TaskPostRequest{
			Action: "comment", ID: taskID, Channel: "general", CreatedBy: "eng",
			Details: "note", ArtifactPath: bad,
		})
		var mutationErr *TaskMutationError
		if !errors.As(err, &mutationErr) || mutationErr.Kind != TaskMutationInvalid {
			t.Fatalf("artifact path %q: expected invalid error, got %v", bad, err)
		}
	}
}

// Reopen on an owned task lands in Running (executable — the dispatch gate
// sendTaskUpdate checks) so the owner is re-engaged; an ownerless task keeps
// the Drafting landing for the human to staff + approve.
func TestReopen_OwnedTaskReturnsToRunning(t *testing.T) {
	b := newCompletionHookBroker(t)
	created, err := b.MutateTask(TaskPostRequest{
		Action: "create", Channel: "general", Title: "Ship the launch checklist",
		Details: "Checklist work.", Owner: "eng", CreatedBy: "ceo",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	approveAndStartAsHuman(t, b, created.Task.ID)
	if err := finishTask(t, b, created.Task.ID, ""); err != nil {
		t.Fatalf("finish: %v", err)
	}
	if _, err := b.MutateTask(TaskPostRequest{Action: "reopen", ID: created.Task.ID, Channel: "general", CreatedBy: "ceo"}); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	task := b.TaskByID(created.Task.ID)
	if task.LifecycleState != LifecycleStateRunning || !isExecutableTeamTaskStatus(task.LifecycleState) {
		t.Fatalf("owned reopen must land Running, got %s", task.LifecycleState)
	}
	if !strings.EqualFold(strings.TrimSpace(task.status), "in_progress") {
		t.Fatalf("owned reopen status must be in_progress, got %q", task.status)
	}
	if task.CompletedAt != "" {
		t.Fatalf("reopen must clear CompletedAt, got %q", task.CompletedAt)
	}
}

func TestReopen_OwnerlessTaskReturnsToDrafting(t *testing.T) {
	b := newCompletionHookBroker(t)
	created, err := b.MutateTask(TaskPostRequest{
		Action: "create", Channel: "general", Title: "Backlog item to triage",
		Details: "No owner yet.", CreatedBy: "ceo",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := b.MutateTask(TaskPostRequest{Action: "cancel", ID: created.Task.ID, Channel: "general", CreatedBy: "ceo"}); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := b.MutateTask(TaskPostRequest{Action: "reopen", ID: created.Task.ID, Channel: "general", CreatedBy: "ceo"}); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	task := b.TaskByID(created.Task.ID)
	if task.LifecycleState != LifecycleStateDrafting {
		t.Fatalf("ownerless reopen must land Drafting, got %s", task.LifecycleState)
	}
}

// The artifact rides the teamTask wire under the additive "artifact" key
// through all four marshal sites.
func TestTeamTaskWire_ArtifactRoundTrips(t *testing.T) {
	task := teamTask{ID: "TASK-1", Title: "t", Artifact: "team/playbooks/x.md", CreatedAt: "now", UpdatedAt: "now"}
	blob, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(blob), `"artifact":"team/playbooks/x.md"`) {
		t.Fatalf("wire key missing: %s", blob)
	}
	var back teamTask
	if err := json.Unmarshal(blob, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Artifact != task.Artifact {
		t.Fatalf("artifact lost in round trip: %q", back.Artifact)
	}
}

func TestValidateTaskArtifactPath(t *testing.T) {
	for _, ok := range []string{"team/playbooks/launch.md", "agents/eng/notes.md", "visual-artifact-42"} {
		if err := validateTaskArtifactPath(ok); err != nil {
			t.Errorf("expected %q valid, got %v", ok, err)
		}
	}
	for _, bad := range []string{"", "/abs/path.md", `\windows\path.md`, "team/../../etc"} {
		if err := validateTaskArtifactPath(bad); err == nil {
			t.Errorf("expected %q rejected", bad)
		}
	}
}

// Deterministic entity extraction: @mentions → people (minus plumbing
// slugs), capitalized multi-word names in goal/deliverables → companies,
// deduped, bounded, slug-validated.
func TestTaskCompletionEntities_Deterministic(t *testing.T) {
	task := teamTask{
		ID:      "TASK-9",
		Title:   "Close the renewal with @eng",
		Details: "Loop in @ceo and @human for sign-off. cc @eng again.",
		Definition: &TaskDefinition{
			Goal: "Renew the Acme Corp account and brief Globex Industries on timing",
			Deliverables: []TaskDeliverable{
				{Name: "Acme Corp renewal brief", Format: "markdown"},
			},
		},
	}
	got := taskCompletionEntities(task)
	want := map[string]EntityKind{
		"eng":               EntityKindPeople,
		"ceo":               EntityKindPeople,
		"acme-corp":         EntityKindCompanies,
		"globex-industries": EntityKindCompanies,
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d entities, got %+v", len(want), got)
	}
	for _, e := range got {
		if want[e.Slug] != e.Kind {
			t.Errorf("unexpected entity %s/%s", e.Kind, e.Slug)
		}
	}
	// @human is plumbing, never an entity.
	for _, e := range got {
		if e.Slug == "human" {
			t.Errorf("plumbing slug must be excluded: %+v", e)
		}
	}

	// Fact text carries the task id, the artifact, and kinded wikilinks for
	// the co-occurring entities (graph edge source), and is timestamp-free
	// so the deterministic fact-id dedup absorbs replays.
	task.Artifact = "team/playbooks/renewal.md"
	text := taskCompletionFactText(task, got, got[0])
	if !strings.Contains(text, "TASK-9") || !strings.Contains(text, "team/playbooks/renewal.md") {
		t.Fatalf("fact text missing task/artifact association: %q", text)
	}
	if !strings.Contains(text, "[[companies/acme-corp]]") {
		t.Fatalf("fact text missing co-occurrence wikilink: %q", text)
	}
	if strings.Contains(text, "[["+string(got[0].Kind)+"/"+got[0].Slug+"]]") {
		t.Fatalf("fact text must not self-link: %q", text)
	}
	if again := taskCompletionFactText(task, got, got[0]); again != text {
		t.Fatalf("fact text must be deterministic")
	}
}

func TestTaskDeliveredContentLine(t *testing.T) {
	task := &teamTask{
		ID:    "TASK-3",
		Title: "Close the renewal",
		Definition: &TaskDefinition{
			Goal:            "Renew Acme",
			SuccessCriteria: []string{"Signed order form in the wiki"},
		},
		Artifact: "team/deals/acme.md",
	}
	got := taskDeliveredContentLine(task)
	want := "Close the renewal delivered: Signed order form in the wiki — artifact: team/deals/acme.md"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	// No criteria → goal; no definition → title; no artifact → segment omitted.
	task.Definition.SuccessCriteria = nil
	task.Artifact = ""
	if got := taskDeliveredContentLine(task); got != "Close the renewal delivered: Renew Acme" {
		t.Fatalf("goal fallback wrong: %q", got)
	}
	task.Definition = nil
	if got := taskDeliveredContentLine(task); got != "Close the renewal delivered: Close the renewal" {
		t.Fatalf("title fallback wrong: %q", got)
	}
}
