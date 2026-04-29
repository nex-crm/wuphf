package team

import "testing"

// TestHasSkillBodyShape locks in the body-shape gate that protects the
// explicit-frontmatter fast path from FAST_PATH_TRAP articles (D9).
// Articles with valid Anthropic frontmatter still need a recognisable
// skill body shape — section header + list/numbered steps — before the
// scanner promotes them without LLM judgment.
func TestHasSkillBodyShape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "steps + numbered list",
			body: "## Steps\n\n1. Do thing.\n2. Do other thing.",
			want: true,
		},
		{
			name: "how to + bullets",
			body: "## How to\n\n- Step one.\n- Step two.",
			want: true,
		},
		{
			name: "procedure + numbered",
			body: "## Procedure\n\n1. First.\n2. Second.",
			want: true,
		},
		{
			name: "runbook + numbered",
			body: "## Runbook\n\n1. Run.\n2. Verify.",
			want: true,
		},
		{
			name: "header but no list (bio prose)",
			body: "## Steps\n\nSome prose without numbered or bulleted items.",
			want: false,
		},
		{
			name: "list but no skill header (random notes)",
			body: "## Notes\n\n1. We talked about Q3.\n2. We talked about Q4.",
			want: false,
		},
		{
			name: "FAST_PATH_TRAP bio (D9)",
			body: "Jane joined the team in 2026 after a decade at Stripe.\nFavourite project: shipping Stripe's first multi-currency rail.",
			want: false,
		},
		{
			name: "FAST_PATH_TRAP decision log (D9)",
			body: "**Context.** Internal-only consumers.\n**Decision.** Ship REST.\n**Consequences.** Slower client iteration.",
			want: false,
		},
		{
			name: "marketing copy with Steps header but no list (D9)",
			body: "## Steps to better collaboration\n\nWUPHF helps your team move faster. Modern teams deserve modern tools.",
			want: false,
		},
		{
			name: "case-insensitive header match",
			body: "## STEPS\n\n1. Yell.",
			want: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := hasSkillBodyShape(tc.body); got != tc.want {
				t.Errorf("hasSkillBodyShape(%q): got %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// TestSpecToTeamSkill_OwnerAgentsRoundTrip locks in the per-agent skill scoping
// data field — owner_agents must travel from frontmatter through specToTeamSkill
// onto the teamSkill struct, mirroring the source_articles plumbing (D2).
func TestSpecToTeamSkill_OwnerAgentsRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		fm   SkillFrontmatter
		want []string
	}{
		{
			name: "owner agents from frontmatter",
			fm: SkillFrontmatter{
				Name:        "deploy-frontend",
				Description: "Ship a hotfix release.",
				Metadata: SkillMetadata{
					Wuphf: SkillWuphfMeta{
						OwnerAgents: []string{"deploy-bot", "ceo"},
					},
				},
			},
			want: []string{"deploy-bot", "ceo"},
		},
		{
			name: "empty owner agents = lead-routable",
			fm: SkillFrontmatter{
				Name:        "weekly-retro",
				Description: "Run the weekly retro.",
			},
			want: nil,
		},
		{
			name: "single owner",
			fm: SkillFrontmatter{
				Name:        "csm-followup",
				Description: "Follow up with a customer.",
				Metadata: SkillMetadata{
					Wuphf: SkillWuphfMeta{
						OwnerAgents: []string{"csm"},
					},
				},
			},
			want: []string{"csm"},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			spec := specToTeamSkill(tc.fm, "## Steps\n\n1. Do it.", "team/playbooks/x.md")

			if len(spec.OwnerAgents) != len(tc.want) {
				t.Fatalf("OwnerAgents len: got %d (%v), want %d (%v)", len(spec.OwnerAgents), spec.OwnerAgents, len(tc.want), tc.want)
			}
			for i, want := range tc.want {
				if spec.OwnerAgents[i] != want {
					t.Errorf("OwnerAgents[%d]: got %q, want %q", i, spec.OwnerAgents[i], want)
				}
			}

			// Mutating the spec slice must not bleed back into the source
			// frontmatter — defensive copy is required so downstream rewrites
			// can not accidentally edit the parsed frontmatter in place.
			if len(spec.OwnerAgents) > 0 && len(tc.fm.Metadata.Wuphf.OwnerAgents) > 0 {
				spec.OwnerAgents[0] = "MUTATED"
				if tc.fm.Metadata.Wuphf.OwnerAgents[0] == "MUTATED" {
					t.Error("specToTeamSkill must defensively copy OwnerAgents")
				}
			}
		})
	}
}

