package team

import (
	"strings"
	"testing"
)

func draftableTask(id, title string) teamTask {
	return teamTask{
		ID:       id,
		Title:    title,
		Owner:    "eng",
		Artifact: "team/reports/out.md",
		Definition: &TaskDefinition{
			Goal: "Get the weekly investor update out to the approved list",
			Deliverables: []TaskDeliverable{
				{Name: "investor update", Format: "markdown in the wiki"},
			},
			SuccessCriteria: []string{
				"Update published to the wiki",
				"Email sent to the investor list",
			},
		},
	}
}

func TestShouldDraftPlaybook(t *testing.T) {
	task := draftableTask("task-1", "Send the weekly investor update")
	if !shouldDraftPlaybook(task) {
		t.Fatal("task with 2 success criteria must qualify")
	}

	oneCriterion := task
	oneCriterion.Definition = &TaskDefinition{
		Goal:            task.Definition.Goal,
		SuccessCriteria: []string{"only one", "   "},
	}
	if shouldDraftPlaybook(oneCriterion) {
		t.Fatal("one non-empty criterion must NOT qualify (blank entries don't count)")
	}

	noDefinition := task
	noDefinition.Definition = nil
	if shouldDraftPlaybook(noDefinition) {
		t.Fatal("a task without a Definition must NOT qualify")
	}
}

func TestBuildPlaybookDraftArticle_SkeletonAndCitations(t *testing.T) {
	task := draftableTask("task-7", "Send the weekly investor update")
	task.Ledger = []TaskLedgerEntry{
		{Agent: "eng", Said: "Pulled the metrics and drafted the update."},
		{Agent: "eng", Said: ""},
		{Agent: "eng", Said: "Sent via the approved mailing list."},
	}
	got := buildPlaybookDraftArticle(task, "send-the-weekly-investor-update")

	for _, want := range []string{
		"---\ndraft: true\n---",
		playbookDraftMarker,
		"## Purpose",
		"Get the weekly investor update out to the approved list[^1]",
		"## Checklist",
		"- [ ] Update published to the wiki",
		"- [ ] Email sent to the investor list",
		"## Steps",
		"1. Pulled the metrics and drafted the update.",
		"2. Sent via the approved mailing list.",
		"## Worked examples",
		"- Task task-7",
		"[team/reports/out.md](team/reports/out.md)",
		"## References",
		"[^1]: Task task-7 — artifact: [team/reports/out.md](team/reports/out.md); completed by eng.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("draft missing %q:\n%s", want, got)
		}
	}
}

func TestBuildPlaybookDraftArticle_StepsFallBackToDeliverables(t *testing.T) {
	task := draftableTask("task-8", "Send the weekly investor update")
	got := buildPlaybookDraftArticle(task, "send-the-weekly-investor-update")
	if !strings.Contains(got, "1. Produce the investor update (markdown in the wiki).") {
		t.Fatalf("expected deliverable-derived step skeleton:\n%s", got)
	}
}

func TestAppendPlaybookWorkedExample_AppendsAndStaysIdempotent(t *testing.T) {
	first := draftableTask("task-1", "Send the weekly investor update")
	existing := buildPlaybookDraftArticle(first, "send-the-weekly-investor-update")

	second := draftableTask("task-2", "Send the weekly investor update for week 24")
	second.Artifact = "team/reports/out-24.md"
	updated := appendPlaybookWorkedExample(existing, second)

	if !strings.Contains(updated, "- Task task-2") || !strings.Contains(updated, "[^2]") {
		t.Fatalf("expected second worked example with footnote 2:\n%s", updated)
	}
	if !strings.Contains(updated, "[^2]: Task task-2 — artifact: [team/reports/out-24.md](team/reports/out-24.md); completed by eng.") {
		t.Fatalf("expected second citation in References:\n%s", updated)
	}
	// Ordering: the new bullet lands inside Worked examples (before
	// References), not dangling at the end of the file.
	if strings.Index(updated, "- Task task-2") > strings.Index(updated, "## References") {
		t.Fatalf("worked example must land inside its section:\n%s", updated)
	}

	// Idempotent: replaying the same task changes nothing.
	if again := appendPlaybookWorkedExample(updated, second); again != updated {
		t.Fatalf("replay must be a no-op")
	}
}

func TestAppendPlaybookWorkedExample_CreatesSectionsOnHumanPlaybook(t *testing.T) {
	human := "# Investor updates\n\nSome curated playbook prose without machine sections.\n"
	task := draftableTask("task-3", "Send the weekly investor update")
	updated := appendPlaybookWorkedExample(human, task)
	for _, want := range []string{"## Worked examples", "- Task task-3", "## References", "[^1]: Task task-3"} {
		if !strings.Contains(updated, want) {
			t.Fatalf("expected %q appended to human playbook:\n%s", want, updated)
		}
	}
}
