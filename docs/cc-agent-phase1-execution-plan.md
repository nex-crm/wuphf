# CC-agent Lessons: Phase 1 Execution Plan

## Purpose

This is the implementation-ready plan for Phase 1 of the CC-agent-inspired WUPHF improvements.

It is narrower than the full roadmap in:

- [cc-agent-implementation-roadmap.md](./cc-agent-implementation-roadmap.md)

This doc answers:

- what the exact first branches should be
- which real WUPHF files are likely to change
- what each branch should prove
- what termwright scenarios should validate

## Current WUPHF Code Map for Phase 1

Phase 1 mostly touches these existing codepaths:

### Channel and composer runtime

- [channel.go](../cmd/wuphf/channel.go)
- [channel_composer.go](../cmd/wuphf/channel_composer.go)
- [channel_test.go](../cmd/wuphf/channel_test.go)

What lives there today:

- main channel model state
- office vs `1:1` mode behavior
- slash command handling
- overlay updates
- input cursor behavior
- notice text
- most dialog-ish interaction decisions

Important constraint:

- there is not yet a separate `channel_doctor.go` or `channel_history.go`
- many Phase 1 changes will initially land in `channel.go` unless we extract small focused helpers first

### Autocomplete

- [autocomplete.go](../internal/tui/autocomplete.go)
- [autocomplete_test.go](../internal/tui/autocomplete_test.go)

What lives there today:

- slash matching
- selection cycling
- accept/dismiss behavior
- rendering of autocomplete rows

### Existing UAT coverage

- [office-channel-e2e.sh](../tests/uat/office-channel-e2e.sh)
- [one-on-one-channel-e2e.sh](../tests/uat/one-on-one-channel-e2e.sh)

What they already prove:

- office boot
- slash autocomplete smoke
- `1:1` boot and switching
- command blocking in `1:1`

## Recommended Phase 1 Branch Order

Recommended order:

1. `feat/context-aware-keybindings`
2. `feat/contextual-footer-hints`
3. `feat/draft-safe-history`
4. `feat/interaction-primitives`
5. `feat/runtime-change-confirmations`
6. `feat/safety-dialogs`

That order keeps risk low:

- branch 1 establishes the keyboard/focus contract the rest of the UI can rely on
- branch 2 improves guidance without changing semantics much
- branch 3 improves trust in composition
- branch 4 creates reusable primitives
- branch 5 starts using those primitives on real mode/runtime changes
- branch 6 applies the same rigor to disruptive actions

## Branch 1: `feat/context-aware-keybindings`

### Goal

Introduce an explicit interaction-context model for keyboard handling so overlays, autocomplete, help, transcript view, and future dialogs stop competing ad hoc.

### What Should Change

- define keyboard contexts for:
  - composer/chat
  - autocomplete
  - confirmation
  - transcript/thread view
  - help/doctor overlays
- centralize resolution of actions by active context
- make `Esc` semantics consistent:
  - dismiss overlay first
  - cancel work only when nothing higher-priority owns `Esc`
- keep the current bindings where possible; improve the model first

### Likely Files

- [channel.go](../cmd/wuphf/channel.go)
- [channel_composer.go](../cmd/wuphf/channel_composer.go)
- [autocomplete.go](../internal/tui/autocomplete.go)
- likely new helper:
  - `[internal/tui/keymap.go](../internal/tui/keymap.go)` or similar
- [channel_test.go](../cmd/wuphf/channel_test.go)
- [autocomplete_test.go](../internal/tui/autocomplete_test.go)

### Suggested Implementation Shape

- add a small interaction-context enum or state model
- avoid a giant generic framework; just make context ownership explicit
- keep command rendering/help wired to the same context model

### Termwright Scenarios

1. Slash autocomplete open:
   - `Esc` dismisses autocomplete, not the whole composer state
2. Thread or transcript view open:
   - navigation keys affect the open surface, not the hidden composer
3. Doctor/help overlay open:
   - `Esc` closes the overlay first
4. No overlay active:
   - existing cancel/back behavior still works

### Done Means

- keyboard behavior feels consistent across surfaces
- overlay/focus bugs become much less likely
## Branch 2: `feat/contextual-footer-hints`

