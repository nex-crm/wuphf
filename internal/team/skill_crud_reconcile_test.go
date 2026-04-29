package team

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestReconcileSkillStatusFromDisk_OwnerAgents pins down PR 7 finding F4:
// reconcileSkillStatusFromDisk must restore OwnerAgents from the on-disk
// SKILL.md frontmatter, not just Status. Without this, manual edits to disk
// (or a restored backup) would lose scope info silently after a broker
// restart.
//
// Setup: write a SKILL.md whose frontmatter declares OwnerAgents=[deploy-bot]
// + status=active. Seed b.skills with the same skill but stale state
// (different OwnerAgents and a different status). Run reconcile. Both fields
// must match disk after the call.
func TestReconcileSkillStatusFromDisk_OwnerAgents(t *testing.T) {
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("repo init: %v", err)
	}
	b := newTestBroker(t)
	worker := NewWikiWorker(repo, b)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)
	b.mu.Lock()
	b.wikiWorker = worker
	b.mu.Unlock()

	// Author a SKILL.md on disk with the canonical (post-edit) state.
	fm := SkillFrontmatter{
		Name:        "deploy-frontend",
		Description: "Ship a hotfix release.",
		Version:     "1.0.0",
		License:     "MIT",
		Metadata: SkillMetadata{
			Wuphf: SkillWuphfMeta{
				Status:      "disabled",
				OwnerAgents: []string{"deploy-bot"},
			},
		},
	}
	mdBytes, err := RenderSkillMarkdown(fm, "## Steps\n\n1. Ship.")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	skillPath := filepath.Join(root, "team", "skills")
	if err := os.MkdirAll(skillPath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillPath, "deploy-frontend.md"), mdBytes, 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	// Seed b.skills with the stale (pre-edit) state.
	b.mu.Lock()
	b.skills = append(b.skills, teamSkill{
		ID:          "skill-deploy-frontend",
		Name:        "deploy-frontend",
		Title:       "Deploy frontend",
		Status:      "active",        // stale
		OwnerAgents: []string{"csm"}, // stale
		Content:     "## Steps\n\n1. Ship.",
	})
	b.mu.Unlock()

	b.reconcileSkillStatusFromDisk()

	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(b.skills))
	}
	got := b.skills[0]
	if got.Status != "disabled" {
		t.Errorf("Status: got %q, want disabled", got.Status)
	}
	if len(got.OwnerAgents) != 1 || got.OwnerAgents[0] != "deploy-bot" {
		t.Errorf("OwnerAgents: got %v, want [deploy-bot]", got.OwnerAgents)
	}
}

// TestOwnerAgentsEqual locks the helper used by reconcile to detect drift.
// Order is significant — disk's order is authoritative, and a permutation
// should trigger a reconcile so the in-memory list stays canonical.
func TestOwnerAgentsEqual(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a, b []string
		want bool
	}{
		{name: "both nil", a: nil, b: nil, want: true},
		{name: "both empty slice", a: []string{}, b: []string{}, want: true},
		{name: "nil and empty are equal", a: nil, b: []string{}, want: true},
		{name: "same order same values", a: []string{"a", "b"}, b: []string{"a", "b"}, want: true},
		{name: "different order", a: []string{"a", "b"}, b: []string{"b", "a"}, want: false},
		{name: "different lengths", a: []string{"a"}, b: []string{"a", "b"}, want: false},
		{name: "different values", a: []string{"a"}, b: []string{"b"}, want: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ownerAgentsEqual(tc.a, tc.b); got != tc.want {
				t.Errorf("ownerAgentsEqual(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}
