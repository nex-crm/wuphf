# CC-agent Lessons: Phase 2 Execution Plan

## Purpose

This is the implementation-ready plan for Phase 2 of the CC-agent-inspired WUPHF improvements.

It corresponds to the `Medium UX Wins` portion of:

- [cc-agent-implementation-roadmap.md](/Users/najmuzzaman/Documents/nex/WUPHF/docs/cc-agent-implementation-roadmap.md)

This doc answers:

- what the exact Phase 2 branches should be
- which real WUPHF files are likely to change
- what each branch should prove
- what termwright scenarios should validate

## Current WUPHF Code Map for Phase 2

Phase 2 is mostly about making existing office/direct capabilities feel much more deliberate and navigable.

### Channel UX and transcript surfaces

- [channel.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel.go)
- [channel_messages.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_messages.go)
- [channel_thread.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_thread.go)
- [channel_render.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_render.go)
- [channel_layout.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_layout.go)
- [channel_sidebar.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_sidebar.go)
- [channel_test.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_test.go)

What lives there today:

- office vs `1:1` rendering
- runtime strip
- channel/thread message stream
- roster/sidebar state
- notices, inline status, and event rendering

### Picker, overlay, and focused interaction surfaces

- [picker.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/tui/picker.go)
- [picker_test.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/tui/picker_test.go)
- [autocomplete.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/tui/autocomplete.go)
- [autocomplete_test.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/tui/autocomplete_test.go)

What lives there today:

- simple picker behaviors
- autocomplete list selection
- lightweight overlay-style interactions

### Human requests, interviews, approvals, and office state

- [broker.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/team/broker.go)
- [ledger.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/team/ledger.go)
- [broker_test.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/team/broker_test.go)

What lives there today:

- pending interview state
- approvals and requests
- action/decision logging
- scheduler and skill state

### Existing UAT coverage

- [office-channel-e2e.sh](/Users/najmuzzaman/Documents/nex/WUPHF/tests/uat/office-channel-e2e.sh)
- [one-on-one-channel-e2e.sh](/Users/najmuzzaman/Documents/nex/WUPHF/tests/uat/one-on-one-channel-e2e.sh)
- [human-judgment-uat.sh](/Users/najmuzzaman/Documents/nex/WUPHF/tests/uat/human-judgment-uat.sh)

What they already prove:

- office boot
- `1:1` boot and switching
- basic command handling
- general office render sanity

## Recommended Phase 2 Branch Order

Recommended order:

1. `feat/structured-human-interviews`
2. `feat/approval-steering`
3. `feat/agent-office-switcher`
4. `feat/unread-navigation-semantics`
5. `feat/transcript-recovery`
6. `feat/away-summaries`
7. `feat/in-channel-readiness`
8. `feat/insert-search-surfaces`

That order keeps the work coherent:

- branch 1 upgrades the weakest human interaction path first
- branch 2 improves high-stakes approvals while the interview machinery is fresh
- branch 3 establishes one canonical navigation surface for office/direct multi-agent work
- branch 4 makes long sessions easier to re-enter and scan
- branch 5 adds safer transcript surgery once navigation is stronger
- branch 6 improves return moments
- branch 7 makes setup/blockers visible in-context
- branch 8 improves authoring ergonomics once the main flows are stable

## Branch 1: `feat/structured-human-interviews`

### Goal

Turn blocking interviews into explicit mini-flows instead of relying on raw transcript back-and-forth.

### What Should Change

- progress state for interview questions
- clearer current-question focus
- skip / continue / review-before-submit semantics
- optional notes or rationale field where relevant
- better in-channel rendering for interview progress and completion

### Likely Files

- [broker.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/team/broker.go)
- [channel.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel.go)
- [channel_render.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_render.go)
- likely new helper:
  - `[channel_interview.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_interview.go)` or similar
- [channel_test.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_test.go)
- [broker_test.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/team/broker_test.go)

### Termwright Scenarios

1. Office interview pending:
   - user can see progress and current question clearly
2. Skip / continue:
   - interview flow advances without losing context
3. Review-before-submit:
   - user can verify answers before final submission
4. `1:1` blocking interview:
   - direct session remains clear and uncluttered

### Done Means

- interviews feel like guided workflows instead of chat interruptions
- users can tell how far through an interview they are

## Branch 2: `feat/approval-steering`

### Goal

Let human approvals include steering at the decision point instead of only yes/no.

### What Should Change

- approve
- approve with note
- reject
- reject with steer
- better approval rendering in-channel and in `1:1`
- clearer summary of what the approval affects

### Likely Files

- [broker.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/team/broker.go)
- [channel.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel.go)
- [channel_messages.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_messages.go)
- [channel_render.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_render.go)
- [channel_test.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_test.go)
- [broker_test.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/team/broker_test.go)

### Termwright Scenarios

1. Approval with note:
   - note is captured and visible to the agent/runtime
2. Reject with steer:
   - rejection keeps the corrective guidance
3. Office approval:
   - approval state is visible in-channel
4. Direct approval:
   - `1:1` completion report stays clear and direct

### Done Means

- approvals capture operator intent, not only permission

## Branch 3: `feat/agent-office-switcher`

### Goal

Introduce one canonical switcher for office, direct sessions, and agent/task transcripts.

