package team

// Tests for the extracted promptBuilder type and its small package-level
// helpers. Written test-first against the proposed surface in PLAN.md §C1
// before the type exists, so a compile failure at first run is expected.
//
// The aim is twofold: (1) exercise branches that buildPrompt wasn't covering
// (1:1 mode, codingAgentSlugs branch, headlessSandboxNote, teamVoiceForSlug
// table) so the new file lands well above the 85% per-file gate, and
// (2) prove that the new type can be driven without a *Launcher — that's
// the whole point of the extraction.

import (
	"strings"
	"testing"
)

func TestTeamVoiceForSlug_KnownSlugs(t *testing.T) {
	cases := map[string]string{
		"ceo":       "Charismatic, decisive",
		"pm":        "Sharp product brain",
		"fe":        "Craft-obsessed",
		"be":        "Systems-minded",
		"ai":        "Curious, pragmatic",
		"designer":  "Taste-driven",
		"cmo":       "Energetic market storyteller",
		"cro":       "Blunt, commercial",
		"tech-lead": "Measured senior engineer",
		"qa":        "Calm breaker of bad assumptions",
		"ae":        "Polished but human closer",
		"sdr":       "High-energy, persistent",
		"research":  "Curious, analytical",
		"content":   "Wordsmith with opinions",
	}
	for slug, want := range cases {
		got := teamVoiceForSlug(slug)
		if !strings.HasPrefix(got, want) {
			t.Errorf("teamVoiceForSlug(%q) = %q, want prefix %q", slug, got, want)
		}
	}
}

func TestTeamVoiceForSlug_UnknownSlugFallsBack(t *testing.T) {
	got := teamVoiceForSlug("unknown-role-12345")
	if !strings.Contains(got, "real teammate") {
		t.Errorf("expected default voice fallback, got %q", got)
	}
}

func TestMarkdownKnowledgeToolBlock_ListsCanonicalTools(t *testing.T) {
	block := markdownKnowledgeToolBlock()
	for _, tool := range []string{
		"notebook_write", "notebook_promote", "notebook_read",
		"team_wiki_read", "team_wiki_write", "wuphf_wiki_lookup",
		"team_learning_search", "team_learning_record",
	} {
		if !strings.Contains(block, tool) {
			t.Errorf("markdownKnowledgeToolBlock missing %q", tool)
		}
	}
}

func TestMarkdownKnowledgeMemoryBlock_RequiresPromotionDiscipline(t *testing.T) {
	block := markdownKnowledgeMemoryBlock()
	for _, want := range []string{
		"notebook_write",
		"notebook_promote",
		"team_learning_record",
		"approved",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("markdownKnowledgeMemoryBlock missing %q", want)
		}
	}
}

func TestPromptBuilder_RendersPriorLearningsWhenMarkdownMemoryActive(t *testing.T) {
	pb := &promptBuilder{
		isOneOnOne:  func() bool { return false },
		isFocusMode: func() bool { return false },
		packName:    func() string { return "Test Office" },
		leadSlug:    func() string { return "ceo" },
		members: func() []officeMember {
			return []officeMember{
				{Slug: "ceo", Name: "CEO", Role: "ceo"},
				{Slug: "fe", Name: "Frontend", Role: "fe"},
			}
		},
		policies: func() []officePolicy { return nil },
		nameFor:  func(slug string) string { return slug },
		learnings: func(slug string) []LearningSearchResult {
			return []LearningSearchResult{{
				LearningRecord: LearningRecord{
					Type:       LearningTypePitfall,
					Key:        "skill-catalog-active-only",
					Insight:    "Skill discovery must filter proposed and archived skills before prompt injection.",
					Confidence: 8,
					Source:     LearningSourceObserved,
					Scope:      "repo",
					Trusted:    false,
				},
				EffectiveConfidence: 8,
			}}
		},
		markdownMemory: true,
	}

	got := pb.Build("fe")
	for _, want := range []string{
		"== PRIOR TEAM LEARNINGS ==",
		"skill-catalog-active-only",
		"source=observed",
		"confidence=8/10",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt missing prior learning fragment %q\n%s", want, got)
		}
	}
}

func TestHeadlessSandboxNote_ForbidsNestedOfficeAndParentSearches(t *testing.T) {
	note := headlessSandboxNote()
	for _, want := range []string{
		"Never launch another `wuphf`",
		"Never search parent or sibling temp directories",
		"operation not permitted",
	} {
		if !strings.Contains(note, want) {
			t.Errorf("headlessSandboxNote missing %q", want)
		}
	}
}

