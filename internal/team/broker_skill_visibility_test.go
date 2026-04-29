package team

import (
	"encoding/json"
	"testing"
)

// TestCanAgentSeeSkill locks in the canonical visibility predicate for
// per-agent skill scoping (PR 7).
//
// Visibility rule:
//   - slug (case-insensitive, trimmed) is in sk.OwnerAgents -> visible
//   - sk.OwnerAgents empty AND slug == office lead -> visible (back-compat)
//   - otherwise -> not visible
//
// Status is intentionally orthogonal — archived/disabled skills stay visible
// to their owners; status filtering happens in listSkillsForAgentLocked.
func TestCanAgentSeeSkill(t *testing.T) {
	t.Parallel()

	type call struct {
		slug    string
		owners  []string
		status  string
		want    bool
		message string
	}

	// All cases run on the same broker, with members={ceo (lead), csm, eng}.
	tests := []call{
		{slug: "csm", owners: []string{"csm"}, status: "active", want: true, message: "owner sees own skill"},
		{slug: "csm", owners: []string{"csm", "eng"}, status: "active", want: true, message: "first of multi-owner sees skill"},
		{slug: "eng", owners: []string{"csm", "eng"}, status: "active", want: true, message: "second of multi-owner sees skill"},
		{slug: "eng", owners: []string{"csm"}, status: "active", want: false, message: "non-owner does not see csm-only skill"},
		{slug: "ceo", owners: nil, status: "active", want: true, message: "lead sees lead-routable empty-owners skill"},
		{slug: "csm", owners: nil, status: "active", want: false, message: "non-lead does not see lead-routable skill"},
		{slug: "ceo", owners: []string{"csm"}, status: "active", want: false, message: "lead does not auto-see csm-scoped skill"},
		{slug: "CSM", owners: []string{"csm"}, status: "active", want: true, message: "case-insensitive uppercase slug"},
		{slug: "csm", owners: []string{"CSM"}, status: "active", want: true, message: "case-insensitive uppercase owner"},
		{slug: "  csm  ", owners: []string{"csm"}, status: "active", want: true, message: "whitespace in slug is trimmed"},
		{slug: "csm", owners: []string{"  csm  "}, status: "active", want: true, message: "whitespace in owner is trimmed"},
		{slug: "", owners: []string{"csm"}, status: "active", want: false, message: "empty slug never sees anything"},
		{slug: "   ", owners: nil, status: "active", want: false, message: "blank slug not treated as lead"},
		{slug: "csm", owners: []string{"csm"}, status: "archived", want: true, message: "archived skill still visible to owner (status orthogonal)"},
		{slug: "csm", owners: []string{"csm"}, status: "disabled", want: true, message: "disabled skill still visible to owner (status orthogonal)"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.message, func(t *testing.T) {
			t.Parallel()

			b := newTestBroker(t)
			b.mu.Lock()
			b.members = []officeMember{
				{Slug: "ceo", BuiltIn: true},
				{Slug: "csm"},
				{Slug: "eng"},
			}
			sk := &teamSkill{
				Name:        "test-skill",
				Status:      tc.status,
				OwnerAgents: tc.owners,
			}
			got := b.canAgentSeeSkillLocked(tc.slug, sk)
			b.mu.Unlock()

			if got != tc.want {
				t.Errorf("canAgentSeeSkillLocked(slug=%q, owners=%v, status=%q): got %v, want %v",
					tc.slug, tc.owners, tc.status, got, tc.want)
			}
		})
	}

	t.Run("nil skill returns false", func(t *testing.T) {
		t.Parallel()
		b := newTestBroker(t)
		b.mu.Lock()
		defer b.mu.Unlock()
		if b.canAgentSeeSkillLocked("ceo", nil) {
			t.Error("nil skill must not be visible to anyone")
		}
	})
}