### What Should Change

- a primary switcher surface that always includes:
  - main office
  - direct sessions
  - active agent/task transcripts
- last-activity summary per destination
- clearer viewed vs active vs running semantics
- “back to main office” is always obvious

### Likely Files

- [channel.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel.go)
- [channel_sidebar.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_sidebar.go)
- [channel_layout.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_layout.go)
- [picker.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/tui/picker.go)
- [picker_test.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/tui/picker_test.go)
- [channel_test.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_test.go)

### Termwright Scenarios

1. Switch from office to agent/task transcript:
   - path is obvious and reversible
2. Switch back to main office:
   - main stays a first-class destination
3. Idle/completed agent:
   - still visible and reviewable
4. Active unread agent:
   - switcher shows enough state to know why it matters

### Done Means

- WUPHF has one canonical navigation surface for office vs agent views

## Branch 4: `feat/unread-navigation-semantics`

### Goal

Make “new since you looked” a first-class concept in channels and direct sessions.

### What Should Change

- unread divider
- jump-to-latest behavior
- compact “new messages” affordance
- better handling when the user has scrolled away from the bottom

### Likely Files

- [channel_messages.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_messages.go)
- [channel_render.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_render.go)
- [channel_layout.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_layout.go)
- [channel_test.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_test.go)
- [office-channel-e2e.sh](/Users/najmuzzaman/Documents/nex/WUPHF/tests/uat/office-channel-e2e.sh)
- [one-on-one-channel-e2e.sh](/Users/najmuzzaman/Documents/nex/WUPHF/tests/uat/one-on-one-channel-e2e.sh)

### Termwright Scenarios

1. User scrolls away while new messages arrive:
   - unread affordance appears
2. Jump to latest:
   - unread state clears correctly
3. Direct session with new agent report:
   - unread semantics remain clean

### Done Means

- long-running sessions feel grounded and re-enterable

## Branch 5: `feat/transcript-recovery`

### Goal

Turn transcript rewind/restore/summarize into explicit operator tools.

### What Should Change

- restore from selected point
- summarize from here / summarize up to here
- safer recovery affordances
- clearer explanation of what restore will change

### Likely Files

- [channel_thread.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_thread.go)
- [channel.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel.go)
- [channel_messages.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_messages.go)
- likely new helper:
  - `[channel_recovery.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_recovery.go)` or similar
- [channel_test.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_test.go)

### Termwright Scenarios

1. Select message point:
   - restore options are visible and bounded
2. Summarize from here:
   - summary path is clear before execution
3. Cancel recovery:
   - no hidden transcript mutation occurs

### Done Means

- transcript surgery feels deliberate and recoverable

## Branch 6: `feat/away-summaries`

### Goal

Add concise “while you were away” summaries for office and direct sessions.

### What Should Change

- per-channel summary
- per-`1:1` summary
- “what changed / why it matters / what next” framing
- summary trigger on return or explicit recall

### Likely Files

- [broker.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/team/broker.go)
- [ledger.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/team/ledger.go)
- [channel.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel.go)
- [channel_render.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_render.go)
- [channel_test.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_test.go)

### Termwright Scenarios

1. Leave office, accumulate changes, return:
   - summary appears and is readable
2. Leave `1:1`, return:
   - summary is scoped to that direct session
3. Summary with no meaningful changes:
   - stays quiet

### Done Means

- returning to WUPHF feels guided instead of forensic

## Branch 7: `feat/in-channel-readiness`

### Goal

Surface setup and readiness blockers in the main flow instead of only via `/doctor`.

### What Should Change

- blocked/provider-disconnected cards in-channel
- stronger empty/offline state guidance
- setup-required notices that point to next action
- partial-readiness explanations in the workspace

### Likely Files

- [channel.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel.go)
- [channel_render.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_render.go)
- [channel_messages.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_messages.go)
- [broker.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/team/broker.go)
- [channel_test.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_test.go)

### Termwright Scenarios

1. Missing provider key:
   - workspace surfaces it clearly
2. Connected account missing:
   - in-channel guidance is specific
3. Partial office readiness:
   - user can tell what still works

### Done Means

- readiness problems meet the user in the workspace

## Branch 8: `feat/insert-search-surfaces`

### Goal

Improve authoring with insertion/search overlays instead of making everything raw text composition.

### What Should Change

- richer path/reference insert surfaces
- safer insertion into prompts and workflow specs
- search surfaces that support insertion, not only navigation

### Likely Files

- [picker.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/tui/picker.go)
- [channel_composer.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_composer.go)
- [autocomplete.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/tui/autocomplete.go)
- [channel.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel.go)
- [picker_test.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/tui/picker_test.go)
- [channel_test.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_test.go)

### Termwright Scenarios

1. Insert path/reference into prompt:
   - result lands exactly in composer
2. Search while composing a workflow prompt:
   - authoring is easier than raw typing
3. Cancel overlay:
   - draft remains intact

### Done Means

- composition becomes faster and less brittle

## Phase 2 Success Criteria

- interviews feel structured and finishable
- approvals capture guidance, not only permission
- office/agent navigation is coherent
- unread and away semantics make long sessions easier to manage
- readiness issues surface naturally where the user already is
- authoring feels assisted, not purely text-only
