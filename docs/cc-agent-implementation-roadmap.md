# CC-agent Lessons: WUPHF Implementation Roadmap

## Purpose

This roadmap converts the CC-agent analysis into an execution plan for WUPHF.

It is intentionally grouped by:

- fast wins
- medium UX wins
- deep architectural polish

The goal is not to make WUPHF look like CC-agent.

The goal is to import the parts of CC-agent that materially improve:

- operator trust
- moment-to-moment flow
- channel legibility
- direct-session clarity
- long-session resilience
- runtime rigor

while preserving WUPHF's office-first product identity.

Related docs:

- [cc-agent-deep-analysis.md](/Users/najmuzzaman/Documents/nex/WUPHF/docs/cc-agent-deep-analysis.md)
- [cc-agent-phase1-execution-plan.md](/Users/najmuzzaman/Documents/nex/WUPHF/docs/cc-agent-phase1-execution-plan.md)
- [cc-agent-phase2-execution-plan.md](/Users/najmuzzaman/Documents/nex/WUPHF/docs/cc-agent-phase2-execution-plan.md)
- [cc-agent-phase3-execution-plan.md](/Users/najmuzzaman/Documents/nex/WUPHF/docs/cc-agent-phase3-execution-plan.md)
- [TODOS.md](/Users/najmuzzaman/Documents/nex/WUPHF/TODOS.md)

## Guiding Principles

### 1. Preserve the office metaphor

WUPHF should not collapse into a giant solo REPL.

Borrow:

- runtime rigor
- interaction quality
- better state architecture

Do not borrow:

- product shape

### 2. Improve trust before adding new power

The next wins are mostly about:

- clearer state
- safer interruptions
- better recovery
- more truthful UI guidance

not more surface area.

### 3. Prefer reusable primitives over one-off polish

If a behavior is useful in one place, it will likely be needed again.

Examples:

- double-press confirmation
- continue prompts
- status/event cards
- draft stash/restore notices
- reversible search/restore flows

### 4. Make runtime state explicit before over-polishing the chrome

Some UI work can land early, but the highest-leverage improvements come from:

- normalized runtime state
- retained execution artifacts
- session memory

Without those, some polish work will always be bolted on.

## Phase Order

Recommended sequence:

1. Fast wins
2. Medium UX wins
3. Deep architectural polish

This order is deliberate:

- fast wins improve operator feel immediately
- medium wins make the product feel much more serious
- deep work prevents future UI polish from becoming brittle

## Phase 1: Fast Wins

These are small-to-medium changes that materially improve QOL without requiring large architecture changes.

### 1. Contextual Footer and Shortcut Hints

Implement:

- mode-aware composer hints
- office vs `1:1` specific hints
- truthful keybinding display where possible
- contextual hints only when friction is real

Examples:

- long input wraps -> suggest external editor
- active selection mode -> show selection actions only
- blocked approval -> show approve/cancel hints only

Why first:

- very visible win
- low risk
- directly improves operator confidence

Suggested branch:

- `feat/contextual-footer-hints`

### 2. Draft-Preserving History and Recall

Implement:

- preserve current draft before history navigation
- restore draft when coming back down
- preserve cursor location
- preserve mode and pasted content
- add safe prompt recall/search

Why first:

- this is a high-trust improvement
- operators immediately feel when a terminal tool protects their unfinished thought

Suggested branch:

- `feat/draft-safe-history`

### 3. Context-Aware Keyboard Navigation Model

Implement:

- explicit interaction contexts for:
  - chat
  - autocomplete
  - confirmation
  - transcript view
  - task/agent switcher
  - help
- centralized keybinding resolution
- truthful shortcut hints derived from the active context
- better separation between dismiss-overlay and cancel-work semantics

Why first:

- keyboard behavior is one of the biggest hidden quality gaps
- this makes the rest of the TUI easier to evolve without regressions

Suggested branch:

- `feat/context-aware-keybindings`

### 4. Reusable Continue / Double-Press / Pending-State Primitives

Implement:

- shared double-press helper
- shared "press enter to continue" component
- shared temporary pending state treatment
- standard byline patterns for confirm/cancel/continue

