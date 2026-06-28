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
		"visual_artifact_create", "visual_artifact_promote",
		"team_wiki_read", "team_wiki_write", "wuphf_wiki_lookup",
		"team_learning_search", "team_learning_record",
	} {
		if !strings.Contains(block, tool) {
			t.Errorf("markdownKnowledgeToolBlock missing %q", tool)
		}
	}
	for _, want := range []string{
		"human explicitly asked",
		"Pass human_request as the broker message ID",
		"tag @librarian (Pam) to land it in the wiki",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("markdownKnowledgeToolBlock missing direct-wiki guardrail %q", want)
		}
	}
	// The removed notebook tool surface must not reappear.
	for _, banned := range []string{"notebook_write", "notebook_promote", "notebook_read", "notebook_search"} {
		if strings.Contains(block, banned) {
			t.Errorf("markdownKnowledgeToolBlock still references removed tool %q", banned)
		}
	}
}

func TestMarkdownKnowledgeMemoryBlock_RoutesCanonicalKnowledgeThroughLibrarian(t *testing.T) {
	block := markdownKnowledgeMemoryBlock()
	for _, want := range []string{
		"team_learning_record",
		"typed learning store",
		"tag @librarian (Pam) to capture it into the wiki",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("markdownKnowledgeMemoryBlock missing %q", want)
		}
	}
	if strings.Contains(block, "team_learning_record succeeded") {
		t.Errorf("markdownKnowledgeMemoryBlock must not treat team_learning_record as wiki storage")
	}
	for _, banned := range []string{"notebook_write", "notebook_promote"} {
		if strings.Contains(block, banned) {
			t.Errorf("markdownKnowledgeMemoryBlock still references removed tool %q", banned)
		}
	}
}

func TestMarkdownKnowledgeToolBlock_NudgesNaturalHTMLArtifactCreation(t *testing.T) {
	block := markdownKnowledgeToolBlock()
	for _, want := range []string{
		"complex specs",
		"PR reviews",
		"interactive tuning surfaces",
		"self-contained HTML article",
		"no network fetches",
		"visual-artifact:ra_...",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("markdownKnowledgeToolBlock missing HTML artifact trigger %q", want)
		}
	}
}

