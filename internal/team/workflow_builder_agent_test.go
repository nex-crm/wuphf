package team

import (
	"strings"
	"testing"
)

// TestWorkflowBuilderIsBuiltInDefaultMember: the Workflow Builder (slug
// "workflow-builder", name "Darryl", role "Workflow Builder") is a built-in
// member of the default roster, like the CEO and the Librarian.
func TestWorkflowBuilderIsBuiltInDefaultMember(t *testing.T) {
	members := defaultOfficeMembers()
	var wb *officeMember
	for i := range members {
		if isWorkflowBuilderSlug(members[i].Slug) {
			wb = &members[i]
			break
		}
	}
	if wb == nil {
		t.Fatalf("workflow-builder missing from defaultOfficeMembers: %+v", members)
	}
	if !wb.BuiltIn {
		t.Errorf("workflow-builder must be BuiltIn")
	}
	if wb.Name != workflowBuilderName || wb.Role != workflowBuilderRole {
		t.Errorf("workflow-builder persona = %q/%q, want %q/%q", wb.Name, wb.Role, workflowBuilderName, workflowBuilderRole)
	}
}

// TestEnsureWorkflowBuilderMemberIdempotent: appending the builder to a roster
// that already has it is a no-op (no duplicate), and adds it exactly once when
// absent.
func TestEnsureWorkflowBuilderMemberIdempotent(t *testing.T) {
	base := []officeMember{{Slug: "ceo"}, {Slug: "growth"}}
	once := ensureWorkflowBuilderMember(base)
	if got := countSlug(once, WorkflowBuilderSlug); got != 1 {
		t.Fatalf("expected exactly 1 workflow-builder after first ensure, got %d", got)
	}
	twice := ensureWorkflowBuilderMember(once)
	if got := countSlug(twice, WorkflowBuilderSlug); got != 1 {
		t.Fatalf("expected ensure to be idempotent, got %d workflow-builder members", got)
	}
}

// TestIsWorkflowBuilderSlug: slug match is case-insensitive and trims space, so
// gating in the prompt builder and MCP server never misses on whitespace drift.
func TestIsWorkflowBuilderSlug(t *testing.T) {
	for _, ok := range []string{"workflow-builder", "Workflow-Builder", "  workflow-builder  "} {
		if !isWorkflowBuilderSlug(ok) {
			t.Errorf("isWorkflowBuilderSlug(%q) = false, want true", ok)
		}
	}
	for _, no := range []string{"", "workflow", "builder", "librarian", "ceo"} {
		if isWorkflowBuilderSlug(no) {
			t.Errorf("isWorkflowBuilderSlug(%q) = true, want false", no)
		}
	}
}

// TestWorkflowDelegationBlockExcludesBuilder: every non-builder agent prompt
// carries the delegation contract telling it to hand workflow work to
// @workflow-builder; the builder's own prompt carries the authority block
// instead and is NOT told to delegate to itself.
func TestWorkflowDelegationBlockExcludesBuilder(t *testing.T) {
	pb := &promptBuilder{
		isOneOnOne:  func() bool { return false },
		isFocusMode: func() bool { return false },
		packName:    func() string { return "Test Office" },
		leadSlug:    func() string { return "ceo" },
		members: func() []officeMember {
			return []officeMember{
				{Slug: "ceo", Name: "CEO", Role: "ceo"},
				{Slug: WorkflowBuilderSlug, Name: workflowBuilderName, Role: workflowBuilderRole},
			}
		},
		policies:       func() []officePolicy { return nil },
		nameFor:        func(slug string) string { return slug },
		markdownMemory: true,
	}

	ceoPrompt := pb.Build("ceo")
	if !strings.Contains(ceoPrompt, "HAND OFF TO @workflow-builder") {
		t.Errorf("CEO prompt must include the workflow delegation block")
	}

	builderPrompt := pb.Build(WorkflowBuilderSlug)
	if strings.Contains(builderPrompt, "HAND OFF TO @workflow-builder") {
		t.Errorf("workflow-builder must NOT be told to delegate to itself")
	}
	if !strings.Contains(builderPrompt, "WORKFLOW OWNERSHIP (you are the Workflow Builder)") {
		t.Errorf("workflow-builder prompt must include its authority block")
	}
}

func countSlug(members []officeMember, slug string) int {
	n := 0
	for i := range members {
		if members[i].Slug == slug {
			n++
		}
	}
	return n
}
