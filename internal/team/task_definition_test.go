package team

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// newDefineTestBroker returns a broker with a general channel and a ceo+eng
// roster, so the CEO-managed scope gate engages (officeLeadSlugFrom resolves
// "ceo" and "eng" is a registered specialist).
func newDefineTestBroker(t *testing.T) *Broker {
	t.Helper()
	b := NewBrokerAt(filepath.Join(t.TempDir(), "broker-state.json"))
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

func TestNormalizeTaskDefinition(t *testing.T) {
	t.Parallel()
	const now = "2026-06-10T00:00:00Z"

	if _, err := normalizeTaskDefinition(nil, now); err == nil {
		t.Fatal("nil definition must be rejected")
	}
	if _, err := normalizeTaskDefinition(&TaskDefinition{Goal: "   "}, now); err == nil {
		t.Fatal("empty goal must be rejected")
	}
	if _, err := normalizeTaskDefinition(&TaskDefinition{
		Goal:            "ship it",
		SuccessCriteria: []string{"tests pass", "  "},
	}, now); err == nil {
		t.Fatal("blank success criterion must be rejected")
	}
	if _, err := normalizeTaskDefinition(&TaskDefinition{
		Goal:         "ship it",
		Deliverables: []TaskDeliverable{{Name: " "}},
	}, now); err == nil {
		t.Fatal("blank deliverable name must be rejected")
	}

	in := &TaskDefinition{
		Goal:            "  ship the export  ",
		Deliverables:    []TaskDeliverable{{Name: " export script ", Format: " python file "}},
		SuccessCriteria: []string{" round-trips big ints "},
		AccessNeeded:    []string{" prod read replica ", ""},
		DefinedAt:       "1999-01-01T00:00:00Z", // caller-supplied — must be ignored
	}
	out, err := normalizeTaskDefinition(in, now)
	if err != nil {
		t.Fatalf("normalizeTaskDefinition: %v", err)
	}
	if out.Goal != "ship the export" {
		t.Fatalf("goal not trimmed: %q", out.Goal)
	}
	if out.Deliverables[0].Name != "export script" || out.Deliverables[0].Format != "python file" {
		t.Fatalf("deliverable not trimmed: %+v", out.Deliverables[0])
	}
	if len(out.AccessNeeded) != 1 || out.AccessNeeded[0] != "prod read replica" {
		t.Fatalf("access needed not cleaned: %v", out.AccessNeeded)
	}
	if out.DefinedAt != now {
		t.Fatalf("DefinedAt must be broker-stamped, got %q", out.DefinedAt)
	}
	// Returned struct must not alias caller-owned slices.
	in.Deliverables[0].Name = "mutated"
	if out.Deliverables[0].Name != "export script" {
		t.Fatal("normalized definition aliases caller input")
	}
}

func TestTaskDefinitionWireRoundTrip(t *testing.T) {
	t.Parallel()
	task := teamTask{
		ID:    "task-1",
		Title: "Ship the export",
		Definition: &TaskDefinition{
			Goal:            "round-trip the wire",
			Deliverables:    []TaskDeliverable{{Name: "doc", Format: "markdown"}},
			SuccessCriteria: []string{"definition survives marshal/unmarshal"},
			AccessNeeded:    []string{"none"},
			DefinedAt:       "2026-06-10T00:00:00Z",
		},
		CreatedBy: "ceo",
		CreatedAt: "2026-06-10T00:00:00Z",
		UpdatedAt: "2026-06-10T00:00:00Z",
	}
	blob, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{`"definition"`, `"goal"`, `"deliverables"`, `"success_criteria"`, `"access_needed"`, `"defined_at"`} {
		if !strings.Contains(string(blob), key) {
			t.Fatalf("wire JSON missing %s: %s", key, blob)
		}
	}
	var back teamTask
	if err := json.Unmarshal(blob, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Definition == nil || back.Definition.Goal != "round-trip the wire" ||
		back.Definition.Deliverables[0].Format != "markdown" ||
		back.Definition.SuccessCriteria[0] != "definition survives marshal/unmarshal" ||
		back.Definition.DefinedAt != "2026-06-10T00:00:00Z" {
		t.Fatalf("definition did not round-trip: %+v", back.Definition)
	}

	// Additive contract: legacy state without the key loads with a nil
	// Definition.
	var legacy teamTask
	if err := json.Unmarshal([]byte(`{"id":"task-old","title":"t","status":"open","created_by":"ceo","created_at":"x","updated_at":"x"}`), &legacy); err != nil {
		t.Fatalf("legacy unmarshal: %v", err)
	}
	if legacy.Definition != nil {
		t.Fatalf("legacy task must load with nil Definition, got %+v", legacy.Definition)
	}
}

func TestMutateTaskDefine(t *testing.T) {
	t.Parallel()
	b := newDefineTestBroker(t)
	created, err := b.MutateTask(TaskPostRequest{
		Action: "create", Channel: "general", Title: "Launch the newsletter",
		Owner: "eng", CreatedBy: "ceo",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Missing goal → invalid.
	_, err = b.MutateTask(TaskPostRequest{
		Action: "define", ID: created.Task.ID, Channel: "general", CreatedBy: "ceo",
		Definition: &TaskDefinition{},
	})
	var mErr *TaskMutationError
	if !errors.As(err, &mErr) || mErr.Kind != TaskMutationInvalid {
		t.Fatalf("goal-less define: want invalid, got %v", err)
	}

	// Happy path: definition + verification in the same call.
	res, err := b.MutateTask(TaskPostRequest{
		Action: "define", ID: created.Task.ID, Channel: "general", CreatedBy: "ceo",
		Definition: &TaskDefinition{
			Goal:            "send the first newsletter",
			Deliverables:    []TaskDeliverable{{Name: "draft", Format: "markdown"}},
			SuccessCriteria: []string{"newsletter.md exists"},
			AccessNeeded:    []string{"mailing list"},
		},
		VerificationKind: "artifact", VerificationSpec: "newsletter.md", VerificationRequired: true,
	})
	if err != nil {
		t.Fatalf("define: %v", err)
	}
	if res.Task.Definition == nil || res.Task.Definition.Goal != "send the first newsletter" {
		t.Fatalf("definition not set: %+v", res.Task.Definition)
	}
	if res.Task.Definition.DefinedAt == "" {
		t.Fatal("DefinedAt not stamped")
	}
	if res.Task.Verification == nil || res.Task.Verification.Spec != "newsletter.md" {
		t.Fatalf("verification not set alongside define: %+v", res.Task.Verification)
	}

	// Re-define updates the definition but must NOT overwrite an
	// established verification gate.
	res, err = b.MutateTask(TaskPostRequest{
		Action: "define", ID: created.Task.ID, Channel: "general", CreatedBy: "ceo",
		Definition:       &TaskDefinition{Goal: "send the first newsletter to partners only"},
		VerificationKind: "command", VerificationSpec: "exit 1", VerificationRequired: true,
	})
	if err != nil {
		t.Fatalf("re-define: %v", err)
	}
	if res.Task.Definition.Goal != "send the first newsletter to partners only" {
		t.Fatalf("re-define did not update goal: %+v", res.Task.Definition)
	}
	if res.Task.Verification.Kind != "artifact" || res.Task.Verification.Spec != "newsletter.md" {
		t.Fatalf("re-define overwrote the existing verification: %+v", res.Task.Verification)
	}

	// Specialists cannot define — not even the task owner.
	_, err = b.MutateTask(TaskPostRequest{
		Action: "define", ID: created.Task.ID, Channel: "general", CreatedBy: "eng",
		Definition: &TaskDefinition{Goal: "owner rewrite"},
	})
	if !errors.As(err, &mErr) || mErr.Kind != TaskMutationForbidden {
		t.Fatalf("specialist define: want forbidden, got %v", err)
	}
}

// TestIntakeTaskArrivesPreDefined: the intake Spec maps into the structured
// Definition at task creation (problem → goal, acceptanceCriteria →
// success_criteria), so intake-created tasks carry the R4 contract without a
// separate define call.
func TestIntakeTaskArrivesPreDefined(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)
	body := `{
		"problem": "inbox triage is manual",
		"targetOutcome": "triage runs nightly",
		"acceptanceCriteria": [
			{"statement": "cron entry registered"},
			{"statement": "digest lands in #general"}
		],
		"assignment": "owner-agent picks up"
	}`
	provider := &fakeIntakeProvider{response: fenceJSON(body)}
	outcome, err := b.StartIntake(context.Background(), "automate inbox triage", provider)
	if err != nil {
		t.Fatalf("StartIntake: %v", err)
	}
	task := b.TaskByID(outcome.TaskID)
	if task == nil || task.Definition == nil {
		t.Fatalf("intake task must arrive pre-defined, got %+v", task)
	}
	if task.Definition.Goal != "inbox triage is manual" {
		t.Fatalf("goal mapping: got %q", task.Definition.Goal)
	}
	if len(task.Definition.SuccessCriteria) != 2 || task.Definition.SuccessCriteria[1] != "digest lands in #general" {
		t.Fatalf("success criteria mapping: %v", task.Definition.SuccessCriteria)
	}
	if task.Definition.DefinedAt == "" {
		t.Fatal("DefinedAt not stamped on intake mapping")
	}
}

// TestExecutionPacketCarriesDefinition: the work packet leads with the
// definition contract so the owner executes against goal / deliverable
// format / success criteria / access.
func TestExecutionPacketCarriesDefinition(t *testing.T) {
	t.Parallel()
	b := newDefineTestBroker(t)
	created, err := b.MutateTask(TaskPostRequest{
		Action: "create", Channel: "general", Title: "Launch the newsletter",
		Owner: "eng", CreatedBy: "ceo",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := b.MutateTask(TaskPostRequest{
		Action: "define", ID: created.Task.ID, Channel: "general", CreatedBy: "ceo",
		Definition: &TaskDefinition{
			Goal:            "first partner newsletter shipped",
			Deliverables:    []TaskDeliverable{{Name: "draft", Format: "markdown in the wiki"}},
			SuccessCriteria: []string{"human approved the draft"},
			AccessNeeded:    []string{"mailing-list account"},
		},
	}); err != nil {
		t.Fatalf("define: %v", err)
	}

	l := launcherForBrokerFixture(b)
	packet := l.notifyCtx().BuildTaskExecutionPacket("eng", officeActionLog{Actor: "ceo"}, *b.TaskByID(created.Task.ID), "Task assigned to you.")
	for _, want := range []string{
		"DEFINITION (the contract you execute against):",
		"Goal: first partner newsletter shipped",
		"draft (format: markdown in the wiki)",
		"1. human approved the draft",
		"Access needed: mailing-list account",
	} {
		if !strings.Contains(packet, want) {
			t.Fatalf("packet missing %q:\n%s", want, packet)
		}
	}
}