// TestInferOwnerAgentsFromPath covers the Stage A path-based default rule:
// per-agent notebook entries auto-scope to that agent; team/playbooks and
// team/customers stay lead-routable; explicit frontmatter always wins. The
// lead-routable case must return nil (not []string{}) so YAML omitempty
// drops the key on render.
func TestInferOwnerAgentsFromPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		relPath           string
		frontmatterOwners []string
		want              []string
	}{
		{
			name:    "agent notebook entry seeds owner from slug",
			relPath: "team/agents/csm/notebook/2026-04-22-followup.md",
			want:    []string{"csm"},
		},
		{
			name:    "deeply nested notebook entry still infers from slug",
			relPath: "team/agents/deploy-bot/notebook/deep/sub/incident.md",
			want:    []string{"deploy-bot"},
		},
		{
			name:    "playbook is lead-routable (nil)",
			relPath: "team/playbooks/retro.md",
			want:    nil,
		},
		{
			name:    "customer doc is lead-routable (nil)",
			relPath: "team/customers/acme/onboarding.md",
			want:    nil,
		},
		{
			name:    "agent profile (non-notebook) is lead-routable",
			relPath: "team/agents/csm/profile.md",
			want:    nil,
		},
		{
			name:    "mixed-case slug is normalised to lowercase",
			relPath: "team/agents/CSM/notebook/note.md",
			want:    []string{"csm"},
		},
		{
			name:              "frontmatter owners win over path inference",
			relPath:           "team/agents/csm/notebook/note.md",
			frontmatterOwners: []string{"deploy-bot", "ceo"},
			want:              []string{"deploy-bot", "ceo"},
		},
		{
			name:              "frontmatter owners win on a playbook path too",
			relPath:           "team/playbooks/retro.md",
			frontmatterOwners: []string{"eng"},
			want:              []string{"eng"},
		},
		{
			name:    "empty path is lead-routable",
			relPath: "",
			want:    nil,
		},
		{
			name:    "agents root directory entry is lead-routable",
			relPath: "team/agents/index.md",
			want:    nil,
		},
		{
			name:    "agents slug-only directory entry is lead-routable",
			relPath: "team/agents/csm",
			want:    nil,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := inferOwnerAgentsFromPath(tc.relPath, tc.frontmatterOwners)

			if tc.want == nil {
				if got != nil {
					t.Fatalf("got %v (%T), want nil — lead-routable case must return nil so YAML omitempty drops the key", got, got)
				}
				return
			}
			if len(got) != len(tc.want) {
				t.Fatalf("len: got %d (%v), want %d (%v)", len(got), got, len(tc.want), tc.want)
			}
			for i, want := range tc.want {
				if got[i] != want {
					t.Errorf("[%d]: got %q, want %q", i, got[i], want)
				}
			}

			// Defensive copy: callers may sort/mutate the result without
			// touching the parsed frontmatter slice they passed in.
			if len(tc.frontmatterOwners) > 0 && len(got) > 0 {
				got[0] = "MUTATED"
				if tc.frontmatterOwners[0] == "MUTATED" {
					t.Error("inferOwnerAgentsFromPath must defensively copy frontmatterOwners")
				}
			}
		})
	}
}
