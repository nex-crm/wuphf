package team

import (
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/workspaces"
)

func TestDeriveIDPrefix(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Nex", "NEX"},
		{"Acme Corp", "ACMEC"},
		{"a.b.c", "ABC"},
		{"  ", defaultIDPrefix},
		{"!@#", defaultIDPrefix},
		{"", defaultIDPrefix},
	}
	for _, c := range cases {
		if got := deriveIDPrefix(c.in); got != c.want {
			t.Errorf("deriveIDPrefix(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestWorkspaceIDPrefix(t *testing.T) {
	cases := []struct {
		name          string
		company       string
		workspaceName string
		want          string
	}{
		{"company name wins when set", "Nex", "acme-revops", "NEX"},
		{"blank company falls back to workspace name", "", "acme-revops", "ACMER"},
		{"symbol-only company falls back to workspace name", "!!!", "beta-team", "BETAT"},
		{"both blank yields empty", "", "", ""},
		{"symbol-only company and blank name yields empty", "###", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := workspaceIDPrefix(c.company, c.workspaceName); got != c.want {
				t.Errorf("workspaceIDPrefix(%q, %q) = %q, want %q",
					c.company, c.workspaceName, got, c.want)
			}
		})
	}
}

// TestRefreshIDPrefixUsesWorkspaceNameWhenCompanyNameEmpty pins the bug fix:
// a workspace that has a Name but no CompanyName must mint task ids from its
// own name (e.g. "acme-revops" → ACMER-1) instead of the shared OFFICE-N
// default. This drives the live refresh path against a seeded registry.
func TestRefreshIDPrefixUsesWorkspaceNameWhenCompanyNameEmpty(t *testing.T) {
	home := t.TempDir()
	// spacesDir() reads the real HOME; RuntimeHomeDir() reads
	// WUPHF_RUNTIME_HOME. Point both at the temp dir so the seeded
	// registry row matches this broker's runtime home.
	t.Setenv("HOME", home)
	t.Setenv("WUPHF_RUNTIME_HOME", home)

	now := time.Now().UTC()
	reg := &workspaces.Registry{
		Version: 1,
		Workspaces: []*workspaces.Workspace{{
			Name:        "acme-revops",
			RuntimeHome: home,
			State:       workspaces.StateRunning,
			CreatedAt:   now,
			LastUsedAt:  now,
			// CompanyName intentionally left empty — the workspace was
			// created without a brand name, the exact "all OFFICE-N" case.
		}},
	}
	if err := workspaces.Write(reg); err != nil {
		t.Fatalf("seed registry: %v", err)
	}

	b := &Broker{}
	b.mu.Lock()
	b.refreshIDPrefixFromWorkspaceLocked()
	b.mu.Unlock()

	if got := b.idPrefix; got != "ACMER" {
		t.Fatalf("IDPrefix = %q, want ACMER (derived from workspace name)", got)
	}

	b.mu.Lock()
	b.counter = 1
	id := b.allocateIssueIDLocked()
	b.mu.Unlock()
	if id != "ACMER-1" {
		t.Fatalf("allocateIssueIDLocked = %q, want ACMER-1", id)
	}
}

// TestRefreshIDPrefixCompanyNameWins confirms an explicit company name still
// takes precedence over the workspace name, preserving the "Nex" → NEX-1
// behaviour for onboarded workspaces.
func TestRefreshIDPrefixCompanyNameWins(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("WUPHF_RUNTIME_HOME", home)

	now := time.Now().UTC()
	reg := &workspaces.Registry{
		Version: 1,
		Workspaces: []*workspaces.Workspace{{
			Name:        "acme-revops",
			RuntimeHome: home,
			CompanyName: "Nex",
			State:       workspaces.StateRunning,
			CreatedAt:   now,
			LastUsedAt:  now,
		}},
	}
	if err := workspaces.Write(reg); err != nil {
		t.Fatalf("seed registry: %v", err)
	}

	b := &Broker{}
	b.mu.Lock()
	b.refreshIDPrefixFromWorkspaceLocked()
	b.mu.Unlock()

	if got := b.idPrefix; got != "NEX" {
		t.Fatalf("IDPrefix = %q, want NEX (company name should win)", got)
	}
}