func TestPromptBuilder_OneOnOneBranch(t *testing.T) {
	pb := &promptBuilder{
		isOneOnOne:  func() bool { return true },
		isFocusMode: func() bool { return false },
		packName:    func() string { return "1:1 with CEO" },
		leadSlug:    func() string { return "ceo" },
		members: func() []officeMember {
			return []officeMember{{Slug: "ceo", Name: "CEO", Role: "ceo", Personality: "decisive"}}
		},
		policies:    func() []officePolicy { return nil },
		nameFor:     func(slug string) string { return slug },
		nexDisabled: true,
	}

	got := pb.Build("ceo")
	if !strings.Contains(got, "direct one-on-one WUPHF session with the human") {
		t.Fatalf("expected 1:1 banner, got: %s", got)
	}
	if strings.Contains(got, "== YOUR TEAM ==") {
		t.Fatalf("1:1 prompt must not list teammates")
	}
	if strings.Contains(got, "team_broadcast: Post to channel") {
		t.Fatalf("1:1 prompt must not include channel-mode broadcast guidance")
	}
	if !strings.Contains(got, "team_broadcast: Send a normal direct chat reply") {
		t.Fatalf("1:1 prompt should describe team_broadcast in 1:1 framing")
	}
	if !strings.Contains(got, "Nex tools are disabled for this run") {
		t.Fatalf("nexDisabled=true should produce the no-Nex 1:1 line")
	}
}

func TestPromptBuilder_OneOnOneSkipsLearningLookup(t *testing.T) {
	pb := &promptBuilder{
		isOneOnOne:  func() bool { return true },
		isFocusMode: func() bool { return false },
		packName:    func() string { return "1:1 with CEO" },
		leadSlug:    func() string { return "ceo" },
		members:     func() []officeMember { return []officeMember{{Slug: "ceo", Name: "CEO"}} },
		policies:    func() []officePolicy { return nil },
		nameFor:     func(slug string) string { return slug },
		learnings: func(slug string) []LearningSearchResult {
			t.Fatalf("1:1 prompt should not fetch prior learnings")
			return nil
		},
		markdownMemory: true,
	}

	got := pb.Build("ceo")
	if strings.Contains(got, "== PRIOR TEAM LEARNINGS ==") {
		t.Fatalf("1:1 prompt should not render prior learnings")
	}
}

func TestPromptBuilder_OneOnOneNexEnabledMentionsContextGraph(t *testing.T) {
	pb := &promptBuilder{
		isOneOnOne:  func() bool { return true },
		isFocusMode: func() bool { return false },
		packName:    func() string { return "1:1 with CEO" },
		leadSlug:    func() string { return "ceo" },
		members:     func() []officeMember { return []officeMember{{Slug: "ceo", Name: "CEO"}} },
		policies:    func() []officePolicy { return nil },
		nameFor:     func(slug string) string { return slug },
		nexDisabled: false,
	}
	got := pb.Build("ceo")
	if !strings.Contains(got, "query_context") {
		t.Fatalf("Nex-enabled 1:1 prompt should reference query_context")
	}
}

func TestPromptBuilder_LeadFocusModeAddsDelegationSection(t *testing.T) {
	pb := &promptBuilder{
		isOneOnOne:  func() bool { return false },
		isFocusMode: func() bool { return true },
		packName:    func() string { return "WUPHF Office" },
		leadSlug:    func() string { return "ceo" },
		members: func() []officeMember {
			return []officeMember{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "fe", Name: "Frontend"},
			}
		},
		policies: func() []officePolicy { return nil },
		nameFor:  func(slug string) string { return slug },
	}
	got := pb.Build("ceo")
	if !strings.Contains(got, "== DELEGATION MODE ==") {
		t.Fatalf("expected lead delegation block when focus mode is on")
	}
	if !strings.Contains(got, "Route and hold") {
		t.Fatalf("expected lead delegation routing guidance")
	}
}

func TestPromptBuilder_SpecialistFocusModeAddsDelegationSection(t *testing.T) {
	pb := &promptBuilder{
		isOneOnOne:  func() bool { return false },
		isFocusMode: func() bool { return true },
		packName:    func() string { return "WUPHF Office" },
		leadSlug:    func() string { return "ceo" },
		members: func() []officeMember {
			return []officeMember{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "fe", Name: "Frontend"},
			}
		},
		policies: func() []officePolicy { return nil },
		nameFor:  func(slug string) string { return slug },
	}
	got := pb.Build("fe")
	if !strings.Contains(got, "Delegation mode is enabled") {
		t.Fatalf("expected specialist delegation block when focus mode is on")
	}
	if !strings.Contains(got, "report completion, blockers, or handoff notes back to @ceo") {
		t.Fatalf("expected specialist hand-off back-to-CEO instruction")
	}
}