Why first:

- small cost
- improves consistency across the whole product

Suggested branch:

- `feat/interaction-primitives`

### 5. Confirm High-Impact Runtime Setting Changes

Implement:

- confirmation step for mid-session changes that materially affect:
  - cost
  - autonomy
  - execution mode
  - provider/runtime behavior
- short explanatory copy at the decision point

Targets:

- provider changes
- autonomy step-up changes
- possibly model/thinking/review-like mode changes if exposed

Suggested branch:

- `feat/runtime-change-confirmations`

### 6. Safety Dialogs for Disruptive Operations

Implement:

- bounded-detail confirmation dialogs before disruptive actions
- clear reason, next effect, and rollback/safety guidance

Targets:

- resets
- transcript restore/rewind
- apply/recover flows
- workflow resume/replay

Suggested branch:

- `feat/safety-dialogs`

### Phase 1 Success Criteria

- Users stop losing drafts unexpectedly
- Shortcuts/help feel more truthful and relevant
- keyboard behavior feels coherent across screens and overlays
- Interrupt/confirm flows feel consistent
- Risky toggles and disruptive actions feel deliberate rather than surprising

## Phase 2: Medium UX Wins

These are product-visible features that make WUPHF feel dramatically more polished and operator-friendly.

### 1. Structured Human Interview Flows

Implement:

- interview mini-flow UI instead of raw transcript-only elicitation
- progress tabs / completion state
- skip/continue semantics
- optional notes
- review-before-submit

### 2. Canonical Agent/Office Switcher

Implement:

- one primary switcher that always includes:
  - main office
  - direct sessions
  - active agent/task transcripts
- unread state
- last-activity context
- "jump back to main" semantics

Why here:

- this is the biggest multi-agent navigation lesson from CC-agent
- it materially improves office coherence without forcing a new product metaphor

Suggested branch:

- `feat/agent-office-switcher`

### 3. Unread Divider and “New Messages” Semantics

Implement:

- first-class unread dividers
- "new since you looked" pill or strip
- better jump-to-latest behavior
- clearer sticky/unread behavior in long channels and direct sessions

Suggested branch:

- `feat/unread-navigation-semantics`

Why it matters:

- this is one of the strongest small-product lessons from CC-agent
- WUPHF already has interviews conceptually, but the UX is still too transcript-shaped

Suggested branch:

- `feat/structured-human-interviews`

### 2. Approval Prompts with Inline Steering

Implement:

- `approve`
- `approve with note`
- `reject`
- `reject with steer`

Examples:

- "yes, but draft first"
- "no, use Gmail not Slack"

Why it matters:

- this captures operator guidance at the exact right moment

Suggested branch:

- `feat/approval-steering`

### 3. Transcript Recovery and Summarize UX

Implement:

- rewind/restore interaction
- summarize from here / summarize up to here
- restore selected context, not only raw replay
- safer transcript surgery for long sessions

Why it matters:

- WUPHF sessions are getting longer
- transcript recovery is currently too primitive for that reality

Suggested branch:

- `feat/transcript-recovery`

### 4. "While You Were Away" Summaries

Implement:

- per-channel away summary
- per-`1:1` away summary
- short, action-oriented recap
- visible "what changed / what next" framing

Why it matters:

- returning to an active office should feel guided, not forensic

Suggested branch:

- `feat/away-summaries`

### 5. Surface Doctor Findings in the Main Flow

Implement:

- blocked state cards in-channel
- setup/readiness status in empty states
- contextual doctor hints without forcing `/doctor`

Why it matters:

- readiness problems should meet the user in the workspace, not hide behind a command

Suggested branch:

- `feat/in-channel-readiness`

### 6. Lightweight Insert/Search Surfaces

Implement:

- richer insert overlays for paths/references
- search surfaces that support insertion, not just navigation
- office/direct authoring helpers beyond raw transcript typing

Why it matters:

- composition becomes faster and more deliberate
- useful especially for workflow specs, long prompts, and recovery flows

Suggested branch:

- `feat/insert-search-surfaces`

### Phase 2 Success Criteria

