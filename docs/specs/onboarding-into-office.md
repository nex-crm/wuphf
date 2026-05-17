# Design — Onboarding into the Office

**Branch:** `design/onboarding-into-office`
**Status:** Draft for review
**Author:** Design consultation, 2026-05-17
**Replaces:** `web/src/components/onboarding/Wizard.tsx` (9-step modal) and the
hard-cut transition into `Shell` after `/onboarding/complete`.

---

## TL;DR

The current onboarding is a 9-step modal wizard that hard-cuts to an office the
user has not yet seen. New users get all the questions front-loaded, the office
appears fully formed in one frame, and agents start running on the first task
before the user has any trust calibration. We replace it with:

1. **One pre-office screen** — provider selection (Claude Code / Codex /
   Opencode). Everything else moves into the office.
2. **An empty office shell with a DM open to CEO.** The user sees their office
   from frame 1, but it is quiet. The CEO greets them and asks the first
   question. The shell **populates live** as decisions are made: channels
   fade in, agents slide into the sidebar, the workspace label updates.
   The early turns are pure form-fills (workspace name, description, website
   URL, blueprint pick, team trim) plus a deterministic website scan that
   seeds the wiki. **No LLM tokens spent in this phase.** The LLM only
   starts speaking once the user describes a first issue.
3. **An issue-first planning loop.** Tasks are upgraded to **Issues** — proper
   spec documents with comments, sub-issues, and an explicit human approval
   gate. CEO drafts the issue with the user; CEO can suggest pulling other
   agents in to comment during drafting. **Nothing executes until the user
   approves the issue.** This is the trust-building beat the current product
   does not have.
4. **One agent cast across two stages.** No separate "planner" role. The same
   Engineer, Designer, PM, etc. comment during drafting and then execute after
   approval. Stages are a property of the issue, not the agents.

Result: the office feels like it is forming around the user as they decide what
to do, and they get to plan before anything runs. Activation goes from
*see → trust*: the user sees a coherent office and gets one trustworthy first
output (an approved issue) before any execution happens.

---

## Eng review decisions (2026-05-17)

`/plan-eng-review` locked these decisions and folded the scope reductions back
into the implementation phases. Each line is a load-bearing decision that
should not be revisited without going through the same review.

### Scope reduction
- **Sub-issues + wiki mirror deferred to Phase 6.** Phase 4 v1 ships the issue
  document, comments timeline, Approve & Start gate, and execution lineup
  dispatch. Sub-issue semantics (parent-child, dispatch order, cascading
  completion) and wiki mirror on approval move to a Phase 6 follow-up. Cuts
  ~3 dev-days off v1 with no loss to the trust-calibration goal.

### Architecture
- **Approval gate: server-side enforcement in the broker.** Adds
  `isExecutableTeamTaskStatus` (true only for `LifecycleStateRunning` and
  `LifecycleStateApproved`). Every dispatch entry point goes through the
  guard: `PamDispatcher`, `headless codex`, self-heal, agent-to-agent
  dispatch, external triggers. Comments allowed in any lifecycle state.
- **Issue surface reuses existing lifecycle states.** No new "Issue" primitive.
  An issue IS a lifecycle task with a richer presentation. Status pill maps
  to existing `LifecycleState`; `Approve & Start` = existing `approve`
  action; comments use the existing comment infrastructure (already wired
  per `broker_inbox_handler.go:229`). Adds one new state
  `LifecycleStateDrafting` for the pre-Intake "agents can comment, not
  dispatch" mode. Cuts ~40% off Phase 3 scope.
- **CEO transcript lives in `b.messages`, not in onboarding state.** The CEO
  DM is a real DM (`dm:ceo:onboarding` reserved slug). Inherits SSE,
  persistence, replay, search for free. `state.CEOTranscript` is NOT
  introduced.
- **Staged form answers + atomic seed at `seed` phase.** User answers update
  `state.FormAnswers`. Sidebar "office forming" is a frontend overlay
  rendering preview rows from `state.FormAnswers`. The atomic
  `seedFromBlueprintLocked` (existing) runs once at the `seed` phase
  boundary. No broker mutation refactor required.
- **Onboarding completes at end of `bridge` phase regardless of first issue.**
  `state.CompletedAt` is set when the user picks "Start an issue" OR
  "Look around first" at `bridge`. Adds separate
  `state.FirstIssueApprovedAt *time.Time` for activation-depth tracking.
  Marcus path (look around first) is a fully onboarded user.
- **Explicit phase cursor + `PendingSuggestion` for resumption.** No
  transcript replay. State has `Phase string` + `PendingSuggestion
  *Suggestion`. On reopen, if `PendingSuggestion != nil`, re-emit it
  (idempotent by `Suggestion.ID`). Transcript is for users to read, not
  for the machine to interpret.
- **Scratch path uses `seedMinimalScratchLocked` (new), not
  `synthesizeBlueprintFromState`.** Scratch seed at `seed` phase commits
  exactly `#general` + 2 wiki pages + CEO. No fake-synthesized team.
  When the user describes a task at `draft` phase, CEO's LLM call
  proposes the team inline and they're added via the broker's existing
  incremental agent-add path.

### Code quality
- **Frontend reuses `DMView` for the CEO chat.** A small `OnboardingDMRoute`
  wrapper provides the preview-overlay context and points `DMView` at
  the reserved `dm:ceo:onboarding` channel. No new chat shell, no new
  composer, no duplicated SSE/scroll/optimistic-post code.
- **Suggestion cards extend `InterviewBar`/`HumanInterviewOverlay`.** New
  message kinds: `ceo_form_field`, `ceo_chip_row`, `ceo_checklist`,
  `ceo_team_trim`, `ceo_scan_chip`. All routed through the existing
  kind-dispatcher. Inherits the `sanitizeContextValue` Go-side
  sanitization fix from PR #684 (the confused-deputy bypass test).

### Tests (load-bearing)
- **Sanitization regression as Phase 2 acceptance gate.** New file
  `broker_onboarding_sanitize_test.go`: every new `ceo_*` kind exercised
  with the PR #684 attack-string set; payload must pass through
  `sanitizeContextValue` before any broker write.
- **Approval gate parametric test as Phase 4 acceptance gate.** New file
  `broker_lifecycle_dispatch_test.go`: parametric over every dispatch
  entry point. Negative cases (Drafting, Intake, Review,
  ChangesRequested) all reject with `ErrIssueNotApproved`. Positive
  cases (Running, Approved) all allow. Comment path allowed in all
  states.
- **E2E for all three ICP scenarios.** Sam (scratch), Priya (blueprint),
  Marcus (look around first). Marcus path asserts ZERO LLM provider calls
  made across the entire flow.

### Performance
- **State persisted on phase transition + form-field commit.** No
  per-keystroke disk I/O. Intra-field typing stays client-side until
  submit. ~10-12 writes to `onboarded.json` per full onboarding.

---

## Design review decisions (2026-05-17)

`/plan-design-review` locked these decisions and folded them into the spec.
Each line is a load-bearing visual + interaction choice that should not be
revisited without going through the same review.

### Focal hierarchy + layout
- **Office-entry frame focal hierarchy:** PRIMARY = CEO greeting + first
  `form_field` card (only purple affordance on the page). SECONDARY = sidebar
  group labels at `--text-tertiary` (helper text SUPPRESSED until phase
  advances past `greet`), ChannelHeader showing 'CEO' + status dot.
  TERTIARY = workspace rail tile (empty placeholder), StatusBar (minimal copy).
- **Issue document layout:** single-column scroll at max-width 720px. Sticky
  status pill header + sticky button row. Spec sections (Goal/Context/
  Approach/Acceptance) + comments timeline in one scrollable column. After
  first approval, spec sections auto-collapse into a 3-line summary card
  with `Expand spec` affordance; comments timeline fills the area. Return
  visits anchor scroll to last-unread comment.