### Goal

Make composer help and slash guidance more truthful, more contextual, and less static.

### What Should Change

- office vs `1:1` composer hints should differ more clearly
- hint copy should respond to current state:
  - plain compose
  - reply mode
  - interview pending
  - blocked/needs-you
  - command/autocomplete open
- autocomplete rows should become more teachable:
  - category or type label
  - clearer descriptions
  - better `1:1` vs office command expectations

### Likely Files

- [channel_composer.go](../cmd/wuphf/channel_composer.go)
- [channel.go](../cmd/wuphf/channel.go)
- [autocomplete.go](../internal/tui/autocomplete.go)
- [channel_test.go](../cmd/wuphf/channel_test.go)
- [autocomplete_test.go](../internal/tui/autocomplete_test.go)
- [office-channel-e2e.sh](../tests/uat/office-channel-e2e.sh)
- [one-on-one-channel-e2e.sh](../tests/uat/one-on-one-channel-e2e.sh)

### Suggested Implementation Shape

- keep the current renderer structure
- add a small helper for contextual composer hint text instead of embedding more inline string logic in `renderComposer`
- extend `tui.SlashCommand` if needed with a lightweight category field
- keep this branch mostly presentational

### Termwright Scenarios

1. Office boot:
   - verify composer hint reflects office context
2. `1:1` boot:
   - verify hint reflects direct mode
3. Reply mode:
   - verify reply-specific hint copy
4. Pending interview:
   - verify answer-specific hint copy
5. Slash autocomplete:
   - verify descriptions remain readable and mode-appropriate

### Done Means

- a user can tell what kind of input they are about to send without parsing surrounding chrome
- `1:1` no longer feels like the same composer with a different header

## Branch 3: `feat/draft-safe-history`

### Goal

Protect unfinished operator input during history and recall.

### What Should Change

- preserve draft before recall/navigation starts
- restore draft when returning from recall
- preserve cursor position
- preserve thread composer draft independently from main composer draft
- add a first safe recall primitive before building a richer search UI later

### Likely Files

- [channel.go](../cmd/wuphf/channel.go)
- new helper likely:
  - `[channel_history.go](../cmd/wuphf/channel_history.go)` or similar
- [channel_composer.go](../cmd/wuphf/channel_composer.go)
- [channel_test.go](../cmd/wuphf/channel_test.go)
- new UAT additions in:
  - [office-channel-e2e.sh](../tests/uat/office-channel-e2e.sh)
  - [one-on-one-channel-e2e.sh](../tests/uat/one-on-one-channel-e2e.sh)

### Suggested Implementation Shape

- add explicit main/thread draft snapshot state to `channelModel`
- implement one safe history step first:
  - previous submitted input recall
  - restore current draft on exit
- do not build full fuzzy history search in this branch
- optimize for trust before capability

### Termwright Scenarios

1. Main composer draft recall:
   - type draft
   - recall last sent prompt
   - return
   - verify original draft restored
2. Thread composer draft recall:
   - same as above inside thread panel
3. Cursor preservation:
   - draft with cursor in middle
   - recall / restore
   - verify cursor placement preserved
4. `1:1` composer:
   - verify same safety behavior

### Done Means

- history/recall cannot silently destroy unsent work
- main and thread composers behave independently and safely

## Branch 4: `feat/interaction-primitives`

### Goal

Create small reusable primitives for confirmation, continue, and pending states before more branches start needing them.

### What Should Change

- reusable double-press confirmation helper
- reusable byline / continue treatment
- reusable pending-state copy or style primitive
- avoid feature-by-feature ad hoc confirmation strings

### Likely Files

- new helpers likely in:
  - [internal/tui](../internal/tui)
  - or small focused files under [cmd/wuphf](../cmd/wuphf)
- [channel.go](../cmd/wuphf/channel.go)
- [channel_test.go](../cmd/wuphf/channel_test.go)

### Suggested Implementation Shape

- keep primitives tiny
- do not over-abstract
- only extract patterns already used in:
  - quit/exit
  - reset-like actions
  - picker cancel/confirm
  - any future runtime toggle confirmations

