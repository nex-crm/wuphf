package team

// broker_task_wiki_refs_test.go proves the task->wiki-article backing link that
// the Slack context-packer reads for the first-party "task-linked wiki refs"
// tier: the WikiRefs field round-trips through the wire, and the create path
// trims/normalizes/dedupes the linked set.

import (
	"strings"
	"testing"
)

func TestWikiRefsRoundTripsThroughWire(t *testing.T) {
	original := teamTask{
		ID:       "task-1",
		Title:    "demo",
		WikiRefs: []string{"team/playbooks/onboarding.md", "team/policies/refunds.md"},
	}
	data, err := original.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if want := `"wiki_refs":[`; !strings.Contains(string(data), want) {
		t.Fatalf("marshalled task missing %s; got %s", want, string(data))
	}
	var decoded teamTask
	if err := decoded.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if len(decoded.WikiRefs) != 2 || decoded.WikiRefs[0] != "team/playbooks/onboarding.md" {
		t.Fatalf("round-trip WikiRefs = %v, want the two linked paths", decoded.WikiRefs)
	}
}

func TestWikiRefsOmittedWhenEmpty(t *testing.T) {
	// The default is "no wiki egress": a task with no links emits no wire key.
	data, err := teamTask{ID: "task-2", Title: "no links"}.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if strings.Contains(string(data), "wiki_refs") {
		t.Fatalf("empty WikiRefs should be omitted, got %s", string(data))
	}
}

func TestMutateTaskCreateDedupesWikiRefs(t *testing.T) {
	b := newTestBroker(t)
	b.channels = []teamChannel{
		{Slug: "general", Name: "general", Members: []string{"ceo"}},
	}

	created, err := b.MutateTask(TaskPostRequest{
		Action:    "create",
		Channel:   "general",
		Title:     "Reconcile invoices",
		Owner:     "alice",
		CreatedBy: "ceo",
		// Duplicates (incl. after trimming) and blanks: dedupePaths normalizes.
		WikiRefs: []string{
			"team/playbooks/billing.md",
			"  team/playbooks/billing.md  ",
			"",
			"team/policies/refunds.md",
		},
	})
	if err != nil {
		t.Fatalf("MutateTask create: %v", err)
	}
	got := created.Task.WikiRefs
	want := []string{"team/playbooks/billing.md", "team/policies/refunds.md"}
	if len(got) != len(want) {
		t.Fatalf("WikiRefs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("WikiRefs[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}
