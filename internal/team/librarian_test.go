package team

import (
	"testing"

	"github.com/nex-crm/wuphf/internal/operations"
)

// TestLibrarianIsBuiltInDefaultMember: the Librarian (slug "librarian", name
// "Pam", role "Librarian") is a built-in member of the default roster.
func TestLibrarianIsBuiltInDefaultMember(t *testing.T) {
	members := defaultOfficeMembers()
	var lib *officeMember
	for i := range members {
		if isLibrarianSlug(members[i].Slug) {
			lib = &members[i]
			break
		}
	}
	if lib == nil {
		t.Fatalf("librarian missing from defaultOfficeMembers: %+v", members)
	}
	if !lib.BuiltIn {
		t.Errorf("librarian must be BuiltIn")
	}
	if lib.Name != librarianName || lib.Role != librarianRole {
		t.Errorf("librarian persona = %q/%q, want %q/%q", lib.Name, lib.Role, librarianName, librarianRole)
	}
}

// TestLibrarianSeededIntoTaskChannel: every task that mints its own channel
// seeds the Librarian as a member (D5: owner + CEO + Librarian).
func TestLibrarianSeededIntoTaskChannel(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", LibrarianSlug, librarianName)
	ensureTestMemberAccess(b, "general", "eng", "Engineer")

	task, _, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "general",
		Title:         "Build the thing",
		Owner:         "eng",
		CreatedBy:     "ceo",
		TaskType:      "feature",
		ExecutionMode: "office",
	})
	if err != nil {
		t.Fatalf("ensure task: %v", err)
	}
	if task.Channel == "general" {
		t.Fatalf("expected task to mint its own channel, stayed in general")
	}

	b.mu.Lock()
	hasLibrarian := b.channelHasMemberLocked(task.Channel, LibrarianSlug)
	hasOwner := b.channelHasMemberLocked(task.Channel, "eng")
	b.mu.Unlock()
	if !hasLibrarian {
		t.Errorf("expected librarian seeded into task channel %q", task.Channel)
	}
	if !hasOwner {
		t.Errorf("expected owner seeded into task channel %q", task.Channel)
	}
}

// TestLibrarianTaskChannelSeedNoopsWithoutMember: in a workspace that has no
// Librarian member yet (e.g. a legacy workspace before the Phase 6 migration),
// task-channel seeding must NOT add a phantom "librarian" member.
func TestLibrarianTaskChannelSeedNoopsWithoutMember(t *testing.T) {
	b := newTestBroker(t)
	// Simulate a legacy workspace: a roster with one member and NO librarian
	// (as a pre-Phase-4 broker-state.json would load). Overwrite the
	// auto-seeded default roster so findMemberLocked("librarian") is nil.
	b.mu.Lock()
	b.members = []officeMember{{Slug: "eng", Name: "Engineer", Role: "Engineer"}}
	b.memberIndex = nil
	b.channels = []teamChannel{{Slug: "general", Name: "general", Members: []string{"eng"}}}
	b.mu.Unlock()

	task, _, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "general",
		Title:         "Legacy task",
		Owner:         "eng",
		CreatedBy:     "ceo",
		TaskType:      "feature",
		ExecutionMode: "office",
	})
	if err != nil {
		t.Fatalf("ensure task: %v", err)
	}
	b.mu.Lock()
	hasLibrarian := b.channelHasMemberLocked(task.Channel, LibrarianSlug)
	b.mu.Unlock()
	if hasLibrarian {
		t.Errorf("did not expect a phantom librarian member when none is registered")
	}
}

// TestLibrarianAwareReviewer: the Librarian becomes the default wiki reviewer
// when the base resolver yields its "ceo" fallback and a Librarian exists; a
// blueprint-configured reviewer is respected; and without a Librarian member
// the fallback is unchanged (legacy-safe).
func TestLibrarianAwareReviewer(t *testing.T) {
	withLib := newTestBroker(t) // auto-seeds the built-in Librarian
	if got := librarianAwareReviewer(withLib, func(string) string { return operations.ReviewerFallback })("team/x.md"); got != LibrarianSlug {
		t.Errorf("fallback reviewer with librarian present = %q, want %q", got, LibrarianSlug)
	}
	if got := librarianAwareReviewer(withLib, func(string) string { return "pinned-reviewer" })("team/x.md"); got != "pinned-reviewer" {
		t.Errorf("configured reviewer = %q, want pinned-reviewer (respected)", got)
	}

	noLib := newTestBroker(t)
	noLib.mu.Lock()
	noLib.members = []officeMember{{Slug: "ceo", Name: "CEO"}}
	noLib.memberIndex = nil
	noLib.mu.Unlock()
	if got := librarianAwareReviewer(noLib, func(string) string { return operations.ReviewerFallback })("team/x.md"); got != operations.ReviewerFallback {
		t.Errorf("fallback reviewer without librarian = %q, want %q (legacy-safe)", got, operations.ReviewerFallback)
	}
}

// TestLibrarianHasCrossChannelAccess: the Librarian, like the CEO, can access
// any channel (for wiki curation context).
func TestLibrarianHasCrossChannelAccess(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.canAccessChannelLocked(LibrarianSlug, "some-channel-it-is-not-a-member-of") {
		t.Fatalf("librarian should have cross-channel access like the CEO")
	}
}