### State coverage
- **Card states matrix.** Every card kind moves through three universal
  stages: `pending` (interactive, primary affordance visible), `submitting`
  (controls disabled, subtle spinner inside primary button only), `committed`
  (collapsed to a one-line confirmation in the timeline using
  `--text-secondary` with a small leading `✓`). Per-kind error states layer
  on top (inline validation for `form_field`, banner above for chip-row /
  checklist, distinct `failed` state for `scan_chip`).
- **Submitting state never animates** beyond the inline spinner. No
  full-card skeleton, no progress bar, no row-shimmer.

### Journey (LLM phase pacing)
- **Stream the draft as it generates.** On `bridge → draft` transition, the
  Issue document opens immediately with empty sections (status: Drafting).
  CEO streams each section in order (Goal → Context → Approach → Acceptance)
  with a typing-dot prefix on the unwritten section. In the CEO DM, a
  one-line acknowledgement `Drafting #142 → see issue` with a Jump
  affordance. No spinner. No 'CEO is thinking' modal. The wait IS the
  experience — same pattern as Claude Code's plan mode.

### AI-slop guardrails
- **CEO voice canon — deterministic templates locked verbatim in spec.**
  Add a `## CEO Voice — deterministic templates` section with exact strings
  per phase (greet / identity 4 fields / scan 3 variations / blueprint /
  team trim / seed success x2 / bridge 2 outcomes). Rules: no 'Welcome',
  no 'I'm your AI', no preamble, declarative, low word count, lightly
  funny if natural but never quippy. CEO does not introduce himself.
  Example reference copy:
  - `greet`: `Office name?`
  - `seed-done (scratch)`: `✓ Empty office, your call. Start an issue, or look around?`
  - `bridge > look around`: `✓ I'll be in #general when you need me.`

