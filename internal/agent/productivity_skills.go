package agent

// productivitySkills is a set of cross-cutting skills ported (with
// permission of the licence) from https://github.com/mattpocock/skills.
// They're useful for any engineering / planning team, so we append them
// to every starter pack's DefaultSkills rather than gating on role.
//
// Each skill's content is generic — no Hanger8200 / Gitea / hanger8200-mcp
// references. Issue-tracker hooks call `gh issue create` (the GitHub CLI)
// since that's the closest thing to a universal default; users on GitLab /
// Gitea can substitute their own CLI without changing the skill body.
var productivitySkills = []PackSkillSpec{
	{
		Name:        "grill-me",
		Title:       "Grill Me",
		Description: "Stress-test a plan or design through sequential single-question interview until both parties align on fundamentals.",
		Tags:        []string{"planning", "review", "interview"},
		Trigger:     "User says 'grill me', 'grill the plan', 'stress-test this', or asks for an intensive design review.",
		Content: `# Grill Me

You are conducting an intensive design review. Your job is to stress-test the user's plan by asking targeted questions, one at a time, until you and the user are aligned on the fundamentals.

## Rules

- **One question per turn.** Do not bundle. Wait for the user's answer before asking the next.
- **Walk decision branches methodically.** When the user picks a path, follow it down before backtracking.
- **Resolve interdependencies.** When a choice forces another choice, surface that link explicitly.
- **Always offer your recommended answer** alongside the question. Do not be neutral. If you have a strong opinion based on the codebase or the user's prior decisions, say so.
- **Reference the codebase when applicable.** If a relevant brief exists in the wiki, ground your questions in it instead of asking the user to repeat what is already documented.
- **No summaries.** Pursue comprehensive understanding. The goal is alignment, not a status report.

## Stop conditions

Stop when one of the following is true:
1. Every load-bearing decision has been answered explicitly.
2. The user says they are aligned and want to move on.
3. The plan has visibly changed enough that it should be re-grilled from the top.

## Output

After alignment, post one short summary listing every decision that landed and any open questions you parked. Do not write a PRD here — that is the to-prd skill.

Adapted from https://github.com/mattpocock/skills (productivity/grill-me).`,
	},
	{
		Name:        "grill-with-docs",
		Title:       "Grill With Docs",
		Description: "Like grill-me, but grounded in the wiki — flags terminology conflicts against project glossaries and ADRs.",
		Tags:        []string{"planning", "review", "wiki"},
		Trigger:     "User says 'grill with docs', 'check this against our docs', or before writing a spec for an entity that already has wiki coverage.",
		Content: `# Grill With Docs

Same protocol as grill-me, but every question is grounded in what the wiki already says.

## Domain grounding

Before the first question, run a single combined search:

1. Search the wiki for the load-bearing nouns in the user's plan (3–6 terms max).
2. Read every brief that scores high — especially anything under team/projects/, team/decisions/, or team/people/.
3. Build an internal mental glossary: term → wiki definition → what the user seems to mean.

## During the interview

- **Flag terminology conflicts immediately.** Format: "Your team/projects/<X>.md defines _term_ as A, but you seem to mean B. Which is correct?"
- **Stress-test relationships with concrete edge cases.** "If a customer cancels mid-renewal, does that hit the X flow you described, or the Y flow? They handle billing differently per team/decisions/billing.md."
- **Skip questions when the wiki already answers them.** Cite the brief; do not re-litigate.

## Documentation hygiene

Update the wiki **inline** as decisions crystallize:
- Add new glossary terms to the relevant team/projects/<slug>.md.
- Create a new team/decisions/<slug>.md ADR if **all three** are true:
  1. The decision is hard to reverse.
  2. The decision will surprise a future reader without context.
  3. The decision is the result of a real trade-off.
  Otherwise: do not write an ADR. Decisions that don't meet all three rot fast.

## Output

Post a one-paragraph summary: what landed, what got documented, what stayed open. Link every wiki page you touched.

Adapted from https://github.com/mattpocock/skills (engineering/grill-with-docs).`,
	},
	{
		Name:        "to-issues",
		Title:       "To Issues",
		Description: "Break a plan into independently-grabbable issues using vertical slices (tracer bullets).",
		Tags:        []string{"planning", "issues"},
		Trigger:     "User says 'turn this into issues', 'create issues for this plan', or after grill-me / to-prd produces an aligned plan.",
		Content: `# To Issues

Break a plan into independently-grabbable issues using vertical slices.

## 1. Gather context

Work from whatever is in the conversation context. If the user passes an issue number or URL, fetch it via your VCS tool (e.g. ` + "`gh issue view <number>`" + `) and read the relevant entries.

## 2. Explore the codebase (optional)

If you have not already explored the relevant code, do so now. Search the wiki for the load-bearing nouns and read any team/projects/<slug>.md briefs. Issue titles must use the project's vocabulary, not invented terms.

## 3. Draft vertical slices

Break the plan into **tracer bullets**. Each issue is a thin vertical slice that cuts through every layer end-to-end (schema → API → UI → tests), NOT a horizontal slice of one layer.

Slices are either **HITL** (require human interaction — architectural decision, design review, manual QA) or **AFK** (can be implemented and merged without human interaction). Prefer AFK over HITL.

Rules:
- Each slice delivers a narrow but COMPLETE path through every layer.
- A completed slice is demoable or verifiable on its own.
- Prefer many thin slices over few thick ones.

## 4. Quiz the user

Present the breakdown as a numbered list. For each slice, show:
- **Title**: short descriptive name
- **Type**: HITL / AFK
- **Blocked by**: which other slices (if any) must complete first
- **User stories covered**: which user stories this addresses

Iterate until the user approves.

## 5. Create the issues

For each approved slice, call your VCS tool (e.g. ` + "`gh issue create`" + `) with the issue body template below. Create issues in dependency order so you can reference real issue numbers in "Blocked by".

` + "```" + `
## Parent

#<parent-issue-number>   (omit if not derived from an existing issue)

## What to build

Concise description of this vertical slice. Describe end-to-end behavior, not layer-by-layer implementation.

## Acceptance criteria

- [ ] Criterion 1
- [ ] Criterion 2
- [ ] Criterion 3

## Blocked by

- Blocked by #<issue-number>     (or "None — can start immediately")
` + "```" + `

Do NOT close or modify the parent issue.

Adapted from https://github.com/mattpocock/skills (engineering/to-issues).`,
	},
	{
		Name:        "tdd",
		Title:       "Test-Driven Development",
		Description: "Red-green-refactor with vertical tracer-bullet slices — one test, one implementation, repeat.",
		Tags:        []string{"coding", "testing", "tdd"},
		Trigger:     "User says 'TDD this', 'write tests first', or starts a new feature with a defined interface.",
		Content: `# Test-Driven Development

## Philosophy

Tests verify behavior through public interfaces, not implementation details. A test that breaks when you rename an internal function was testing implementation, not behavior. Good tests survive refactors.

## Anti-pattern: horizontal slicing

**DO NOT write all tests first, then all implementation.** That produces tests that:
- Verify imagined behavior, not actual.
- Test the shape of things (signatures, data structures), not user-facing behavior.
- Pass when behavior breaks, fail when behavior is fine.

**Correct approach: vertical slices.** One test → one implementation → repeat. Each test responds to what you learned from the previous cycle.

` + "```" + `
WRONG (horizontal):
  RED:   test1, test2, test3, test4, test5
  GREEN: impl1, impl2, impl3, impl4, impl5

RIGHT (vertical):
  RED→GREEN: test1→impl1
  RED→GREEN: test2→impl2
  ...
` + "```" + `

## Workflow

### 1. Plan

Before writing code, search the wiki for the relevant project. Test names and interface vocabulary should match the wiki's project vocabulary.

Confirm with user:
- What interface changes are needed?
- Which behaviors are most important to test? (Prioritize.)
- Are there opportunities for **deep modules** (small interface, deep implementation)?

You can't test everything. Focus on critical paths and complex logic.

### 2. Tracer bullet

Write ONE test that confirms ONE thing about the system:
` + "```" + `
RED:   Write test for first behavior → fails
GREEN: Write minimal code to pass → test passes
` + "```" + `
This proves the path works end-to-end.

### 3. Incremental loop

For each remaining behavior:
` + "```" + `
RED:   Write next test → fails
GREEN: Minimal code to pass → passes
` + "```" + `

Rules: one test at a time, only enough code to pass current test, don't anticipate future tests, focused on observable behavior.

### 4. Refactor

After all tests pass:
- Extract duplication.
- Deepen modules (move complexity behind simple interfaces).
- Run tests after each refactor step.

**Never refactor while RED.** Get to GREEN first.

Adapted from https://github.com/mattpocock/skills (engineering/tdd).`,
	},
	{
		Name:        "diagnose",
		Title:       "Diagnose",
		Description: "Six-phase debugging discipline for hard bugs — feedback loop first, hypothesise second, instrument third.",
		Tags:        []string{"debugging", "incident"},
		Trigger:     "User reports a bug that needs structured investigation, especially intermittent / production / multi-layer bugs.",
		Content: `# Diagnose

A discipline for hard bugs. Skip phases only when explicitly justified.

## Phase 1 — Build a feedback loop

**This is the skill. Everything else is mechanical.** If you have a fast, deterministic, agent-runnable pass/fail signal for the bug, you will find the cause. If you don't, no amount of staring at code will save you. Spend disproportionate effort here.

Try in roughly this order:
1. **Failing test** at whatever seam reaches the bug.
2. **curl / HTTP script** against a running dev server.
3. **CLI invocation** with a fixture input, diff stdout against known-good.
4. **Headless browser** (Playwright) — drives the UI, asserts on DOM/console/network.
5. **Replay a captured trace** — save real network request / payload / log to disk; replay through the code path in isolation.
6. **Throwaway harness** — minimal subset that exercises the bug code path with one function call.
7. **Property / fuzz loop** — for "sometimes wrong output" bugs.
8. **Bisection harness** — automate "boot at state X, check, repeat" so you can ` + "`git bisect run`" + `.

### Iterate on the loop itself

Treat the loop as a product. Once you have *a* loop:
- Make it faster (cache setup, narrow test scope).
- Make the signal sharper (assert on the specific symptom, not "didn't crash").
- Make it deterministic (pin time, seed RNG, isolate filesystem, freeze network).

A 30-second flaky loop is barely better than nothing. A 2-second deterministic loop is a debugging superpower.

### Non-deterministic bugs

The goal is **higher reproduction rate**, not a clean repro. Loop the trigger 100×, parallelise, add stress, narrow timing windows.

### When you cannot build a loop

Stop and say so. List what you tried. Ask the user for: (a) access to the env that reproduces, (b) a captured artifact, or (c) permission to add temporary instrumentation.

## Phase 2 — Reproduce

Run the loop. Watch the bug appear. Confirm: it produces the failure mode the user described (not a different one nearby), reproducible across multiple runs, exact symptom captured.

## Phase 3 — Hypothesise

Generate **3–5 ranked hypotheses before testing any of them**. Each must be **falsifiable**: state the prediction.

> Format: "If X is the cause, then changing Y will make the bug disappear / changing Z will make it worse."

Show the ranked list to the user before testing. They often have domain knowledge that re-ranks instantly.

## Phase 4 — Instrument

Each probe maps to a specific prediction from Phase 3. **Change one variable at a time.** Tag every debug log with a unique prefix, e.g. ` + "`[DEBUG-a4f2]`" + `. Cleanup at the end is a single grep.

## Phase 5 — Fix + regression test

Write the regression test **before the fix** — but only if there is a **correct seam** for it. If no correct seam exists, that itself is the finding — flag for the improve-codebase-architecture skill.

## Phase 6 — Cleanup + post-mortem

Required: original repro no longer reproduces, regression test passes, all [DEBUG-...] instrumentation removed, throwaway prototypes deleted, the hypothesis that turned out correct is stated in the commit / PR message.

**Ask: what would have prevented this bug?** If architectural, hand off to improve-codebase-architecture with specifics.

Adapted from https://github.com/mattpocock/skills (engineering/diagnose).`,
	},
	{
		Name:        "improve-codebase-architecture",
		Title:       "Improve Codebase Architecture",
		Description: "Surface deepening opportunities — refactors that turn shallow modules (interface ≈ implementation) into deep ones.",
		Tags:        []string{"architecture", "refactoring", "review"},
		Trigger:     "User says 'where is this codebase weak?', 'find refactor candidates', or after a bug-fix turns up architectural smell.",
		Content: `# Improve Codebase Architecture

Surface architectural friction. Propose refactors that improve testability and AI-navigability.

## Vocabulary (precise, do not blur)

- **Module** — anything with an interface and an implementation.
- **Depth** — behavioral leverage at the interface. Deep = small interface, large implementation.
- **Seam** — where an interface lives. A place behavior can be altered without editing in place.
- **Locality** — concentrated change, bugs, and knowledge inside the module instead of scattered across callers.

## The deletion test

If you delete this module, does complexity *concentrate at callers* (module earned its keep — kept depth there) or does it *merely relocate* (module was shallow, just shuffled bytes)? Shallow modules are refactor candidates.

## Three-phase process

### 1. Explore

Search the wiki for project briefs and ADRs that constrain the area. Note friction points where:
- Understanding bounces between modules to make sense of one change.
- An interface is nearly as complex as its implementation.
- One module's tests have to know intricate details about another module's internals.
- A bug fix in one module forced edits in three others.

Do NOT propose interfaces yet.

### 2. Present candidates

Numbered list. For each candidate:
- **Files** affected.
- **Problem statement** — what friction exists today, in one paragraph.
- **Proposed deepening** — what could move behind a smaller interface.
- **Benefits** — what becomes easier (testing, change locality, future extension).

Use vocabulary from the wiki's project briefs. Do not invent new terms.

### 3. Grilling loop

Collaboratively design the deepened module with the user. Updates happen inline:
- New domain terms → write to the relevant team/projects/<slug>.md.
- Rejected candidates with load-bearing reasons → write team/decisions/<slug>.md (ADR) only if the three ADR criteria hold.

Surface ADR conflicts only when friction justifies revisiting. Do not re-litigate every theoretical refactor an existing ADR forbids.

Adapted from https://github.com/mattpocock/skills (engineering/improve-codebase-architecture).`,
	},
	{
		Name:        "zoom-out",
		Title:       "Zoom Out",
		Description: "Surface higher-level architectural / strategic context when the conversation has drifted into low-level detail.",
		Tags:        []string{"strategy", "review"},
		Trigger:     "User says 'zoom out', 'what's the bigger picture?', or the agent has been spelunking three layers deep without surfacing.",
		Content: `# Zoom Out

You have been working at low altitude. Step back.

## Move

1. Restate the **goal** in one sentence using vocabulary from the relevant team/projects/<slug>.md brief — not the implementation language you have been swimming in.
2. List the **module relationships** that matter for this goal: which modules talk to which, and which seams the work crosses.
3. Identify the **caller hierarchy**: who depends on the thing being changed; who would be affected if it broke.
4. Name the **alternative paths** you considered or rejected — even briefly. The path you are on is one of N.

If team/projects/<slug>.md does not exist for this area, that itself is a finding — flag it. Do not invent vocabulary.

## Output

A short message:
- **Goal:** one sentence.
- **Where we are:** which seam you are currently editing.
- **Why this seam:** one sentence.
- **What this changes upstream / downstream:** one bullet each.
- **Alternative paths:** up to two, with one-line trade-offs.

Do not propose implementation. Do not write code. The only output of zoom-out is the orientation summary.

Adapted from https://github.com/mattpocock/skills (engineering/zoom-out).`,
	},
	{
		Name:        "to-prd",
		Title:       "To PRD",
		Description: "Convert current conversation context into a Product Requirements Document and write it to the wiki.",
		Tags:        []string{"planning", "prd"},
		Trigger:     "User says 'turn this into a PRD' or after grill-me/grill-with-docs lands an aligned plan.",
		Content: `# To PRD

Convert the current conversation context into a Product Requirements Document. Optionally submit it as an issue.

## 1. Explore the repo

Search the wiki for load-bearing nouns. Read any team/projects/<slug>.md and team/decisions/<slug>.md that overlap. Use the wiki's vocabulary in the PRD — do not invent terms.

## 2. Design modules

Identify the major components needed. Emphasize **deep modules** — components that encapsulate a lot of functionality behind a simple, testable interface that rarely changes. List each one with:
- **Name** (use wiki vocabulary).
- **Responsibility** in one sentence.
- **Interface sketch** (function signatures or REST routes; not full code).

## 3. Validate with user

Show the proposed module list. Ask:
- Are these the right modules?
- Are any too shallow (interface nearly as complex as implementation)?
- What is the hardest behavior to test, and at which seam should the test live?

## 4. Generate the PRD

Use the template below. Pull from existing context — do **not** interview the user for things that are already in the conversation or the wiki.

` + "```" + `
# <Title>

## Problem

<One paragraph. What is broken / missing / slow today, in user terms.>

## Solution

<One paragraph. What we are building, in user terms. Defer implementation.>

## User stories

- As a <role>, I want <capability>, so that <outcome>.
- (Cover every user-facing behavior the solution unlocks.)

## Implementation

### Modules

<List from step 2, with responsibilities + interface sketches.>

### Decisions

- <Decision> — <Reason>. (Cite team/decisions/<slug>.md if applicable.)

## Testing

<Which behaviors get tests, at which seams. Reference the tdd skill.>

## Out of scope

- <Thing the reader will assume is in scope but is not.>
` + "```" + `

## 5. Output

- Write the PRD to team/projects/<slug>.md via the wiki write tool.
- If the user wants an issue, call your VCS tool (e.g. ` + "`gh issue create`" + `) with the PRD body.
- Post a one-line summary to the channel with links.

Adapted from https://github.com/mattpocock/skills (engineering/to-prd).`,
	},
}

// AppendProductivitySkills returns base with the productivitySkills slice
// appended. Used by every starter pack so any new install gets the whole
// set without having to re-list each one in three places.
func AppendProductivitySkills(base []PackSkillSpec) []PackSkillSpec {
	out := make([]PackSkillSpec, 0, len(base)+len(productivitySkills))
	out = append(out, base...)
	out = append(out, productivitySkills...)
	return out
}