func TestMarkdownKnowledgeToolBlock_HTMLArticleIsSingleTool(t *testing.T) {
	// Finding 2: markdownKnowledgeToolBlock used to instruct the deprecated
	// "after notebook_write, create an HTML companion" pattern, which
	// contradicted the visualArtifactForcingBlock single-tool flow (HTML
	// article = one visual_artifact_create call with empty
	// source_path, no companion notebook_write). The two blocks must tell one
	// consistent story.
	block := markdownKnowledgeToolBlock()
	// The deprecated companion-after-notebook_write framing must be gone.
	if strings.Contains(block, "After notebook_write") {
		t.Errorf("markdownKnowledgeToolBlock still chains the HTML artifact AFTER notebook_write (deprecated companion pattern)")
	}
	if strings.Contains(block, "HTML companion") {
		t.Errorf("markdownKnowledgeToolBlock still describes the HTML artifact as a companion to notebook_write")
	}
	// The single-tool flow must be stated: empty source_path, the HTML
	// article is the deliverable.
	for _, want := range []string{
		"leave source_path empty",
		"HTML article IS the deliverable",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("markdownKnowledgeToolBlock missing single-tool HTML flow phrase %q", want)
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

func TestPromptBuilder_OneOnOneMarkdownMemoryMentionsHTMLArtifacts(t *testing.T) {
	pb := &promptBuilder{
		isOneOnOne:     func() bool { return true },
		isFocusMode:    func() bool { return false },
		packName:       func() string { return "1:1 with CEO" },
		leadSlug:       func() string { return "ceo" },
		members:        func() []officeMember { return []officeMember{{Slug: "ceo", Name: "CEO", Role: "ceo"}} },
		policies:       func() []officePolicy { return nil },
		nameFor:        func(slug string) string { return slug },
		markdownMemory: true,
		nexDisabled:    true,
	}

	got := pb.Build("ceo")
	for _, want := range []string{
		"Markdown wiki memory is active in this 1:1",
		"visual_artifact_create",
		"diagram, mockup, report, comparison grid, code explainer, PR review, or interactive tuning surface",
		"visual-artifact:ra_...",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("1:1 markdown prompt missing %q:\n%s", want, got)
		}
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

func TestPromptBuilder_MarkdownMemoryPromptsNaturalHTMLArtifactsDuringWork(t *testing.T) {
	pb := &promptBuilder{
		isOneOnOne:  func() bool { return false },
		isFocusMode: func() bool { return false },
		packName:    func() string { return "WUPHF Office" },
		leadSlug:    func() string { return "ceo" },
		members: func() []officeMember {
			return []officeMember{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "pm", Name: "Product Manager"},
			}
		},
		policies:       func() []officePolicy { return nil },
		nameFor:        func(slug string) string { return slug },
		markdownMemory: true,
	}

	for _, slug := range []string{"ceo", "pm"} {
		got := pb.Build(slug)
		for _, want := range []string{
			"visual_artifact_create",
			"complex specs",
			"implementation plans",
			"comparison grids",
			"interactive tuning surfaces",
			"HTML visual artifact",
			"long markdown wall",
			"visual-artifact:ra_...",
		} {
			if !strings.Contains(got, want) {
				t.Fatalf("%s prompt missing natural HTML artifact guidance %q:\n%s", slug, want, got)
			}
		}
	}
}

func TestPromptBuilder_VisualArtifactSelectivityRulePresentOnEverySurface(t *testing.T) {
	// The OLD version of this prompt block FORCED an HTML article for every
	// research/explain/plan request. The 2026-05-29 demo showed that was a
	// bug: a one-line coffee-pressure question got a full HTML article plus
	// an unsolicited team_skill_create. The block is now a selectivity
	// decision tree — agents must judge whether HTML is warranted before
	// reaching for the tool. This test pins the new shape across every
	// surface that markdown memory reaches.
	mkBuilder := func(oneOnOne bool) *promptBuilder {
		return &promptBuilder{
			isOneOnOne:  func() bool { return oneOnOne },
			isFocusMode: func() bool { return false },
			packName:    func() string { return "WUPHF Office" },
			leadSlug:    func() string { return "ceo" },
			members: func() []officeMember {
				return []officeMember{
					{Slug: "ceo", Name: "CEO", Role: "ceo"},
					{Slug: "pm", Name: "Product Manager"},
				}
			},
			policies:       func() []officePolicy { return nil },
			nameFor:        func(slug string) string { return slug },
			markdownMemory: true,
			nexDisabled:    true,
		}
	}

	cases := []struct {
		name     string
		oneOnOne bool
		slug     string
	}{
		{name: "lead/office", oneOnOne: false, slug: "ceo"},
		{name: "specialist/office", oneOnOne: false, slug: "pm"},
		{name: "lead/one-on-one", oneOnOne: true, slug: "ceo"},
	}
	wants := []string{
		// Selectivity framing — header explicitly says "selectivity, not reflex".
		"HTML ARTICLE RULE",
		"selectivity, not reflex",
		"It is NOT the default answer format",
		"answer in plain text in the channel and STOP",
		// Positive trigger: all three conditions must be true.
		"USE an HTML article ONLY when ALL THREE",
		"comparing two-or-more things side by side",
		"walking a multi-step process or timeline",
		"mapping a 2D variable space",
		"multi-section explainer with at least THREE distinct sections",
		"Plain prose in chat would lose meaningful information density",
		// Negative trigger: the decision tree's "DO NOT" branch.
		"DO NOT use an HTML article when",
		"conversational, a status update, a short factual reply",
		"one-liner question expecting a one-liner answer",
		"mostly a list, a code snippet, a small table",
		"urge to \"codify\" or \"document\"",
		"Do not announce that you decided against an artifact",
		// When HTML IS warranted: it must be a real artifact with real figures.
		"WHEN HTML IS WARRANTED",
		"pure-text \"article\" with no figures is NOT an artifact",
		"genuine SVG figures",
		"#1342FF",
		"FIG_NNN labels",
		"monospace captions",
		// Atomic-turn rule still applies WHEN the rule fires.
		"ATOMIC-TURN RULE (only when HTML IS warranted)",
		"SAME assistant response",
		"Do NOT narrate the process between steps",
		"visual_artifact_create",
		"visual-artifact:ra_...",
		"full breakdown below.",
		// Broadcast budget — at most 2 for artifact turns, at most 1 otherwise.
		"BROADCAST BUDGET PER TURN",
		"Artifact turns: AT MOST two chat messages",
		"Non-artifact turns: AT MOST one chat message",
		"No plan preamble",
		// Unsolicited-tools ban — the hard ban on task/wiki creation.
		"DO NOT CALL these tools without an explicit human request",
		"team_task create / complete",
		"team_wiki_write",
		"save to wiki",
		"self-codify the pattern",
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mkBuilder(tc.oneOnOne).Build(tc.slug)
			for _, want := range wants {
				if !strings.Contains(got, want) {
					t.Fatalf("missing %q in %s prompt:\n%s", want, tc.name, got)
				}
			}
		})
	}
}

