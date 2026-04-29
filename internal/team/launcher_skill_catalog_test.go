package team

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/nex-crm/wuphf/internal/agent"
)

// catalogTestLauncher returns a Launcher wired to a fresh broker with the
// given members + skills already seeded under b.mu. The fixture is shared by
// the skill-catalog buildPrompt tests so the assertions stay focused on
// catalog rendering, not test plumbing.
func catalogTestLauncher(t *testing.T, members []officeMember, skills []teamSkill) (*Launcher, *Broker) {
	t.Helper()
	b := newTestBroker(t)
	b.mu.Lock()
	b.members = members
	b.skills = skills
	b.mu.Unlock()

	agents := make([]agent.AgentConfig, 0, len(members))
	for _, m := range members {
		agents = append(agents, agent.AgentConfig{Slug: m.Slug, Name: m.Name})
	}
	l := &Launcher{
		pack: &agent.PackDefinition{
			LeadSlug: "ceo",
			Agents:   agents,
		},
		broker: b,
	}
	return l, b
}

// TestBuildPrompt_SkillCatalog_StableBytes guards the cache-stability
// invariant: identical b.skills + slug snapshots must produce byte-identical
// prompts across calls so prompt caching keeps hitting between turns.
func TestBuildPrompt_SkillCatalog_StableBytes(t *testing.T) {
	l, _ := catalogTestLauncher(t,
		[]officeMember{
			{Slug: "ceo", Name: "CEO", BuiltIn: true},
			{Slug: "deploy-bot", Name: "Deploy Bot"},
		},
		[]teamSkill{
			{ID: "skill-a", Name: "deploy-frontend", Description: "Ship a hotfix release.", Trigger: "Sev-1 prod bug", Status: "active", OwnerAgents: []string{"deploy-bot"}},
			{ID: "skill-b", Name: "run-pre-pr-tests", Description: "Run the full suite before PR.", Trigger: "Before gh pr create", Status: "active", OwnerAgents: []string{"deploy-bot"}},
		},
	)

	first := l.buildPrompt("deploy-bot")
	second := l.buildPrompt("deploy-bot")
	if first != second {
		t.Errorf("buildPrompt is not byte-stable across calls\nfirst len=%d\nsecond len=%d", len(first), len(second))
	}
	if !strings.Contains(first, "== YOUR SKILL CATALOG ==") {
		t.Error("expected YOUR SKILL CATALOG section in prompt")
	}
	if !strings.Contains(first, "deploy-frontend") || !strings.Contains(first, "run-pre-pr-tests") {
		t.Error("expected both visible skills in catalog")
	}
	// Sorted by name lex: run-pre-pr-tests > deploy-frontend, so the order
	// in the rendered catalog must be deploy-frontend, then run-pre-pr-tests.
	deployIdx := strings.Index(first, "- deploy-frontend:")
	runIdx := strings.Index(first, "- run-pre-pr-tests:")
	if deployIdx == -1 || runIdx == -1 {
		t.Fatalf("expected both bullets in catalog: deploy=%d run=%d", deployIdx, runIdx)
	}
	if deployIdx > runIdx {
		t.Error("catalog must be sorted by name lexicographically")
	}
}

// TestBuildPrompt_SkillCatalog_ActiveOnly verifies the catalog skips skills
// whose status is not "active" — disabled, archived, and proposed must be
// invisible to the prompt-time agent.
func TestBuildPrompt_SkillCatalog_ActiveOnly(t *testing.T) {
	l, _ := catalogTestLauncher(t,
		[]officeMember{
			{Slug: "ceo", Name: "CEO", BuiltIn: true},
			{Slug: "deploy-bot", Name: "Deploy Bot"},
		},
		[]teamSkill{
			{ID: "skill-a", Name: "active-skill", Description: "Ship.", Status: "active", OwnerAgents: []string{"deploy-bot"}},
			{ID: "skill-b", Name: "disabled-skill", Description: "Paused.", Status: "disabled", OwnerAgents: []string{"deploy-bot"}},
			{ID: "skill-c", Name: "archived-skill", Description: "Retired.", Status: "archived", OwnerAgents: []string{"deploy-bot"}},
			{ID: "skill-d", Name: "proposed-skill", Description: "Pending review.", Status: "proposed", OwnerAgents: []string{"deploy-bot"}},
		},
	)

	prompt := l.buildPrompt("deploy-bot")
	if !strings.Contains(prompt, "active-skill") {
		t.Error("expected active-skill in catalog")
	}
	for _, name := range []string{"disabled-skill", "archived-skill", "proposed-skill"} {
		if strings.Contains(prompt, "- "+name+":") {
			t.Errorf("expected %q to NOT appear in active-only catalog", name)
		}
	}
}