- Human interviews feel like workflows, not interruptions
- Operator approvals capture steering, not only permission
- Rewind/recovery becomes safe and useful
- Returning to the office after absence feels guided
- Setup issues surface naturally in context

## Phase 3: Deep Architectural Polish

These are the high-leverage systems that make the UI improvements durable.

### 1. Normalized Runtime State Model

Implement a single UI-facing runtime state object that unifies:

- agent activity
- blocked/waiting-human state
- readiness/integration state
- direct vs office semantics
- active execution context

Why it matters:

- this is the foundation for better headers, timeline, doctor, roster, and event cards

Suggested branch:

- `feat/runtime-state-model`

### 2. Rich Execution Artifact Model

Implement retained execution artifacts for:

- tasks
- workflows
- approvals
- interrupts
- external actions

Each should retain:

- started/running/blocked/completed states
- partial outputs
- progress summaries
- resume/review metadata

Why it matters:

- many UI improvements become much easier once work is an object, not just text

Suggested branch:

- `feat/execution-artifacts`

### 3. Session Memory and Context Compaction

Implement:

- session-operational memory distinct from Nex organizational memory
- transcript compaction
- recovery summaries
- better continuity for long-running offices and `1:1` sessions

Why it matters:

- long-lived offices need memory beyond raw scrollback

Suggested branch:

- `feat/session-memory`

### 4. Long-History Virtualization

Implement:

- incremental visible-row rendering
- bounded first paint work
- lower-cost heavy markdown/history rendering

Why it matters:

- caching helps, but long-term transcript scale needs virtualization

Suggested branch:

- `feat/history-virtualization`

### 5. Capability Registry

Implement a normalized registry for:

- tools
- office actions
- direct actions
- Nex capabilities
- action providers
- workflows
- future plugins/skills

Why it matters:

- capability sprawl will otherwise leak into UI and runtime decisions in inconsistent ways

Suggested branch:

- `feat/capability-registry`

### Phase 3 Success Criteria

- UI surfaces draw from one coherent runtime model
- work has retained lifecycle objects, not just transcript traces
- long sessions stay understandable and performant
- capability exposure becomes easier to reason about and safer to evolve

## Cross-Cutting Polish Track

These can happen alongside later phases once the foundations exist.

### 1. Delight and Teaching Layer

Implement:

- smarter empty states
- better waiting copy
- richer welcome/return moments
- progressive teaching hints

### 2. Better Generated Workflow Content Quality

Implement:

- cleaner summaries
- more actionable output formatting
- better final artifact polish

### 3. Stronger Channel Empty and Offline States

Implement:

- more alive previews
- more useful inactive office messaging
- clearer "what can I do here?" cues

## Suggested Branch Order

Recommended order if this becomes a multi-day program:

1. `feat/contextual-footer-hints`
2. `feat/draft-safe-history`
3. `feat/interaction-primitives`
4. `feat/runtime-change-confirmations`
5. `feat/safety-dialogs`
6. `feat/structured-human-interviews`
7. `feat/approval-steering`
8. `feat/transcript-recovery`
9. `feat/away-summaries`
10. `feat/in-channel-readiness`
11. `feat/insert-search-surfaces`
12. `feat/runtime-state-model`
13. `feat/execution-artifacts`
14. `feat/session-memory`
15. `feat/history-virtualization`
16. `feat/capability-registry`

## What Not To Do

Avoid these failure modes:

- do not copy CC-agent's full product shape
- do not replace the office metaphor with a monolithic REPL
- do not land superficial visual polish without runtime state cleanup
- do not add more status chrome without making it more selective
- do not make every improvement transcript-shaped if it is really a workflow

## Practical Recommendation

If only the next 2-3 branches are funded right now, do:

1. contextual footer/help
2. draft-preserving history/recall
3. structured human interviews

That combination gives WUPHF the highest short-term quality jump per unit of effort:

- composition feels safer
- help feels smarter
- interviews become dramatically better

If a larger investment is available, the real inflection point is:

- normalized runtime state
- execution artifacts
- session memory

That trio is where WUPHF starts gaining CC-agent-level rigor without losing its own identity.