func TestPromptBuilder_HTMLArtifactFlowIsConsistentAcrossBlocks(t *testing.T) {
	// Finding 2: the full prompt must NOT simultaneously instruct the
	// deprecated "notebook_write then HTML companion" pattern (from
	// markdownKnowledgeToolBlock) and the single-tool "HTML article with
	// empty source_path, no companion notebook_write" pattern (from
	// visualArtifactForcingBlock). The two blocks must tell one story.
	mkBuilder := func(oneOnOne bool) *promptBuilder {
		return &promptBuilder{
			isOneOnOne:  func() bool { return oneOnOne },
			isFocusMode: func() bool { return false },
			packName:    func() string { return "WUPHF Office" },
			leadSlug:    func() string { return "ceo" },
			members: func() []officeMember {
				return []officeMember{
					{Slug: "ceo", Name: "CEO", Role: "ceo"},
					{Slug: "pm", Name: "Product Manager"},
				}
			},
			policies:       func() []officePolicy { return nil },
			nameFor:        func(slug string) string { return slug },
			markdownMemory: true,
			nexDisabled:    true,
		}
	}
	cases := []struct {
		name     string
		oneOnOne bool
		slug     string
	}{
		{name: "lead/office", oneOnOne: false, slug: "ceo"},
		{name: "specialist/office", oneOnOne: false, slug: "pm"},
		{name: "lead/one-on-one", oneOnOne: true, slug: "ceo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mkBuilder(tc.oneOnOne).Build(tc.slug)
			// The single-tool flow is the canonical instruction and must be
			// present.
			if !strings.Contains(got, "Leave source_path empty; the HTML is the article, not a companion to a markdown file.") {
				t.Fatalf("%s prompt missing single-tool HTML flow (empty source_path)", tc.name)
			}
			// The deprecated companion-after-notebook_write instruction must be
			// gone everywhere — no block may still chain the HTML artifact
			// AFTER a notebook_write of the same content.
			if strings.Contains(got, "After notebook_write, create a self-contained HTML companion") {
				t.Fatalf("%s prompt still carries the deprecated notebook_write-then-companion instruction", tc.name)
			}
			if strings.Contains(got, "create an HTML visual companion with visual_artifact_create") {
				t.Fatalf("%s prompt still describes the HTML artifact as a companion to notebook_write", tc.name)
			}
		})
	}
}