### Design system / token bindings
- **Bind every new surface to existing `global.css` tokens.** Spec lists
  exact CSS-variable bindings: sidebar empty group label =
  `--text-tertiary`; suggestion card border = `--border` 1px; primary
  affordance = `--accent` (#9F4DBF); committed line text =
  `--text-secondary`.
- **Add 3 new preview-state tokens** for the staged sidebar overlay:
  `--preview-row-bg` = `--accent-bg` (lavender tint, already exists);
  `--preview-row-border` = dashed 1px `--accent-bg-strong`;
  `--preview-row-text` = `--text-secondary`. Visual language: dashed
  border + lavender bg = "this thing is staged, not committed yet."
- **Status pills reuse the existing `LifecycleStatePill` component.** Add
  ONE new entry to `STATE_PILL_TOKENS` for the new `drafting` state:
  `bg: var(--accent-bg)`, `text: var(--accent)`, `label: "drafting"`.
  Distinct from intake/ready (which use `--bg-row-active`) — signals
  "needs human attention" via the brand accent.
- **Reuse `PixelAvatar`** for CEO and all agent avatars; do not introduce a
  new avatar system.

### Responsive
- **Mobile (< 768px) is explicitly de-scoped for v1, with NO notice or
  redirect.** Whatever the desktop layout renders at narrow widths is what
  the user sees. WUPHF is a local dev tool; mobile is genuine edge case.
  Not tracked as a follow-up.

### Accessibility
- **Keyboard model per card kind.**

  | Card | Tab | Arrow | Enter | Space |
  |---|---|---|---|---|
  | `form_field` | input focus | — | submit | type |
  | `chip_row` | into row | move chip selection | commit | commit |
  | `checklist` | item | — | Submit button | toggle |
  | `team_trim` | item | — | Submit button | toggle |
  | `scan_chip` | — (read-only) | — | — | — |

- **Focus management on commit.** After card N commits, focus moves to
  card N+1's first interactive element. On `seed` phase commit, focus moves
  to the bridge-phase chip row.
- **ARIA live region** for the sidebar preview overlay: `aria-live="polite"`,
  announces each preview-row enter as `"Added channel #billing"` etc.
  Streaming CEO message during `draft` shares the region but is throttled
  (announce final text only, not each token).
- **Reduced motion** rules extend to streaming drafts: when
  `prefers-reduced-motion: reduce`, the entire response renders instantly
  with no typing dots.
- **Color contrast** verification: `--text-secondary` on `--accent-bg`
  must meet WCAG AA (4.5:1 ratio). If verification fails, bump to
  `--text` on `--accent-bg` (which definitely passes).
- **Deferred to a follow-up:** richer screen-reader hints, voice
  navigation, RTL layout testing.

### Resolved transitions
- **Marcus "look around first" exit moment.** CEO posts final line
  `✓ I'll be in #general when you need me.` Sidebar groups unlock fully
  (helper text removed). A one-time `IssuesEmpty` placeholder renders in
  the main area with `+ New issue` button and copy `No issues yet. When
  you're ready, type what you want done.` Dismisses on first issue
  created, never returns. CEO DM does NOT auto-open on next session;
  user lands on last-visited channel or `#general` default.
- **Provider-pick → office transition.** Same-tab navigation with a
  350ms cross-fade through a brief loading state. T+0: card press (180ms)
  → T+0.18: page fades to white → T+0.36: loading line `Opening your
  office…` on neutral background → T+0.4-1.0: broker initializes →
  T+1.0: loading fades → T+1.18: office shell visible, CEO greeting
  starts streaming. If broker init exceeds 800ms, a 2px indeterminate
  progress line appears under the loading text.

---

## ICP tutorial examples (test the design against these before declaring done)

Per global rules, the design must work end-to-end for three real users:

### 1. Sam — solo founder, billing service (scratch path)
- Runs `./wuphf`, picks Claude Code on the pre-office screen, lands in
  the office.
- **Deterministic chat begins** (no LLM calls).
- CEO: *"Welcome. What should we call this office?"* — form-field message
  inline in chat. Sam types *"Acme Billing"*. Workspace rail label updates
  with a 240ms cross-fade.
- CEO: *"What's the short description?"* — Sam: *"Subscription billing for
  indie SaaS."*
- CEO: *"Top priority right now? (Optional.)"* — Sam skips with the chip
  *"Not yet."*
- CEO: *"Got a website I can scan for context?"* — Sam pastes `acme.com`.
  CEO acknowledges with a scan-progress chip in the chat (*"Scanning
  acme.com…"*). `POST /onboarding/scan` runs in the background; the chat
  keeps moving.
- CEO: *"Your name? Your role? (Optional.)"* — Sam fills both.
- CEO: *"Pick a starter template, or start from scratch:"* — chips render
  inline (Bookkeeping / Content Ops / Engineering Team / Start from scratch).
  Sam clicks **Start from scratch**. **No team picker step in scratch path.**
- Scratch path materializes: `#general` channel, a minimal wiki scaffold
  (`Company Facts` placeholder + `Decisions`). When the website scrape
  returns ~3 seconds later, the chip in chat flips to *"Wiki updated with
  acme.com facts ✓"* and `Company Facts` gets a content fade in the wiki.
- CEO: *"All set up. What do you want to start working on?"* — this is the
  handoff message from deterministic mode.
- **LLM mode begins.** Sam: *"Get Stripe webhooks working."*
- CEO drafts issue `#1 Stripe webhook handler` in the main area (first LLM
  call). Because Sam went scratch, CEO ALSO proposes an agent roster as
  part of the draft: *"For this work I'd bring in Founding Engineer + PM.
  Add?"* Sam approves; agents slide into sidebar with intro lines posted
  in the issue's comments timeline.
- Engineer posts technical sanity-check comment (LLM). Sam edits acceptance
  criteria. Clicks **Approve & Start**. Execution dispatches.

### 2. Priya — design lead, content ops for an agency (blueprint path)
- Picks Codex on the pre-office screen. Office opens.
- **Deterministic chat.** CEO asks the same identity questions as in Sam's
  flow (office name, description, priority, website, owner). Priya answers
  briefly and lets the website scrape run in the background.
- CEO: *"Pick a starter template, or start from scratch:"* — Priya clicks
  the **Content Ops** chip. Still deterministic — no inference.
- Blueprint materializes: `#editorial` and `#assets` channels fade into
  the sidebar with stagger; wiki scaffold pages (Style Guide, Editorial
  Calendar, Brand Voice) appear under their wiki tree.
- CEO: *"This blueprint comes with a team — keep or trim:"* — renders a
  deterministic checklist of agents (Writer ✓, Editor ✓, Designer ✓).
  Priya unchecks Writer (*"We use a real writer"*) and submits.
- On submit, Editor + Designer slide into sidebar with intro lines posted
  in the CEO DM. Still no LLM call.
- CEO: *"All set up. Want to start an issue?"* — Priya: *"Content calendar
  for May."*
- **LLM mode begins.** CEO drafts `#1 Content calendar for May`. Priya
  rewrites the acceptance criteria. CEO suggests pulling Designer in for
  asset review. Designer comments. Priya approves. Work starts.

### 3. Marcus — engineering manager, evaluating WUPHF (blueprint, no issue)
- Picks Claude Code. Office opens.
- **Deterministic chat.** Marcus answers the identity questions tersely
  and skips the website URL (he's evaluating, not bringing a real company).
- CEO: *"Pick a starter template, or start from scratch:"* — Marcus picks
  **Engineering Team**.
- Channels (`#engineering`, `#standup`) materialize; wiki scaffold (Eng
  Practices, Architecture Decisions) appears.
- CEO: *"This blueprint comes with a team — keep or trim:"* — Marcus
  accepts the default roster (Founding Engineer, PM, Designer). Agents
  slide into sidebar.
- CEO: *"All set up. Want to start an issue, or look around first?"* —
  Marcus picks **Look around first** via a chip. **No LLM call happens.**
  Office stays real and quiet. Marcus reads agent profiles, browses the
  wiki, hovers over Issues (empty), and can return to start an issue any
  time via `+ New issue` (which re-enters the LLM `draft` phase). Trust
  through inertia, not action.

All three flows must succeed end-to-end before this design ships.

---

## What we are deliberately cutting from the wizard

Current `STEP_ORDER`: `welcome → identity → templates → team → setup → nex →
task → ready` (plus a hidden `analysis` step).

| Step | Decision | Why |
|---|---|---|
| `welcome` | Cut | CEO's first message is the welcome. |
| `identity` | Move into chat | CEO asks naturally. The wizard's separate identity form is friction. |
| `templates` | Render as chip picker inside chat | **Deterministic.** CEO posts the blueprint chips as a structured message. No LLM inference of blueprint from free text — the user picks. |
| `analysis` | Keep but render in chat | The `POST /onboarding/scan` of the user's website URL is the same backend call; chat shows a scan-progress chip. Environment probes (runtime detection) remain silent unless they fail. |
| `team` | Render as checklist in chat **only on blueprint path** | Deterministic trim for blueprint path: same multi-select as today, rendered as a CEO message. Scratch path skips this entirely; CEO proposes agents during issue drafting (the only LLM moment in the team flow). |
| `setup` | **Keep, pre-office** | Provider selection has to happen before any agent can run. This is the only true blocker. |
| `nex` | **Drop from onboarding** | Optional integration. Move to Settings → Integrations. Nothing depends on it for the first issue. |
| `task` | Replace with issue drafting | Old flow: type a sentence and the agent starts. New flow: CEO drafts an issue with you, you approve, then it starts. |
| `ready` | Cut | The Approve & Start button on the first issue is the readiness gate. |

Net: **1 pre-office screen + a chat that builds the office around you**, instead
of 9 modal steps the user clicks through before seeing anything real.

---

## Surface 1 — Pre-office: provider selection

A single full-bleed screen. No modal chrome.

```
┌─────────────────────────────────────────────────────────┐
│                                                         │
│             Welcome to WUPHF                            │
│             Pick the AI runtime to power your office.   │
│                                                         │
│   ┌─────────────────┐  ┌─────────────────┐              │
│   │  Claude Code    │  │  Codex          │   ← cards    │
│   │  ✓ Detected     │  │  ✓ Detected     │              │
│   │  v1.2.3         │  │  v0.8.1         │              │
│   └─────────────────┘  └─────────────────┘              │
│                                                         │
│   ┌─────────────────┐                                   │
│   │  Opencode       │     ┌──────────────────────────┐  │
│   │  Not installed  │     │  I'll add one later  →   │  │
│   │  Install →      │     └──────────────────────────┘  │
│   └─────────────────┘     [secondary, smaller]          │
│                                                         │
└─────────────────────────────────────────────────────────┘
```

- Detected runtimes are tappable; install links open in browser; user has to
  click the runtime to confirm (no auto-pick) so they know what they chose.
- "I'll add one later" is real: lets the user enter the office in a sandboxed
  read-only mode. CEO greets them but cannot dispatch work until they
  configure a runtime. We need this for evaluators.
- **No identity step here.** Name + git handle is asked by CEO in chat if and
  when we need it (we mostly do not — git already has it from the host).
- One short helper line under the cards: *"You can change this any time in
  Settings → Runtimes."*

This is also the screen where we run the existing `Step3bAnalysis`
prereqs scan, but silently — its only output here is enabling/disabling
the runtime cards.

---

## Surface 2 — Office entry: empty shell + CEO DM open

The user lands directly into the real `Shell` component. No transition
animation across the runtime → office boundary; they already trust they are
moving forward.

```
┌──┬──────────┬──────────────────────────────────────────┬─────┐
│Wp│ Channels │  DM — CEO                                │     │
│  │  (none)  │                                          │     │
│  │          │  CEO  ●                                  │     │
│  │ Issues   │  Welcome. I'm the CEO of your office.   │     │
│  │  (none)  │  What are you working on right now?     │     │
│  │          │                                          │     │
│  │ Agents   │  ┌────────────────────────────────────┐  │     │
│  │ • CEO ✓  │  │ Type your answer or pick a starter │  │     │
│  │          │  │ • Bookkeeping  • Content ops       │  │     │
│  │          │  │ • Engineering team • Start scratch │  │     │
│  │          │  └────────────────────────────────────┘  │     │
│  │          │                                          │     │
└──┴──────────┴──────────────────────────────────────────┴─────┘
 56px   240px            flex-grow                       hidden
```

Behavior rules for this frame:
- `Channels` and `Issues` sidebar groups render as empty states with a
  faint "CEO will set these up" placeholder, not hidden. The user must see
  what the surfaces ARE so they understand what is forming.
- `Agents` shows only CEO with a status pill *Ready*.
- CommandPalette, Search, AgentPanel, ThreadPanel are mounted but inert
  (no entry points exposed until first issue exists).
- Workspace rail shows one workspace tile with an unnamed placeholder.
- StatusBar: small *"Office initializing — chat with CEO to set it up."*

The composer below the CEO DM has the standard input AND a row of "starter
intent" chips for users who do not want to type. Picking a chip is the same
as typing the equivalent prompt — the chat continues with CEO's blueprint
inference message.

---

## The deterministic / LLM split

The CEO chat looks like one continuous conversation, but only a subset of
its turns make LLM calls. This is deliberate:

- **Deterministic turns are instant, free, and cannot fail.** They power
  the entire setup phase (workspace name, description, priority, website
  URL + scan, owner info, blueprint pick, team trim, wiki seeding). The
  CEO renders them as natural speech, but the implementation is templates
  with slotted values + standard form payloads. Zero LLM tokens.
- **LLM turns start only after the deterministic foundation is built.**
  They begin when the user describes a first issue. At that point, agents
  have wiki context (from the deterministic scrape and blueprint
  scaffold) and a known blueprint or none, so the LLM has grounded input.

The user never sees this boundary. Both kinds of turns render identically
as CEO speech. Suggestion cards, chip rows, and checklists are all just
CEO messages with an interactive payload.

### Deterministic phase — no LLM tokens

The form-fills below are exactly the ones the current wizard collects
(`Step3Identity` + `Step2Templates` + `Step3bAnalysis` + `Step4Team`).
We are **not removing** these collections — we are re-rendering them as
CEO chat messages with interactive payloads.

| Step | Source | Side effect |
|---|---|---|
| Workspace name | Form field in CEO message | Workspace rail label updates (240ms cross-fade) |
| Short description | Form field | Stored in office config |
| Top priority (optional) | Form field | Stored in office config |
| Website URL (optional) | Form field | Fires `POST /onboarding/scan` (existing endpoint); chat shows a scan-progress chip until response lands |
| Owner name + role (optional) | Form fields | Stored for git attribution + CEO addressing the user by name |
| Blueprint pick | Chip row in CEO message | If a blueprint is selected: its channels + wiki scaffold are materialized and the team picker renders next. If **Start from scratch**: sidebar stays minimal (just `#general` + minimal wiki scaffold) and team picker is skipped. |
| Team trim (blueprint path only) | Checklist in CEO message | Selected agents are added to sidebar with stagger animation; each posts a one-line intro |
| Wiki seed (background) | Response of `POST /onboarding/scan` + blueprint scaffold | Company Facts page is updated with scraped data when the scrape returns; blueprint scaffold pages are materialized at blueprint-pick time |

All templated. Even the "intro line" each agent posts on join is a template
with one slotted variable.

### LLM phase — begins when user describes first issue

| Step | Why it's LLM |
|---|---|
| Issue draft | Spec must reflect the user's free-text description |
| Agent suggestion (scratch path only) | No blueprint roster to draw from; CEO must reason about which agents fit this issue |
| Agent comments on draft | Agents must read the draft and respond in role |
| Sub-issue decomposition (when user clicks *Break this up*) | Requires reasoning about scope |
| Execution after Approve | The actual work |

### Routing rule

CEO has a single state machine. Each phase has a `mode` of either
`deterministic` or `llm`. Before the `draft` phase, every CEO message is
rendered from a template; from `draft` onward, every CEO message goes
through the model. This makes the trust calibration honest: by the time
the model starts speaking, the user has already seen the office form
around them and understands what's about to happen.

The boundary is a single backend flag; the frontend renders both modes
through the same message envelope.

---

## Surface 3 — The conversation as office-builder

Each user reply causes 0–N office mutations, paired with a short CEO
acknowledgement in chat. The CEO never silently changes the office; every
change is announced AND visible. This is the heart of the trust calibration.

Both deterministic and LLM turns use the same chat-message envelope and
the same suggestion-card mechanic. The differences are: deterministic
turns are templated and instant, LLM turns are model-driven and stream
in with a typing indicator. From the user's POV, CEO is one consistent
voice.

### Turn-by-turn template

```
User:  <answer>
       ───────────────────────────────────────────
CEO:   <acknowledgement + suggestion>
       ┌─────────────────────────────────────┐
       │  Suggested:                         │
       │  • Add #billing channel             │
       │  • Add PM agent (rationale)         │
       │                                     │
       │  [Approve all]  [Pick which]  [No]  │
       └─────────────────────────────────────┘
```

The suggestion card is a structured message in the DM (not a modal). When
the user clicks **Approve all** or picks individually:
1. The card collapses into a one-line confirmation in the timeline:
   *"Added #billing and PM. ✓"*
2. The sidebar mutates with the chosen items, **one item at a time**, 250ms
   apart. Each item: opacity 0 → 1, translateX -8px → 0, 200ms ease-out.
   Items use `prefers-reduced-motion` to fall back to instant.
3. Each newly added agent posts a one-line "intro" message in the DM:
   *"PM here — I help with scope and acceptance criteria. Pinging when
   useful."* Agents do this once, on join. Never spammy.
4. CEO's next message arrives ~600ms after the last item finishes animating
   — slow enough that the user can read each arrival.

### Conversation phases (CEO is stateful)

| Phase | Mode | Goal | What happens |
|---|---|---|---|
| `greet` | Deterministic | Establish presence | Templated CEO greeting + one open question. |
| `identity` | Deterministic | Collect office name, description, priority, owner info | Form-field messages inline in chat (same payloads as today's `Step3Identity`). |
| `scan` | Deterministic (async) | Optional website scrape → wiki facts | URL form field + chip showing scrape status. Continues in background while the user answers later questions. Reuses `POST /onboarding/scan`. |
| `blueprint` | Deterministic | Pick a starter template or scratch | Chip row inside a CEO message (same options as today's `Step2Templates`). No LLM inference of blueprint from text. |
| `team` | Deterministic (blueprint) / **Skipped** (scratch) | Trim the blueprint's agent roster | Checklist inside a CEO message (same payload as today's `Step4Team`). Scratch path skips this phase. |
| `seed` | Deterministic | Materialize channels + wiki scaffold; finish website scrape | Sidebar mutations + intro lines. No LLM. |
| `bridge` | Deterministic | Offer to start an issue | Templated message. User can also say "look around first" — exits onboarding with first issue deferred. |
| `draft` | **LLM begins** | Co-author the first issue. Scratch path: CEO also proposes agents inline. | First LLM call. Moves user to Surface 4. |
| `approve` | Deterministic | Human approves the issue | Status flips, suggestion card collapses. |
| `kickoff` | **LLM** | Suggest execution lineup, dispatch | Suggestion card. First post-approval LLM call. |

The phase is a property of the onboarding conversation, persisted in the
existing onboarding state (`internal/onboarding/state.go`). On resume the
CEO knows where it left off and picks up the next CEO message at that phase.

### What is cut from CEO's conversation surface
- No multi-select grids. Every choice is either a chip row or a structured
  suggestion card with explicit approve/decline.
- No progress dots. Progress is the office filling in — visible state, not
  abstract indicator.
- No "Next" button. Conversation advances when CEO speaks.
- No skip-to-end. If the user is bored they pick a starter chip and CEO
  fast-paths the rest.

---

## Surface 4 — The Issue document (replaces "first task")

Issues are the morphed version of the existing `task` primitive. Same
underlying storage, upgraded surface. Sub-issues are nested issues with a
parent reference. **Approved issues** also publish a read-only mirror under
the wiki at `Issues/` for future reference, with no edit affordance there.

### Anatomy

```
┌──────────────────────────────────────────────────────────────┐
│  Issues / #1 Stripe webhook handler          [Drafting ▼]    │
│  ──────────────────────────────────────────────────────────  │
│  Assignees:  CEO (drafting)         + Add                    │
│  Sub-issues: none                   + Break this up          │
│                                                              │
│  ## Goal                                                     │
│  Receive Stripe webhook events (charge.succeeded,            │
│  charge.failed) and update local subscription state.         │
│                                                              │
│  ## Context                                                  │
│  Subscriptions are currently stored in… [editable]           │
│                                                              │
│  ## Approach                                                 │
│  ...                                                         │
│                                                              │
│  ## Acceptance criteria                                      │
│  - Webhook endpoint at POST /stripe/webhook                  │
│  - HMAC-SHA256 signature verified per Stripe docs           │
│  - charge.succeeded marks sub as active                      │
│  - charge.failed marks sub as past_due, sends email          │
│                                                              │
│  ─── Comments ───────────────────────────────────────────    │
│  CEO  10:03  Drafted spec based on our chat. Engineer can    │
│              you sanity-check Approach?                      │
│  Eng  10:04  Approach looks good but I'd add idempotency     │
│              via the Stripe Event ID. Want me to spec it?   │
│  You  10:05  Yes, please add to acceptance criteria.         │
│  CEO  10:05  Updated. Ready for your review.                 │
│                                                              │
│  ──────────────────────────────────────────────────────────  │
│  [ Approve & Start ]  [ Save draft ]  [ Discard ]            │
└──────────────────────────────────────────────────────────────┘
```

Status pill drives the surface heavily:

| Status | Editable | Visible buttons | Activity feed |
|---|---|---|---|
| `Drafting` | Yes (all sections) | Approve & Start, Save draft, Discard | Comments only |
| `Approved` | No (except acceptance) | Reopen for edits | Comments + dispatch event |
| `In Progress` | No (except acceptance) | Pause, Cancel | Comments + agent activity events |
| `Review` | No | Approve close, Request changes | Comments + agent activity events |
| `Done` | No | Reopen | Frozen |

The "Activity feed" mixes structured agent events (tool calls, file changes,
test results) inline with comments. Same surface, different message types.
Agent events use a quieter style — small avatar, monospace metadata,
collapsible payload — so the spec stays readable.

### Where the Issue lives

- **Sidebar:** `Issues` section between `Channels` and `Agents`. Shows up to
  20 most-recent / open issues, with a search-and-filter affordance via the
  command palette.
- **Per-workspace surface:** `/issues` route shows a Linear-style list with
  status filters, assignee filters, and the option to surface only issues
  the user has touched (drafted, commented on, or approved).
- **Cross-link from chat:** typing `#142` in any channel composer
  auto-completes to an issue link with hover preview (title + status).
- **Wiki mirror:** on first approval, the issue's spec sections are copied
  read-only into `wiki/Issues/<slug>.md`. Comments and activity do not
  mirror — the wiki is the durable spec, the issue is the live record.

### Sub-issues

- *Break this up* on the parent issue puts CEO in `decompose` mode: CEO
  drafts 2–N sub-issues with titles + one-line goals, posts as a single
  suggestion card.
- User approves the breakdown, sub-issues are created in `Drafting` status,
  parent issue's status freezes at `Approved` (or `Drafting` if user wants
  to plan the whole tree before approving any).
- Each sub-issue follows the same draft → approve → execute lifecycle.
- Parent issue auto-closes when all sub-issues hit `Done`, OR user closes
  manually.

---

## Stage transitions and the approval gate

The single most important UX rule:

> **No agent does any execution work for an issue until the human clicks
> Approve & Start on that issue.**

This is the rule the current product violates and the reason new users feel
the system is "too quick." Concretely:

- During `Drafting`, agents pulled in by CEO can **only** post comments on
  the issue document. They cannot call tools, run code, write files, or
  send messages anywhere else. The broker enforces this server-side by
  refusing to dispatch tool calls for an issue not in `Approved` /
  `In Progress`.
- On `Approve & Start`:
  1. Issue status flips to `Approved` for ~300ms with a soft confirmation
     toast (*"Approved. Dispatching CEO to coordinate execution."*).
  2. CEO posts an execution suggestion card: *"For execution, I'll route
     this to Engineer with Designer as reviewer. Sound good?"*
  3. User approves the lineup; status flips to `In Progress`; first agent
     activity event appears within seconds.

This means the same Engineer agent can comment in `Drafting` and execute
in `In Progress` — they are the same agent, the gate is the issue's status.

---

## Motion specification

Goal: the office feels like it is **forming around the user**, not
**rendering through them**. Slow, deliberate, never bouncy.

### Library
Add `motion` (the renamed, slimmer fork of framer-motion, ~12 kB gzipped)
as a single dependency. Use it only for:
- Sidebar item enter / exit
- Issue surface enter
- Status pill morph

CSS transitions are sufficient for everything else (hover, focus, button
press, chat bubble fade-in). Do not introduce a second motion library.

### Durations and easings
| Element | From | To | Duration | Easing |
|---|---|---|---|---|
| Sidebar row enter | opacity 0, x -8px | opacity 1, x 0 | 220 ms | ease-out |
| Sidebar row stagger | — | — | 200 ms gap | — |
| Chat bubble enter | opacity 0, y 6px | opacity 1, y 0 | 180 ms | ease-out |
| Suggestion card collapse | full → 1 line | scale anchored top | 240 ms | ease-in-out |
| Issue open | opacity 0 + main translate 8px | normal | 320 ms | ease-out |
| Status pill morph | color A | color B + label change | 220 ms | linear |
| Workspace label update | text fade-cross | — | 240 ms | linear |

All durations halve and easings collapse to linear when
`prefers-reduced-motion: reduce` is true. No animation has a duration over
350 ms; the office must never feel slow.

### Anti-patterns (do not use)
- Spring physics on enter (bouncy).
- Cascading reveal on first paint of static UI (the wizard is gone — we do
  not need a tutorial reveal).
- Animated gradients, glow effects, or shimmers.
- Sound effects.
- Confetti or celebration animations on Approve & Start. The serious tone is
  the trust-building lever.
- Skeleton placeholders that then fade into real content. Either the row
  exists or it doesn't.

### Pacing rule
Between any two CEO turns, the user must have at least **400 ms of
quiet** after the last animation completes. This is what makes the office
feel deliberate. Front-load this; do not let it slip in implementation.

---

## State and resumption

The onboarding conversation persists into the existing
`internal/onboarding/state.go` JSON (the same place the wizard draft lives
today). New fields:

```go
// Schema v2 (existing v1 fields preserved; new fields added).
// Migration on read: v1 onboarded.json → load fields → bump version to 2.
type State struct {
    // v1 existing fields
    Version            int             `json:"version"`            // bump to 2
    CompletedAt        string          `json:"completed_at,omitempty"` // set at end of `bridge` phase
    CompanyName        string          `json:"company_name,omitempty"`
    CompletedSteps     []string        `json:"completed_steps,omitempty"` // legacy wizard step IDs
    ChecklistDismissed bool            `json:"checklist_dismissed"`
    Partial            *PartialProgress `json:"partial,omitempty"`  // legacy wizard draft

    // v2 chat-mode additions
    Phase                string      `json:"phase,omitempty"`                  // greet | identity | scan | blueprint | team | seed | bridge | draft | approve | kickoff
    CEODMChannelID       string      `json:"ceo_dm_channel_id,omitempty"`      // pointer into b.messages; transcript NOT stored here
    PendingSuggestion    *Suggestion `json:"pending_suggestion,omitempty"`     // idempotent re-emit on resume
    FormAnswers          FormAnswers `json:"form_answers,omitempty"`           // staged deterministic answers; committed at `seed`
    FirstIssueID         string      `json:"first_issue_id,omitempty"`
    FirstIssueApprovedAt string      `json:"first_issue_approved_at,omitempty"` // activation depth, distinct from CompletedAt
}

type FormAnswers struct {
    CompanyName string   `json:"company_name,omitempty"`
    Description string   `json:"description,omitempty"`
    Priority    string   `json:"priority,omitempty"`
    WebsiteURL  string   `json:"website_url,omitempty"`
    OwnerName   string   `json:"owner_name,omitempty"`
    OwnerRole   string   `json:"owner_role,omitempty"`
    BlueprintID string   `json:"blueprint_id,omitempty"`        // empty = scratch
    PickedAgents []string `json:"picked_agents,omitempty"`      // blueprint path team-trim result
    ScanComplete bool    `json:"scan_complete,omitempty"`
}

type Suggestion struct {
    ID      string          `json:"id"`       // stable per (phase, options-hash) for idempotent re-emit
    Phase   string          `json:"phase"`
    Kind    string          `json:"kind"`     // ceo_form_field | ceo_chip_row | ceo_checklist | ceo_team_trim | ceo_scan_chip
    Payload json.RawMessage `json:"payload"`
}

// Activated() distinguishes onboarded-but-no-first-issue users (Marcus path)
// from fully-activated users (Sam, Priya after Approve & Start).
func (s *State) Activated() bool {
    return s.Onboarded() && s.FirstIssueApprovedAt != ""
}
```

Resumption: if the user closes the tab during `populate`, the next open
shows the office in its current state with the unresolved suggestion card
still visible at the bottom of the DM. CEO does not re-greet; the
conversation continues. The existing `ResumeBanner` infra is repurposed for
the rare case where the user has been gone > 24 hours.

`CompletedAt` is set when the first issue hits `Approved`. After that, the
office is in normal mode: command palette is unlocked, `+ Add channel`,
`+ New issue` affordances appear, runtime strip becomes visible.

---

## What this design does NOT do

- **Does not change the existing review queue or task-as-state machine.**
  Issues are tasks rebranded with a richer surface. The broker's existing
  task lifecycle remains the substrate; the issue document is a new
  presentational layer plus the comments timeline.
- **Does not introduce new agent roles.** The existing CEO + role-based
  agents are sufficient. The two-stage behavior is an issue-status
  property, not an agent property.
- **Does not deprecate channels.** Channels remain the home for
  cross-issue conversation and ambient agent chatter. Issues are where
  scoped work lives.
- **Does not change the marketing site or its DESIGN.md.** This is a
  separate visual layer; the app keeps its modern Slack-like aesthetic.
- **Does not block on motion library adoption.** If `motion` adoption is
  blocked for any reason, ship with CSS transitions only; the design still
  works, just with less polished sidebar enter/exit.

---

## Implementation phases

This is a substantial change. Phase it to keep main green and to allow
incremental dogfooding.

### Phase 1 — Pre-office screen + empty-shell entry
- Build the single-screen provider picker (replace `Wizard.tsx` invocation
  in `App.tsx`).
- Allow entering the office immediately after provider pick.
- Preserve the existing wizard behind a feature flag for rollback. Default
  the flag ON for `npx wuphf` dogfood, OFF in prod.

**Acceptance:** New users see provider picker → office with empty shell +
CEO DM that says hi. No regressions for users who already onboarded.

### Phase 2 — Deterministic CEO conversation
- Wire the phase state machine in `internal/team/broker_onboarding.go`:
  `greet → identity → scan → blueprint → team (or skip) → seed → bridge`.
  All deterministic — no model calls.
- Build the form-field, chip-row, and checklist message types in the
  existing message envelope. Reuse the payloads of today's
  `Step3Identity`, `Step2Templates`, `Step4Team`.
- Wire `POST /onboarding/scan` to fire from the `scan` phase, with the
  scan-progress chip in chat and async wiki facts update on completion.
- Implement sidebar mutation animations (channels, agents, workspace
  label cross-fade) on `seed`.
- End the conversation at `bridge` with "Click `+ New issue` when ready"
  or *"Look around first"* (Marcus path). No LLM drafting yet.

**Acceptance:** All three ICP tutorial examples reach `bridge` end-to-end
with motion working and zero LLM calls made. Verify by asserting the
provider mock is never invoked during the deterministic phase.

### Phase 3 — Issue document surface
- New `/issues` route + sidebar group.
- Migrate existing tasks to render as Issues (back-compat read).
- Issue document layout, status pill, comments timeline.
- Wiki mirror on approval.

**Acceptance:** Existing tasks display correctly as Issues; users can open
an issue, see its current state, and the surface is read-only for now.

### Phase 4 — Draft + approve + execute (v1 scope)
- CEO can draft an issue inside Surface 4. The issue is a lifecycle task
  in the new `LifecycleStateDrafting` state.
- Agents can comment on issues in `Drafting` (broker server-side gates
  every dispatch entry point).
- Comments timeline interleaves humans + agents using the existing
  comment infrastructure (`broker_inbox_handler.go:229`).
- **Approve & Start** maps to the existing `approve` lifecycle action,
  transitions Drafting → Running, dispatches CEO's execution lineup
  (suggestion card mirroring the populate pattern).
- Add `broker_lifecycle_dispatch_test.go` parametric test as a gate.

**Acceptance:**
1. All three ICP tutorial examples complete end-to-end with approval gate
   working. No dispatch happens before Approve & Start.
2. `TestBrokerRefusesDispatchForNonExecutableLifecycle` passes — parametric
   over every dispatch entry point with negative cases (Drafting, Intake,
   Review, ChangesRequested) and positive cases (Running, Approved).
3. Comment path verified allowed in all states.
4. Sam-path E2E test asserts no LLM provider invocation for execution
   tools until Approve & Start.
5. **CEO voice regression test** (added per design review TODO-D1): the
   draft-phase and kickoff-phase LLM prompts are evaluated against a
   hand-written 5-example regression corpus. Each example expects the
   CEO response to: (a) start without a greeting/preamble, (b) avoid the
   string "I'm your" / "I am your" / "Welcome", (c) stay under ~40 words
   in the acknowledgement message, (d) match the declarative tone of
   the deterministic templates. Test lives in
   `internal/team/broker_ceo_voice_test.go` and runs against a mocked
   provider returning fixture-ish responses for shape, plus a marker
   test against real provider gated to nightly. Failing voice regression
   blocks the Phase 4 acceptance gate.

### Phase 6 — Sub-issues + wiki mirror (deferred from Phase 4)
- *Break this up* affordance on the parent issue: CEO drafts 2-N
  sub-issues with titles + one-line goals (LLM call).
- Sub-issue creation, parent-child link, breadcrumb in issue header.
- Each sub-issue has its own approval gate. Parent freezes at Approved
  while sub-issues are drafted and approved individually.
- Cascading parent close when all sub-issues hit `Done`.
- Wiki mirror: on first `approve` action, snapshot issue spec sections
  into `wiki/Issues/<slug>.md` read-only. Comments + activity do not
  mirror.
- Sub-issue dispatch order: user declares dependencies in the issue
  spec; broker enforces them (no autonomous DAG inference in v1).

**Acceptance:** Parent + sub-issue lifecycle works end-to-end; wiki mirror
exists and is regenerated correctly on subsequent edits-then-approvals.

### Phase 5 — Polish and cleanups
- Remove the old wizard from the codebase (delete `Wizard.tsx`, all step
  files, draft sync, `synthesizeBlueprintFromState` dead path, etc.).
- Move Nex signup to Settings → Integrations.
- Migrate any existing onboarding telemetry to the new phase names.
- Update `docs/tutorials/` for the new flow.
- **Promote `InterviewBar`'s kind-dispatcher + sanitization into a shared
  `StructuredMessageCard` module** under `web/src/components/messages/cards/`.
  Both interview kinds (`approval`, `enhance_skill_proposal`) and CEO kinds
  (`ceo_form_field`, `ceo_chip_row`, `ceo_checklist`, `ceo_team_trim`,
  `ceo_scan_chip`) become consumers. Single source of truth for the PR #684
  sanitization audit point.

**Acceptance:**
1. No references to `Wizard.tsx` or the 8-step wizard remain.
2. Tutorials match shipped behavior.
3. `StructuredMessageCard` module exists, both interview cards and CEO
   cards consume it, sanitization regression tests live alongside the
   module (one audit point, not two).

---

## Open questions — resolved by /plan-eng-review (2026-05-17)

1. **Broker enforcement of the approval gate.** RESOLVED → server-side
   enforcement. See "Eng review decisions" section above.
2. **Sub-issue dispatch model.** DEFERRED to Phase 6. User declares
   dependencies in the issue doc; broker enforces them. No autonomous
   DAG.
3. **Wiki mirror format.** DEFERRED to Phase 6. Plan: as-is markdown
   snapshot of issue spec sections.
4. **Comments on already-approved issues.** RESOLVED → comments stay
   open in all states (per the lifecycle comment infrastructure
   already in place). Agents read new comments as guidance during
   execution.
5. **Old draft migration.** RESOLVED → finish in-flight wizard drafts
   through the old wizard (feature flag scoped to "started after
   deploy"); delete the wizard entirely in Phase 5.

## Genuinely open (for implementer judgment)

These are not gates, just places the implementer will make small choices:

1. **`LifecycleStateDrafting` vs reuse `LifecycleStateIntake`.** Adding
   a new state is more explicit; reusing Intake with a "no dispatch
   from Intake" guard is fewer moving parts. Implementer's call when
   wiring the lifecycle transitions.
2. **Sandbox mode for the evaluator path.** Pre-office picker "I'll add
   one later" enters a runtime-less office. Deterministic chat works
   fine (no LLM needed). `bridge` "look around first" works fine. What
   if the user then clicks `+ New issue` and tries to draft? Need a
   gentle blocker: "Hook up a runtime in Settings before starting an
   issue." Wording + placement is implementer judgment.
3. **Pacing rule (400ms quiet) location.** Recommended: frontend
   orchestrator throttles deterministic message arrival. Implementer
   can also do it backend-side via stream-throttling. Either works.

---

## NOT in scope (deferred, explicit)

Items considered and deferred, with rationale. Each should be picked up
as a follow-up issue, not dropped silently.

- **Sub-issues** — `Break this up`, parent-child semantics, dispatch order,
  cascading parent close. → Phase 6. Reason: non-trivial dispatch logic
  that doesn't move the trust-calibration needle for v1.
- **Wiki mirror on approval** — read-only snapshot of approved issues under
  `wiki/Issues/<slug>.md`. → Phase 6. Reason: archival convenience, not
  load-bearing for the design goal.
- **Nex signup in onboarding** — Phase 5 moves to Settings → Integrations.
  Reason: optional integration, blocks nothing for first issue.
- **Autonomous sub-issue DAG inference** — when v6 ships sub-issues, the
  broker enforces user-declared deps, no AI inference. Reason: blast
  radius — wrong inference re-introduces the trust problem we just fixed.
- **CEO transcript GC** — what to do with the onboarding DM transcript
  after months of dust. → Wiki lifecycle absorbs this naturally if/when
  we wire the DM into the existing wiki staleness signals.
- **Pacing rule as a backend stream throttle** — implementer judgment
  whether the 400ms quiet rule lives in the frontend orchestrator or
  the backend stream. Either works; not architectural.

## What already exists (reuse map)

Existing code that the plan deliberately reuses rather than rebuilds.
Adversarial code review should compare these claims against current main.

| Plan needs… | Existing surface | Reuse strategy |
|---|---|---|
| Provider runtime detection | `internal/onboarding/prereqs.go` + `Step3bAnalysis` (existing JSON, existing endpoint) | Use as-is. Drive the new single-screen picker from the same data. |
| Website scrape + wiki seed | `POST /onboarding/scan` (`internal/onboarding/handlers.go:75,679-760`) + blueprint `wiki_scaffold` materialization (`b.materializeBlueprintWiki`) | Use as-is. Wire scan completion into a `ceo_scan_chip` update message. |
| Atomic blueprint + team seed | `b.seedFromBlueprintLocked` + `b.materializeBlueprintWiki` (existing) | Use as-is at the `seed` phase boundary. No refactor. |
| Idempotent onboarding seed (crash recovery) | `b.onboardingCompleteFn` dedupe-by-`onboarding_origin` (`broker_onboarding.go`) | Use as-is. Inherits crash safety. |
| Task lifecycle state machine | `LifecycleState{Intake, Ready, Running, Review, ChangesRequested, Approved, …}` + transitions in `broker_lifecycle_*.go` + `lifecycleIndex` | Issue surface is a richer view onto this index. New state `LifecycleStateDrafting` (or reuse Intake) at the front. |
| Approve / request_changes / defer actions | `broker_inbox_handler.go:229` + `broker_inbox.go` + `broker_decision_packet.go` | `Approve & Start` button maps directly to `approve` action. |
| Comments on lifecycle entities | Same comment payload field on the existing decision action (`"comment": "<reviewer note>"`) | Reuse the comment infrastructure for the issue document's timeline. |
| Inbox / review UI surface | `web/src/components/review/{ReviewCard,ReviewColumn,ReviewDetail}.tsx` + the unified Inbox shipped via PR #885 | Issues list `/issues` route is a presentational sibling that filters/renders the same lifecycle index. |
| Chat surface (composer, message feed, SSE, scroll, autocomplete, typing) | `DMView`, `Composer`, `MessageFeed`, `MessageBubble`, `TypingIndicator` | Reuse via a thin `OnboardingDMRoute` wrapper that points `DMView` at `dm:ceo:onboarding`. |
| Interactive structured cards | `HumanInterviewOverlay` + `InterviewBar` with kind dispatch + `EXTERNAL ACTION` badge pattern | Extend with `ceo_form_field`, `ceo_chip_row`, `ceo_checklist`, `ceo_team_trim`, `ceo_scan_chip`. |
| Suggestion-card sanitization | `sanitizeContextValue` (Go-side, per PR #684 closing confused-deputy bypass) | Inherited automatically by extending the same payload path. Add regression test for new kinds. |
| Onboarding state persistence | `internal/onboarding/state.go` `Load`/`Save` (existing) | Extend schema to v2 with new fields. Migrate v1 → v2 on read. |
| Onboarding handler authentication + JSON wiring | `internal/onboarding/handlers.go` (existing `authMiddleware`, scan handler patterns) | New chat-phase handlers follow the same patterns. |

If any of these claims fail on grounding (e.g., the surface doesn't actually
do what we expect), the implementer must flag immediately — they are
load-bearing for the plan's scope estimate.

## Parallelization strategy

Phases 1 → 2 → 3 → 4 → 5 → 6 are largely sequential because each phase
builds on the surface the prior one introduced. The opportunity is
**within Phase 2**, which has three independent workstreams that can run
in parallel worktrees:

| Lane | Modules touched | Depends on |
|------|----------------|------------|
| A: Backend state machine | `internal/onboarding/`, `internal/team/broker_onboarding.go` (additive, not seed-path) | — |
| B: Frontend chat surface | `web/src/components/messages/InterviewBar.tsx`, new `web/src/components/onboarding/OnboardingDMRoute.tsx`, new `ceo_*` card components | — |
| C: Sidebar preview overlay | `web/src/components/layout/Sidebar.tsx`, new `web/src/components/onboarding/usePreviewOffice.ts` | — |

Lane A and Lane B/C share no files. Lanes B and C share the `web/`
directory but touch different components — minimal conflict risk if both
respect the file boundaries above. Recommend:

```
git worktree add ../onboarding-into-office-laneA -b feat/onboarding-laneA-backend
git worktree add ../onboarding-into-office-laneB -b feat/onboarding-laneB-cards
git worktree add ../onboarding-into-office-laneC -b feat/onboarding-laneC-sidebar
```

Lanes B and C should integrate first (both web/), then Lane A. The
spec's Phase 2 acceptance test (`TestOnboardingDeterministicPhasesNeverCallLLMProvider`)
requires all three to land before the gate passes.

Phases 3, 4, 6 each have less internal parallelism. Phase 5 cleanup is
naturally one developer, one PR.

## Failure modes

For each new codepath, the realistic production failure scenario and
whether the design accounts for it.

| Codepath | Failure mode | Test | Error handling | User-visible? |
|---|---|---|---|---|
| Approval gate (broker dispatch guard) | Caller bypasses guard from a new dispatch entry point added later | `TestBrokerRefusesDispatchForNonExecutableLifecycle` is parametric — adding a new entry point requires extending the test (catches drift) | `ErrIssueNotApproved` returned; caller's responsibility to surface | Yes — frontend shows "issue not approved" if it slips |
| `POST /onboarding/scan` async | Website unreachable / times out | Existing handler test covers happy + bad-body; need additional timeout path test | Returns error to chat; chip shows "couldn't read site" | Yes — clear chat message |
| Sanitization on new `ceo_*` kinds | Agent-controlled string with HTML/script injection | `broker_onboarding_sanitize_test.go` parametric over all new kinds | Sanitized to safe text before write | No — silent escape |
| Atomic seed at `seed` phase | Mid-seed crash (e.g., wiki materialization fails after channels seeded) | Existing `onboarding_origin` dedupe guard handles crash recovery | Resume re-runs idempotent seed; the broker dedupes | No — invisible recovery |
| Phase resumption with `PendingSuggestion` | Suggestion ID collides across phases | ID is stable per `(phase, options-hash)` — collision implies same payload, dedupe is correct | Idempotent re-emit; frontend dedupes by ID | No |
| Marcus path (no first issue) | User completes `bridge` with "look around first," office stays empty forever | E2E test asserts `Onboarded()` true + `+ New issue` works on demand | No error path; the design is "this is valid" | No |
| `LifecycleStateDrafting` introduction | Existing tasks that should be in another state default to Drafting on migration | Lifecycle migration test (already exists per `broker_lifecycle_migration_test.go`) — extend with new state | Migration assigns Drafting only to brand-new tasks; existing tasks keep their state | No |

**Critical gap (any failure mode with no test AND no error handling AND
silent failure):** none identified — all gaps in the test diagram have at
least one of test, error handling, or user-visible message.

## Decisions log

| Date | Decision | Rationale |
|---|---|---|
| 2026-05-17 | Modern app shell with soft motion, not pixel art | Pixel art is the marketing site's job. The app's job is to look like a tool the user trusts. Different surfaces, different aesthetics. |
| 2026-05-17 | DM with CEO inside the empty shell, not a full-bleed onboarding canvas | The product *is* the office. The user should see it from frame 1. The empty shell calibrates expectations and the live populate animation IS the value prop. |
| 2026-05-17 | Tasks become Issues with their own surface, not threads or wiki pages | Issues need title + status + assignees + sub-issues + comments + approval gate. Wiki pages cannot host a status field cleanly; channel threads cannot have sub-threads at the level we need. Linear-style works. |
| 2026-05-17 | One agent cast across two stages, status-driven | Doubling the agent count to add "planners" doubles cognitive load with no real upside. The same engineer should plan and build, like in a real team. The stage gate is the issue's status, not the agent. |
| 2026-05-17 | Server-side enforcement of the no-execution-before-approval rule (recommended, open) | Trust is the design's core. Frontend-only enforcement is a footgun. |
| 2026-05-17 | `motion` library, not framer-motion or pure CSS-only | framer-motion is too heavy; pure CSS misses the staggered sidebar enter. Slim motion lib hits both. |
| 2026-05-17 | Deterministic-first conversation; LLM only after first issue description | Setup is form-fillable. Rendering it as templated CEO speech keeps it free, fast, and unfailable while preserving the natural-chat feel. LLM kicks in once the user has decided what to work on. |
| 2026-05-17 | Blueprint picker is deterministic chips, not LLM-inferred from description | LLM blueprint inference adds a failure mode (wrong blueprint) for no real win. The user picks explicitly. Same as today's `Step2Templates`, rendered as a CEO message. |
| 2026-05-17 | Team picker renders only on blueprint path; scratch path defers team to issue drafting | A blueprint comes with a team — that's a deterministic answer. Scratch has no team to offer up-front; CEO has to reason from the task, which is the right LLM moment. |
| 2026-05-17 | Website scan + wiki seed are reused from existing onboarding | `POST /onboarding/scan` and blueprint `wiki_scaffold` already exist. We don't redesign them — we re-render their prompts and results as CEO chat. |
| 2026-05-17 (eng review) | Server-side approval gate enforced in broker | The trust calibration is load-bearing. Frontend-only gating leaks through every alternate dispatch entry point (PamDispatcher, headless codex, self-heal, agent-to-agent). Server-side is the only honest answer. |
| 2026-05-17 (eng review) | Issue surface reuses `LifecycleState*` instead of new primitive | Lifecycle apparatus already covers status, comments, approve/request_changes, and the Inbox PR-style loop (PR #885). New `LifecycleStateDrafting` is the only addition. Cuts ~40% off Phase 3. |
| 2026-05-17 (eng review) | CEO transcript lives in `b.messages`, not in onboarding state | Reuses SSE + persistence + replay + search. Onboarding state stays small and focused. Eliminates ~200 lines of parallel persistence code. |
| 2026-05-17 (eng review) | Staged FormAnswers + atomic seed at `seed` phase | Avoids refactoring `seedFromBlueprintLocked` into incremental mutations. Crash mid-flow leaves an empty office, not a half-formed one. Existing `onboarding_origin` dedupe guard inherits. |
| 2026-05-17 (eng review) | `CompletedAt` set at end of `bridge` phase regardless of first issue | Marcus's "look around first" is a valid onboarded state. Activation depth (first issue approved) is tracked separately in `FirstIssueApprovedAt`. |
| 2026-05-17 (eng review) | Explicit phase cursor + `PendingSuggestion` for resumption | State machines should not depend on log replay. Cursor + pending payload = two fields, deterministic, testable. Transcript is for users to read. |
| 2026-05-17 (eng review) | Scratch path uses `seedMinimalScratchLocked`, not `synthesizeBlueprintFromState` | Don't seed a fake team for a user who said "no team yet." Empty-but-real is honest; synthesized-team contradicts the trust-calibration goal we just locked. |
| 2026-05-17 (eng review) | Frontend reuses `DMView` for CEO chat | Inherits Composer, MessageFeed, SSE, scroll, optimistic posts, autocomplete, typing indicator. The CEO DM IS a DM. |
| 2026-05-17 (eng review) | Suggestion cards extend `InterviewBar` kind dispatcher | Inherits PR #684 `sanitizeContextValue` sanitization automatically. Parallel card system would have re-introduced the confused-deputy bypass. |
| 2026-05-17 (eng review) | Sub-issues + wiki mirror deferred to Phase 6 | Phase 4 v1 stays focused on the trust-calibration goal (approval gate). Cuts ~3 dev days from v1; both features are real but neither is load-bearing for v1. |
| 2026-05-17 (eng review) | Sanitization + approval gate tests as explicit Phase 2/4 acceptance gates | PR #684 evidence: load-bearing safety tests get cherry-picked into "later" follow-ups unless they're required gates. Making them gates is cheap insurance. |
| 2026-05-17 (eng review) | State persisted on phase transition + form-field commit, not per-keystroke | Avoids per-keystroke disk I/O; intra-field typing client-side until submit. ~10-12 writes per full onboarding. |
| 2026-05-17 (eng review) | `StructuredMessageCard` consolidation lands in Phase 5 | Both `InterviewBar` kinds and CEO kinds consume the same kind-dispatch + sanitization module. Single audit point for the PR #684 class of bugs. |

---

## GSTACK REVIEW REPORT

| Review | Trigger | Why | Runs | Status | Findings |
|--------|---------|-----|------|--------|----------|
| CEO Review | `/plan-ceo-review` | Scope & strategy | 0 | — | not run |
| Codex Review | `/codex review` | Independent 2nd opinion | 0 | — | not run |
| Eng Review | `/plan-eng-review` | Architecture & tests (required) | 1 | CLEAR (PLAN) | 12 issues raised, 12 resolved, 0 unresolved, 0 critical gaps |
| Design Review | `/plan-design-review` | UI/UX gaps | 1 | CLEAR | score: 6/10 → 9/10, 8 decisions made, 0 unresolved |
| DX Review | `/plan-devex-review` | Developer experience gaps | 0 | — | not run |

- **UNRESOLVED:** 0
- **CROSS-MODEL:** outside voices skipped at user's request (well-grounded plan in both reviews)
- **VERDICT:** ENG + DESIGN CLEARED — plan is ready to implement. Phase 1 (provider picker + empty-shell entry) is the smallest first slice with a feature flag for rollback.