// TestBuildPrompt_SkillCatalog_EmptyOmits verifies the catalog header is not
// emitted at all when the agent has no visible active skills. Avoids a
// dangling section header in the prompt.
func TestBuildPrompt_SkillCatalog_EmptyOmits(t *testing.T) {
	t.Run("agent with no scoped skills sees no catalog", func(t *testing.T) {
		l, _ := catalogTestLauncher(t,
			[]officeMember{
				{Slug: "ceo", Name: "CEO", BuiltIn: true},
				{Slug: "csm", Name: "CSM"},
				{Slug: "deploy-bot", Name: "Deploy Bot"},
			},
			[]teamSkill{
				{ID: "skill-a", Name: "active-skill", Description: "Ship.", Status: "active", OwnerAgents: []string{"deploy-bot"}},
			},
		)

		prompt := l.buildPrompt("csm")
		if strings.Contains(prompt, "== YOUR SKILL CATALOG ==") {
			t.Error("agent with no visible skills must not see the catalog header")
		}
	})

	t.Run("empty broker skills emits nothing", func(t *testing.T) {
		l, _ := catalogTestLauncher(t,
			[]officeMember{{Slug: "ceo", Name: "CEO", BuiltIn: true}},
			nil,
		)

		prompt := l.buildPrompt("ceo")
		if strings.Contains(prompt, "== YOUR SKILL CATALOG ==") {
			t.Error("empty b.skills must not emit the catalog header")
		}
	})
}

// TestBuildPrompt_SkillCatalog_TruncatesLongDescription locks in the 100-char
// description cap so a single verbose skill cannot blow the prompt budget.
func TestBuildPrompt_SkillCatalog_TruncatesLongDescription(t *testing.T) {
	long := strings.Repeat("verbose-", 30) // ~240 chars
	l, _ := catalogTestLauncher(t,
		[]officeMember{
			{Slug: "ceo", Name: "CEO", BuiltIn: true},
			{Slug: "deploy-bot", Name: "Deploy Bot"},
		},
		[]teamSkill{
			{ID: "skill-a", Name: "verbose-skill", Description: long, Status: "active", OwnerAgents: []string{"deploy-bot"}},
		},
	)

	prompt := l.buildPrompt("deploy-bot")
	if !strings.Contains(prompt, "verbose-skill") {
		t.Fatal("expected verbose-skill in catalog")
	}
	// The description bullet line must include the truncation marker.
	for _, line := range strings.Split(prompt, "\n") {
		if strings.HasPrefix(line, "- verbose-skill:") {
			if !strings.HasSuffix(strings.TrimSpace(line), "...") {
				t.Errorf("expected ellipsis on truncated description: %q", line)
			}
			// Cap is desc + "...": "- verbose-skill: " + 100 + "..." = ~119
			// chars, well below any reasonable limit.
			if len(line) > 200 {
				t.Errorf("description line too long after truncation: %d chars", len(line))
			}
		}
	}
}

// PR 7 /review P2: renderSkillCatalogSection must truncate by rune count,
// not byte index, so multi-byte UTF-8 (CJK, emoji) doesn't get sliced
// mid-codepoint into invalid output.
func TestRenderSkillCatalogSection_TruncatesByRune(t *testing.T) {
	// 200 CJK runes, each 3 bytes — 600 bytes total. Byte truncation at
	// 100 would split a code point at byte 99 (1/3 into the 34th char) and
	// emit a UTF-8 invalid byte sequence.
	long := strings.Repeat("漢", 200)
	visible := []teamSkill{{Name: "kanji-skill", Description: long, Status: "active"}}
	out := renderSkillCatalogSection(visible)

	// Output must remain valid UTF-8.
	if !utf8.ValidString(out) {
		t.Fatalf("renderSkillCatalogSection produced invalid UTF-8")
	}
	// Find the bullet line.
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "- kanji-skill:") {
			continue
		}
		// Strip "- kanji-skill: " prefix to get the description portion.
		desc := strings.TrimPrefix(line, "- kanji-skill: ")
		// Truncated form ends with the ASCII ellipsis (3 dots).
		if !strings.HasSuffix(desc, "...") {
			t.Errorf("expected '...' suffix on truncation: %q", desc)
		}
		descNoSuffix := strings.TrimSuffix(desc, "...")
		if got := len([]rune(descNoSuffix)); got != 100 {
			t.Errorf("truncated rune count: got %d, want 100", got)
		}
		return
	}
	t.Fatalf("catalog missing kanji-skill bullet:\n%s", out)
}