func TestPromptBuilder_ToolSearchAcceptanceLanguagePreserved(t *testing.T) {
	// Existing behavior we don't want to lose: when claude-code defers tool
	// schemas, the agent should make ONE ToolSearch call at the start of the
	// turn (silently, no narration) and proceed. The schema list it loads is
	// now pared back — it must NOT preload the genuinely-unsolicited tools
	// (skill_create, wiki_write) unless the human explicitly asked. team_task
	// is NO LONGER in the ban: Rule Zero requires team_task action=create as
	// the FIRST tool call on a work-shaped request, so banning its schema
	// would forbid loading a tool the agent is mandated to use.
	pb := &promptBuilder{
		isOneOnOne:  func() bool { return false },
		isFocusMode: func() bool { return false },
		packName:    func() string { return "WUPHF Office" },
		leadSlug:    func() string { return "ceo" },
		members: func() []officeMember {
			return []officeMember{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "pm", Name: "Product Manager"},
			}
		},
		policies: func() []officePolicy { return nil },
		nameFor:  func(slug string) string { return slug },
	}
	for _, slug := range []string{"ceo", "pm"} {
		got := pb.Build(slug)
		for _, want := range []string{
			"claude-code defers their schemas behind a built-in ToolSearch tool",
			"do it ONCE at the very start of your turn",
			"single ToolSearch call",
			"Load ONLY the schemas you actually plan to use",
			"Do NOT preload team_wiki_write",
			"Never call ToolSearch a second time in the same turn",
			"Do NOT narrate the tool-loading process",
		} {
			if !strings.Contains(got, want) {
				t.Fatalf("%s prompt missing ToolSearch language %q", slug, want)
			}
		}
		// The preload ban must NOT swallow team_task — Rule Zero mandates it.
		// The old blanket ban listed all three tools together; assert that
		// exact contradiction is gone and the Rule-Zero carve-out is present.
		if strings.Contains(got, "Do NOT preload team_skill_create, team_task, or team_wiki_write") {
			t.Fatalf("%s prompt still bans preloading team_task — contradicts Rule Zero", slug)
		}
		if !strings.Contains(got, "team_task is exempt — Rule Zero requires team_task action=create") {
			t.Fatalf("%s prompt missing Rule-Zero exemption carve-out for team_task", slug)
		}
	}
}

func TestPromptBuilder_UnsolicitedToolBanIsExplicit(t *testing.T) {
	// Live demo failure 2026-05-29: after answering a coffee question, the
	// agent self-codified a skill and called team_task to mark a task
	// complete. Neither was requested. team_skill_create is gone entirely
	// (core-loop R5); pin the ban on the remaining tools plus the absence
	// of any skill-creation tool mention.
	pb := &promptBuilder{
		isOneOnOne:  func() bool { return false },
		isFocusMode: func() bool { return false },
		packName:    func() string { return "WUPHF Office" },
		leadSlug:    func() string { return "ceo" },
		members: func() []officeMember {
			return []officeMember{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "pm", Name: "Product Manager"},
			}
		},
		policies:       func() []officePolicy { return nil },
		nameFor:        func(slug string) string { return slug },
		markdownMemory: true,
		nexDisabled:    true,
	}
	for _, slug := range []string{"ceo", "pm"} {
		got := pb.Build(slug)
		// Ban must apply to the remaining tool families.
		for _, want := range []string{
			"team_task create / complete — ONLY when the human assigned a task",
			"Do not invent a task to mark complete after a chat answer",
			"team_wiki_write — ONLY when the human says",
			"self-codify the pattern",
		} {
			if !strings.Contains(got, want) {
				t.Fatalf("%s prompt missing unsolicited-tool ban %q", slug, want)
			}
		}
		if strings.Contains(got, "team_skill_create") {
			t.Fatalf("%s prompt still mentions team_skill_create — the tool was removed", slug)
		}
	}
}

