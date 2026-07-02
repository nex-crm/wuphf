package team

import (
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/onboarding"
)

// The wizard's contract since the packs/CEO removal: blueprint "" plus an
// explicit empty agents list seeds an office with NO agents. No synthesis, no
// DefaultManifest fallback, no lead to tag. These tests pin that contract; the
// legacy nil-agents synthesis path keeps its own coverage in
// broker_onboarding_wiki_test.go.

func TestOnboardingCompleteNoTeamSeedsEmptyOffice(t *testing.T) {
	ensureOperationsFallbackFS(t)
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("WUPHF_RUNTIME_HOME", tmpHome)

	b := newTestBroker(t)
	if err := b.onboardingCompleteFn("Audit our CRM for duplicate accounts", false, "", []string{}, "Dunder HQ"); err != nil {
		t.Fatalf("onboardingCompleteFn: %v", err)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.members) != 0 {
		slugs := make([]string, 0, len(b.members))
		for _, m := range b.members {
			slugs = append(slugs, m.Slug)
		}
		t.Fatalf("expected zero members in a no-team seed, got %v", slugs)
	}

	var origin *channelMessage
	for i := range b.messages {
		if b.messages[i].Kind == "onboarding_origin" {
			origin = &b.messages[i]
		}
	}
	if origin == nil {
		t.Fatal("expected an onboarding_origin message in #general")
	}
	if origin.Channel != "general" {
		t.Fatalf("expected the first workflow in #general, got %q", origin.Channel)
	}
	if len(origin.Tagged) != 0 {
		t.Fatalf("expected the first workflow untagged (no lead exists), got tags %v", origin.Tagged)
	}
}

func TestOnboardingCompleteNoTeamSkipTaskPostsEmptyOfficeWelcome(t *testing.T) {
	ensureOperationsFallbackFS(t)
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("WUPHF_RUNTIME_HOME", tmpHome)

	b := newTestBroker(t)
	if err := b.onboardingCompleteFn("", true, "", []string{}, ""); err != nil {
		t.Fatalf("onboardingCompleteFn: %v", err)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.members) != 0 {
		t.Fatalf("expected zero members in a no-team skip seed, got %d", len(b.members))
	}
	found := false
	for i := range b.messages {
		if b.messages[i].Kind == "system" && b.messages[i].Content == emptyOfficeWelcome {
			found = true
		}
		if strings.Contains(b.messages[i].Content, "lead only") {
			t.Fatalf("no-team seed must not post the lead-only notice, got %q", b.messages[i].Content)
		}
	}
	if !found {
		t.Fatal("expected the empty-office welcome message in #general")
	}
}

// An onboarded office with zero members is intentional, not corrupted state:
// the load-time recovery hook must not resurrect the DefaultManifest roster.
func TestEnsureDefaultOfficeMembersSkipsOnboardedEmptyOffice(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("WUPHF_RUNTIME_HOME", tmpHome)

	if err := onboarding.Save(&onboarding.State{
		CompletedAt: "2026-07-01T00:00:00Z",
		Version:     2,
		Phase:       "complete",
	}); err != nil {
		t.Fatalf("save onboarding state: %v", err)
	}

	b := newTestBroker(t)
	b.mu.Lock()
	b.members = nil
	b.ensureDefaultOfficeMembersLocked()
	got := len(b.members)
	b.mu.Unlock()

	if got != 0 {
		t.Fatalf("expected the onboarded empty office to stay empty, got %d default members", got)
	}
}

// The load-time normalization must not resurrect built-ins either: the Pam /
// App Builder back-fill and the ceo channel pin only apply to rosters that
// have agents. An intentionally empty office survives a broker restart empty.
func TestNormalizeLoadedStateKeepsEmptyOfficeEmpty(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("WUPHF_RUNTIME_HOME", tmpHome)

	if err := onboarding.Save(&onboarding.State{
		CompletedAt: "2026-07-01T00:00:00Z",
		Version:     2,
		Phase:       "complete",
	}); err != nil {
		t.Fatalf("save onboarding state: %v", err)
	}

	b := newTestBroker(t)
	b.mu.Lock()
	b.members = nil
	b.channels = []teamChannel{{
		Slug:        "general",
		Name:        "general",
		Description: "Primary coordination channel.",
		Members:     []string{},
	}}
	b.ensureDefaultOfficeMembersLocked()
	b.normalizeLoadedStateLocked()
	members := len(b.members)
	var generalMembers []string
	for _, ch := range b.channels {
		if ch.Slug == "general" {
			generalMembers = ch.Members
		}
	}
	b.mu.Unlock()

	if members != 0 {
		t.Fatalf("expected the empty office to stay empty across normalization, got %d members", members)
	}
	for _, slug := range generalMembers {
		if slug == "ceo" {
			t.Fatal("expected no ceo ghost pinned into #general membership")
		}
	}
}

// Before onboarding completes, zero members still means corrupted/never-seeded
// state and the recovery roster must keep firing.
func TestEnsureDefaultOfficeMembersStillRecoversPreOnboarding(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("WUPHF_RUNTIME_HOME", tmpHome)

	b := newTestBroker(t)
	b.mu.Lock()
	b.members = nil
	b.ensureDefaultOfficeMembersLocked()
	got := len(b.members)
	b.mu.Unlock()

	if got == 0 {
		t.Fatal("expected the recovery roster for a never-onboarded zero-member office")
	}
}