// TestListSkillsForAgentLocked_StableSort guards the cache-stability
// invariant: identical b.skills snapshots must produce byte-identical output
// across calls so the per-agent catalog injected into buildPrompt stays
// prompt-cacheable.
func TestListSkillsForAgentLocked_StableSort(t *testing.T) {
	t.Parallel()

	b := newTestBroker(t)
	b.mu.Lock()
	b.members = []officeMember{{Slug: "ceo", BuiltIn: true}, {Slug: "csm"}}
	// Insert in non-sorted order on purpose.
	b.skills = []teamSkill{
		{Name: "zeta-skill", Status: "active", OwnerAgents: []string{"csm"}},
		{Name: "alpha-skill", Status: "active", OwnerAgents: []string{"csm"}},
		{Name: "mid-skill", Status: "active", OwnerAgents: []string{"csm"}},
	}
	first := b.listSkillsForAgentLocked("csm", listSkillsOpts{activeOnly: true})
	second := b.listSkillsForAgentLocked("csm", listSkillsOpts{activeOnly: true})
	b.mu.Unlock()

	if len(first) != 3 {
		t.Fatalf("expected 3 visible skills, got %d", len(first))
	}
	wantOrder := []string{"alpha-skill", "mid-skill", "zeta-skill"}
	for i, want := range wantOrder {
		if first[i].Name != want {
			t.Errorf("first[%d].Name = %q, want %q", i, first[i].Name, want)
		}
	}
	a, _ := json.Marshal(first)
	bj, _ := json.Marshal(second)
	if string(a) != string(bj) {
		t.Errorf("listSkillsForAgentLocked output is not byte-stable across calls:\n  first:  %s\n  second: %s", a, bj)
	}
}

// TestListSkillsForAgentLocked_ActiveOnlyFilter checks the activeOnly opts
// gate plus the visibility intersection.
func TestListSkillsForAgentLocked_ActiveOnlyFilter(t *testing.T) {
	t.Parallel()

	b := newTestBroker(t)
	b.mu.Lock()
	defer b.mu.Unlock()

	b.members = []officeMember{{Slug: "ceo", BuiltIn: true}, {Slug: "csm"}, {Slug: "eng"}}
	b.skills = []teamSkill{
		{Name: "csm-active", Status: "active", OwnerAgents: []string{"csm"}},
		{Name: "csm-archived", Status: "archived", OwnerAgents: []string{"csm"}},
		{Name: "csm-proposed", Status: "proposed", OwnerAgents: []string{"csm"}},
		{Name: "csm-disabled", Status: "disabled", OwnerAgents: []string{"csm"}},
		{Name: "eng-active", Status: "active", OwnerAgents: []string{"eng"}},
		{Name: "lead-routable", Status: "active", OwnerAgents: nil},
	}

	t.Run("activeOnly=true filters non-active", func(t *testing.T) {
		got := b.listSkillsForAgentLocked("csm", listSkillsOpts{activeOnly: true})
		if len(got) != 1 || got[0].Name != "csm-active" {
			names := make([]string, len(got))
			for i, sk := range got {
				names[i] = sk.Name
			}
			t.Errorf("activeOnly csm: got %v, want [csm-active]", names)
		}
	})

	t.Run("activeOnly=false returns every visible status", func(t *testing.T) {
		got := b.listSkillsForAgentLocked("csm", listSkillsOpts{activeOnly: false})
		want := []string{"csm-active", "csm-archived", "csm-disabled", "csm-proposed"}
		if len(got) != len(want) {
			names := make([]string, len(got))
			for i, sk := range got {
				names[i] = sk.Name
			}
			t.Fatalf("csm all-status: got %v, want %v", names, want)
		}
		for i, w := range want {
			if got[i].Name != w {
				t.Errorf("got[%d].Name = %q, want %q", i, got[i].Name, w)
			}
		}
	})

	t.Run("lead sees lead-routable plus their own scope", func(t *testing.T) {
		got := b.listSkillsForAgentLocked("ceo", listSkillsOpts{activeOnly: true})
		if len(got) != 1 || got[0].Name != "lead-routable" {
			names := make([]string, len(got))
			for i, sk := range got {
				names[i] = sk.Name
			}
			t.Errorf("ceo activeOnly: got %v, want [lead-routable]", names)
		}
	})

	t.Run("non-owner sees nothing they don't own", func(t *testing.T) {
		got := b.listSkillsForAgentLocked("eng", listSkillsOpts{activeOnly: true})
		if len(got) != 1 || got[0].Name != "eng-active" {
			names := make([]string, len(got))
			for i, sk := range got {
				names[i] = sk.Name
			}
			t.Errorf("eng activeOnly: got %v, want [eng-active]", names)
		}
	})

	t.Run("empty b.skills returns empty slice (not nil)", func(t *testing.T) {
		b.skills = nil
		got := b.listSkillsForAgentLocked("csm", listSkillsOpts{activeOnly: true})
		if got == nil {
			t.Error("expected non-nil empty slice for stable JSON encoding")
		}
		if len(got) != 0 {
			t.Errorf("expected empty result, got %d entries", len(got))
		}
	})
}