func TestPromptBuilder_VisualArtifactForcingRuleSkippedWithoutMarkdownMemory(t *testing.T) {
	// The forcing rule only applies when markdown memory is the backend; if
	// notebook/wiki tools are not available there is nothing to call. Cover
	// every surface the positive test covers (lead/specialist/1:1) so a
	// regression that re-emits the block on any of them is caught.
	mkBuilder := func(oneOnOne bool) *promptBuilder {
		return &promptBuilder{
			isOneOnOne:  func() bool { return oneOnOne },
			isFocusMode: func() bool { return false },
			packName:    func() string { return "WUPHF Office" },
			leadSlug:    func() string { return "ceo" },
			members: func() []officeMember {
				return []officeMember{{Slug: "ceo", Name: "CEO", Role: "ceo"}, {Slug: "pm", Name: "PM"}}
			},
			policies:       func() []officePolicy { return nil },
			nameFor:        func(slug string) string { return slug },
			markdownMemory: false,
			nexDisabled:    true,
		}
	}
	cases := []struct {
		name     string
		oneOnOne bool
		slug     string
	}{
		{name: "lead/office", oneOnOne: false, slug: "ceo"},
		{name: "specialist/office", oneOnOne: false, slug: "pm"},
		{name: "lead/one-on-one", oneOnOne: true, slug: "ceo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mkBuilder(tc.oneOnOne).Build(tc.slug)
			if strings.Contains(got, "VISUAL ARTIFACT RULE") {
				t.Fatalf("%s prompt should NOT contain VISUAL ARTIFACT RULE when markdownMemory=false:\n%s", tc.name, got)
			}
		})
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

func TestMarkdownKnowledgeToolBlock_HumanRememberAutoRoutingNote(t *testing.T) {
	// PR 7 edit 1: the memory guidance must warn agents that the
	// broker auto-routes human "remember this" / "save to wiki" phrases so
	// they do not duplicate the write. PR 2 originally added this copy; PR 7
	// keeps it as a regression gate.
	block := markdownKnowledgeToolBlock()
	for _, want := range []string{
		"remember this",
		"save to wiki",
		"do NOT re-route",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("markdownKnowledgeToolBlock missing human auto-routing fragment %q", want)
		}
	}
}

func TestPromptBuilder_LibrarianOwnsWikiReviewCEODelegates(t *testing.T) {
	// Phase 4: wiki promotion/review authority moved from the CEO to the
	// Librarian. The Librarian prompt mentions team_notebook_review + the demand
	// signal; the CEO prompt no longer runs review itself and instead delegates
	// to @librarian.
	mk := func() *promptBuilder {
		return &promptBuilder{
			isOneOnOne:  func() bool { return false },
			isFocusMode: func() bool { return false },
			packName:    func() string { return "WUPHF Office" },
			leadSlug:    func() string { return "ceo" },
			members: func() []officeMember {
				return []officeMember{
					{Slug: "ceo", Name: "CEO"},
					{Slug: LibrarianSlug, Name: librarianName, Role: librarianRole},
					{Slug: "fe", Name: "Frontend"},
				}
			},
			policies:       func() []officePolicy { return nil },
			nameFor:        func(slug string) string { return slug },
			markdownMemory: true,
		}
	}

	lib := mk().Build(LibrarianSlug)
	for _, want := range []string{
		"WIKI OWNERSHIP (you are the Librarian)",
		"You own the team's wiki",
	} {
		if !strings.Contains(lib, want) {
			t.Errorf("Librarian prompt missing wiki-authority fragment %q", want)
		}
	}

	ceo := mk().Build("ceo")
	if strings.Contains(ceo, "WIKI OWNERSHIP (you are the Librarian)") {
		t.Errorf("CEO prompt must not carry the Librarian's wiki-ownership block")
	}
	if !strings.Contains(ceo, "tag @librarian (Pam) to capture it into the team wiki") {
		t.Errorf("CEO prompt should hand durable knowledge capture to @librarian")
	}
	if strings.Contains(ceo, "team_notebook_review") {
		t.Errorf("CEO prompt must not reference the removed team_notebook_review tool")
	}
}