### Termwright Scenarios

1. Repeated-press confirmation:
   - verify pending state appears
   - verify timeout clears it
2. Continue prompt:
   - verify consistent phrasing/style
3. Cancel/confirm byline:
   - verify consistent shape across at least two surfaces

### Done Means

- confirmation behavior feels like one product, not several unrelated prompts

## Branch 5: `feat/runtime-change-confirmations`

### Goal

Add confirmation and explanation when the user changes settings/modes that materially affect cost, autonomy, or execution behavior.

### Target Changes

Highest-value candidates in current WUPHF:

- provider switches
- mode switches that reset or restart the runtime
- possibly `1:1` enable/disable flows when context will be lost

### Likely Files

- [channel.go](../cmd/wuphf/channel.go)
- new small dialog/helper file likely:
  - `[channel_confirm.go](../cmd/wuphf/channel_confirm.go)` or similar
- [channel_test.go](../cmd/wuphf/channel_test.go)
- [one-on-one-channel-e2e.sh](../tests/uat/one-on-one-channel-e2e.sh)

### Suggested Implementation Shape

- use the interaction primitives from branch 4
- require confirmation only when:
  - context or session shape materially changes
  - execution provider changes
  - cost/autonomy behavior changes in a meaningful way
- keep copy short and operator-focused

### Termwright Scenarios

1. Switch provider:
   - verify confirmation appears
   - verify cancel path leaves state unchanged
2. Disable `1:1`:
   - verify confirmation if session context will be reset
3. Confirm path:
   - verify health/runtime actually changes after confirm, not before

### Done Means

- high-impact mode/runtime changes never feel like hidden state mutation

## Branch 6: `feat/safety-dialogs`

### Goal

Put bounded, explicit safety UX in front of disruptive operations.

### Target Changes

First candidates in current WUPHF:

- reset
- transcript rewind/restore once it exists
- risky workflow replay/apply/resume actions
- destructive or context-resetting channel commands

### Likely Files

- [channel.go](../cmd/wuphf/channel.go)
- likely new helper:
  - `[channel_dialogs.go](../cmd/wuphf/channel_dialogs.go)` or similar
- [channel_test.go](../cmd/wuphf/channel_test.go)
- relevant UATs:
  - [office-channel-e2e.sh](../tests/uat/office-channel-e2e.sh)
  - [one-on-one-channel-e2e.sh](../tests/uat/one-on-one-channel-e2e.sh)

### Suggested Implementation Shape

- explain:
  - what is about to happen
  - what context may be lost
  - what the user can do instead
- keep detail bounded
- avoid giant configuration screens

### Termwright Scenarios

1. `/reset`:
   - verify dialog appears
   - verify cancel leaves composer/session untouched
   - verify confirm resets
2. Any destructive runtime action introduced later:
   - verify same dialog style and byline behavior

### Done Means

- disruptive actions feel deliberate and reversible where possible

## Phase 1 Implementation Notes

### 1. Do not over-refactor before the first branch lands

`channel.go` is large, but Phase 1 does not require a big decomposition first.

Use this rule:

- extract only the helpers that make the branch cleaner immediately
- defer major `channel.go` splitting until after the first 1-2 branches land

### 2. Keep termwright scenarios small and behavior-focused

Do not build giant end-to-end flows for Phase 1.

Best pattern:

- one short UAT scenario per behavior
- one or two precise assertions per scenario
- keep Go tests as the main correctness net

### 3. Favor visible trust improvements over novelty

If a choice is between:

- adding a new trick
- making an existing interaction safer and clearer

choose the second one in Phase 1.

## Recommended First Branch

Start with:

- `feat/contextual-footer-hints`

Why:

- lowest risk
- immediate visible win
- improves both office and `1:1`
- creates better footing for later recall, confirmations, and dialogs

## Recommended First Three Branches

If you want the highest near-term quality jump, do these first:

1. `feat/contextual-footer-hints`
2. `feat/draft-safe-history`
3. `feat/interaction-primitives`

That combination should noticeably improve:

- operator confidence
- composition safety
- consistency of small interactions

without requiring deeper architectural work yet.