func TestPromptBuilder_SpecialistCodingAgentRequiresGhPRCreate(t *testing.T) {
	// codingAgentSlugs (eng/be/fe/etc.) get the explicit "actually open the
	// PR" instruction. Verify that branch fires for an eng specialist.
	pb := &promptBuilder{
		isOneOnOne:  func() bool { return false },
		isFocusMode: func() bool { return false },
		packName:    func() string { return "WUPHF Office" },
		leadSlug:    func() string { return "ceo" },
		members: func() []officeMember {
			return []officeMember{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "eng", Name: "Engineer"},
			}
		},
		policies: func() []officePolicy { return nil },
		nameFor:  func(slug string) string { return slug },
	}
	got := pb.Build("eng")
	if !strings.Contains(got, "gh pr create") {
		t.Fatalf("coding-agent specialist prompt must require running gh pr create, got: %s", got)
	}
	if !strings.Contains(got, "https://github.com/...") {
		t.Fatalf("coding-agent specialist prompt must require pasting the returned URL")
	}
}

func TestPromptBuilder_SpecialistNonCodingAgentOmitsGhPRRequirement(t *testing.T) {
	pb := &promptBuilder{
		isOneOnOne:  func() bool { return false },
		isFocusMode: func() bool { return false },
		packName:    func() string { return "WUPHF Office" },
		leadSlug:    func() string { return "ceo" },
		members: func() []officeMember {
			return []officeMember{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "designer", Name: "Designer"},
			}
		},
		policies: func() []officePolicy { return nil },
		nameFor:  func(slug string) string { return slug },
	}
	got := pb.Build("designer")
	if strings.Contains(got, "gh pr create") {
		t.Fatalf("non-coding specialist prompt must NOT contain gh pr create instructions")
	}
}

func TestPromptBuilder_LeadIncludesActivePoliciesSorted(t *testing.T) {
	pb := &promptBuilder{
		isOneOnOne:  func() bool { return false },
		isFocusMode: func() bool { return false },
		packName:    func() string { return "WUPHF Office" },
		leadSlug:    func() string { return "ceo" },
		members:     func() []officeMember { return []officeMember{{Slug: "ceo", Name: "CEO"}} },
		// Caller is responsible for passing pre-sorted policies (matches the
		// existing buildPrompt behaviour). The block formatting itself is
		// what we're asserting here.
		policies: func() []officePolicy {
			return []officePolicy{
				{ID: "a", Rule: "always confirm before deleting"},
				{ID: "b", Rule: "never push to main"},
			}
		},
		nameFor: func(slug string) string { return slug },
	}
	got := pb.Build("ceo")
	if !strings.Contains(got, "== ACTIVE OFFICE POLICIES ==") {
		t.Fatalf("expected active policies banner")
	}
	if !strings.Contains(got, "always confirm before deleting") {
		t.Fatalf("expected first policy rule")
	}
	if !strings.Contains(got, "never push to main") {
		t.Fatalf("expected second policy rule")
	}
	// Order: by appearance in slice (caller pre-sorts).
	a := strings.Index(got, "always confirm")
	b := strings.Index(got, "never push")
	if a == -1 || b == -1 || a > b {
		t.Fatalf("expected policies in input order, got positions %d/%d in:\n%s", a, b, got)
	}
}

func TestPromptBuilder_DeterministicOrderingFromMembers(t *testing.T) {
	// PLAN.md trap-adjacent: prompt cache hits depend on byte-identical
	// output across runs. The promptBuilder must sort its own members
	// snapshot before walking it (it currently does this inside buildPrompt
	// at line 3761). Two builds with the same members in different input
	// order must produce identical output.
	mk := func(order []string) *promptBuilder {
		members := make([]officeMember, len(order))
		for i, s := range order {
			members[i] = officeMember{Slug: s, Name: s}
		}
		return &promptBuilder{
			isOneOnOne:  func() bool { return false },
			isFocusMode: func() bool { return false },
			packName:    func() string { return "WUPHF Office" },
			leadSlug:    func() string { return "ceo" },
			members:     func() []officeMember { return members },
			policies:    func() []officePolicy { return nil },
			nameFor:     func(slug string) string { return slug },
		}
	}
	a := mk([]string{"ceo", "eng", "fe"}).Build("ceo")
	b := mk([]string{"fe", "ceo", "eng"}).Build("ceo")
	if a != b {
		t.Fatalf("promptBuilder.Build is not deterministic across member input orderings.\nA len=%d\nB len=%d", len(a), len(b))
	}
}