func TestPromptBuilder_NonCEOOmitsTeamNotebookReview(t *testing.T) {
	// PR 7 edit 3 is CEO-only. Specialist prompts must not include
	// team_notebook_review (it is not in their tool set).
	pb := &promptBuilder{
		isOneOnOne:  func() bool { return false },
		isFocusMode: func() bool { return false },
		packName:    func() string { return "WUPHF Office" },
		leadSlug:    func() string { return "ceo" },
		members: func() []officeMember {
			return []officeMember{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "fe", Name: "Frontend"},
			}
		},
		policies:       func() []officePolicy { return nil },
		nameFor:        func(slug string) string { return slug },
		markdownMemory: true,
	}
	got := pb.Build("fe")
	if strings.Contains(got, "team_notebook_review") {
		t.Fatalf("specialist prompt must not mention team_notebook_review (CEO-only tool)")
	}
}

func TestPromptBuilder_DurableKnowledgeRoutesThroughLibrarian(t *testing.T) {
	// Wiki authority lives with the Librarian. After the notebook/promotion
	// removal, both CEO and specialist prompts route durable knowledge capture
	// through @librarian rather than a notebook_promote review queue.
	mk := func() *promptBuilder {
		return &promptBuilder{
			isOneOnOne:  func() bool { return false },
			isFocusMode: func() bool { return false },
			packName:    func() string { return "WUPHF Office" },
			leadSlug:    func() string { return "ceo" },
			members: func() []officeMember {
				return []officeMember{
					{Slug: "ceo", Name: "CEO"},
					{Slug: "fe", Name: "Frontend"},
				}
			},
			policies:       func() []officePolicy { return nil },
			nameFor:        func(s string) string { return s },
			markdownMemory: true,
		}
	}
	for _, slug := range []string{"ceo", "fe"} {
		got := mk().Build(slug)
		if !strings.Contains(got, "@librarian") {
			t.Errorf("%s prompt should route durable knowledge through @librarian", slug)
		}
		for _, banned := range []string{"notebook_promote", "notebook_write", "team_notebook_review"} {
			if strings.Contains(got, banned) {
				t.Errorf("%s prompt still references removed tool %q", slug, banned)
			}
		}
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

// TestVisualArtifactForcingBlock_CoversIssueSpecs locks in the rule that a
// non-trivial Issue spec must ship with an HTML artifact reference inside
// the `details` field. The FE's IssueDescription renders that artifact
// inline above the markdown body via the same RichArtifactEmbed pipeline
// wiki articles use — without this prompt language, agents fall back to
// pasting a wall of markdown into details and the inline-embed surface
// never gets exercised.
func TestVisualArtifactForcingBlock_CoversIssueSpecs(t *testing.T) {
	got := visualArtifactForcingBlock()
	for _, want := range []string{
		"ISSUE SPECS ALSO QUALIFY",
		"team_task action=create",
		"`visual-artifact:ra_<id>` on its own line inside the `details` field",
		"RichArtifactEmbed",
		"Skip the artifact for trivial Issues",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf(
				"visualArtifactForcingBlock missing required Issue-spec language %q\n--- got ---\n%s",
				want, got,
			)
		}
	}
}

// TestPromptBuilder_PoliciesFilteredByAgentAssignment pins the B3
// always-loaded contract for policies: a policy scoped to another agent
// stays OUT of this agent's prompt; nil-scope (all agents) and own-scope
// policies are rendered.
func TestPromptBuilder_PoliciesFilteredByAgentAssignment(t *testing.T) {
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
		policies: func() []officePolicy {
			return []officePolicy{
				{ID: "a", Rule: "applies to everyone"},
				{ID: "b", Rule: "eng-only deploy checklist", Agents: []string{"eng"}},
				{ID: "c", Rule: "ceo-only hiring review", Agents: []string{"ceo"}},
			}
		},
		nameFor: func(slug string) string { return slug },
	}

	eng := pb.Build("eng")
	if !strings.Contains(eng, "applies to everyone") || !strings.Contains(eng, "eng-only deploy checklist") {
		t.Fatalf("eng prompt must carry all-agents + eng-scoped policies:\n%s", eng)
	}
	if strings.Contains(eng, "ceo-only hiring review") {
		t.Fatalf("eng prompt must NOT carry a policy scoped to another agent")
	}

	ceo := pb.Build("ceo")
	if !strings.Contains(ceo, "ceo-only hiring review") || strings.Contains(ceo, "eng-only deploy checklist") {
		t.Fatalf("ceo prompt scope filter broken:\n%s", ceo)
	}
}

// TestPromptBuilder_GroundingBlockOnLeadAndSpecialist pins the
// anti-fabrication contract (ICP-eval v2 [00:47]/[00:50]: three invented
// humans shipped in a customer QBR): both office surfaces must carry the
// grounding rule, and the block itself must name the sourcing contract,
// the [NEEDS CONFIRMATION] escape hatch, and the no-hits honesty rule.
func TestPromptBuilder_GroundingBlockOnLeadAndSpecialist(t *testing.T) {
	block := groundingBlock()
	for _, want := range []string{
		"== GROUNDING",
		"RETRIEVED CONTEXT",
		"human_interview",
		"[NEEDS CONFIRMATION:",
		"firing offense",
		"no hits",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("groundingBlock missing %q", want)
		}
	}

	pb := &promptBuilder{
		isOneOnOne:  func() bool { return false },
		isFocusMode: func() bool { return false },
		packName:    func() string { return "Test Office" },
		leadSlug:    func() string { return "ceo" },
		members: func() []officeMember {
			return []officeMember{
				{Slug: "ceo", Name: "CEO", Role: "ceo"},
				{Slug: "eng", Name: "Engineer", Role: "eng"},
			}
		},
		policies: func() []officePolicy { return nil },
		nameFor:  func(slug string) string { return slug },
	}
	for _, slug := range []string{"ceo", "eng"} {
		if got := pb.Build(slug); !strings.Contains(got, "== GROUNDING") {
			t.Errorf("prompt for %q missing the grounding block", slug)
		}
	}
}

// TestPromptBuilder_DestructiveVCSGuardOnLeadAndSpecialist pins the
// destructive-git contract (ICP-eval v3 V3-N6: a human "Stop" was answered
// with `git checkout HEAD`, destroying the session's deliverable). Both
// office surfaces must carry the guard; the block itself must name the
// work-discarding commands, the ask-first contract, and the Stop=pause rule.
func TestPromptBuilder_DestructiveVCSGuardOnLeadAndSpecialist(t *testing.T) {
	block := destructiveVCSGuardBlock()
	for _, want := range []string{
		"== DESTRUCTIVE GIT GUARD",
		"push --force",
		"what would be lost",
		"human_interview",
		"never yours to destroy",
		"PAUSE and report state",
		"do NOT revert",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("destructiveVCSGuardBlock missing %q", want)
		}
	}
	// The guard is paid on every turn: keep the body under 80 words.
	if words := len(strings.Fields(block)); words > 95 { // header + <=80-word body
		t.Errorf("destructiveVCSGuardBlock too long: %d words", words)
	}

	pb := &promptBuilder{
		isOneOnOne:  func() bool { return false },
		isFocusMode: func() bool { return false },
		packName:    func() string { return "Test Office" },
		leadSlug:    func() string { return "ceo" },
		members: func() []officeMember {
			return []officeMember{
				{Slug: "ceo", Name: "CEO", Role: "ceo"},
				{Slug: "eng", Name: "Engineer", Role: "eng"},
			}
		},
		policies: func() []officePolicy { return nil },
		nameFor:  func(slug string) string { return slug },
	}
	for _, slug := range []string{"ceo", "eng"} {
		if got := pb.Build(slug); !strings.Contains(got, "== DESTRUCTIVE GIT GUARD") {
			t.Errorf("prompt for %q missing the destructive git guard", slug)
		}
	}
}
