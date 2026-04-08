# WUPHF TUI Gap Analysis vs Hermes Agent and OpenClaw

## Context

This analysis is grounded in the actual WUPHF repo root.

It replaces an earlier incorrect pass that was done in the wrong neighboring repo.

External references:

- Hermes Agent README: <https://github.com/NousResearch/hermes-agent>
- OpenClaw README: <https://github.com/openclaw/openclaw>

## Executive Summary

WUPHF is not missing a TUI from scratch.

It already has:

- a real multi-pane office UI
- `1:1` mode
- slash-command autocomplete
- task, request, policy, calendar, and skills surfaces
- session and lifetime token/cost display
- live activity hints from agent panes
- Nex / integration setup and direct-mode switching

So the right next step is not a large redesign.

The real opportunity is to tighten three things:

1. make active work more explicit in the main message surface
2. add a clearer readiness / doctor experience
3. make direct `1:1` execution feel more intentional and inspectable

That aligns with the strongest relevant lessons from Hermes and OpenClaw without diluting WUPHF's office model.

## What Hermes Actually Suggests

From the public Hermes README, the parts most relevant to WUPHF are:

- strong terminal interaction quality
- multiline editing
- slash-command autocomplete
- interrupt / redirect behavior
- streaming tool output
- built-in setup and doctor flows
- explicit scheduling and automation affordances

What matters for WUPHF:

- Hermes treats agent execution as something the human can follow in real time
- Hermes treats setup and recovery as first-class product flows, not just docs

What does **not** map directly:

- Hermes is fundamentally a personal agent surface with broad platform reach
- WUPHF is an office runtime with a visible team model and channel-based coordination

So WUPHF should borrow:

- execution visibility
- setup ergonomics
- command clarity

It should not copy:

- the single-assistant product framing
- the remote / gateway-first mental model

## What OpenClaw Actually Suggests

From the public OpenClaw README, the most relevant lessons are:

- onboarding is a product, not a README
- `onboard` and `doctor` are explicit control points
- operational readiness is surfaced clearly
- the control plane is separate from the assistant experience

What matters for WUPHF:

- a new user should know what is missing and what to do next
- risky or incomplete runtime setup should be legible from inside the app

What does **not** map directly:

- OpenClaw is a personal assistant and gateway product
- WUPHF is an office TUI, not a personal messaging hub

So WUPHF should borrow:

- doctor/readiness concepts
- guided setup quality
- explicit runtime health reporting

It should not copy:

- the personal-assistant framing
- the platform-messaging-heavy UX

## What WUPHF Already Has

Compared against the real WUPHF codebase, WUPHF already has far more than the wrong-pass analysis assumed.

Current strengths in the actual repo:

- `1:1` mode already exists via `--1o1` and `/1o1`
- slash autocomplete already exists in both the office stream and the channel UI
- message, task, request, policy, calendar, and skills views already exist
- policy visibility is already present through signals, decisions, actions, and watchdogs
- usage display already shows session and total cost/tokens
- live activity is already tracked from teammate panes
- the calendar is already more than a flat queue
- integration commands already exist in the TUI

This means the real gap is not “WUPHF lacks a modern agent TUI.”

The real gap is that the product still sometimes feels:

- harder to read than it should be
- less explicit about current work than it should be
- less guided during setup / integration-readiness problems than it should be

## The Actual WUPHF Gaps

### 1. Agent work is still too implicit

WUPHF has message history and some activity hints, but active execution is not yet surfaced as a crisp first-class runtime story.

Examples of missing clarity:

- what an agent is doing right now
- whether it is reading, thinking, executing, blocked, or reporting
- which tool / integration / workflow it is currently touching
- what changed in the last few seconds without reading the whole channel

### 2. `1:1` mode needs stronger execution affordances

WUPHF has direct mode, but it should feel more like a deliberate “agent console.”

What is still missing:

- a direct execution timeline
- clearer step-by-step action progress
- stronger separation between internal chatter and direct reporting
- better reassurance that the selected agent is actively working

### 3. Readiness/setup is still too distributed

The pieces exist, but the product does not yet feel as guided as Hermes / OpenClaw.

What is missing:

- a compact doctor/readiness view in the office itself
- clearer distinction between:
  - Nex unavailable
  - provider key missing
  - connected account missing
  - runtime unhealthy
- next-step guidance written for non-technical users

### 4. The command layer is functional, but not expressive enough

Autocomplete exists, but it is still closer to a raw list than to a guided control layer.

What is missing:

- categories in slash autocomplete
- better descriptions for commands
- clearer mode-sensitive command sets
- more instructional failure messages

### 5. Status surfaces need better hierarchy

WUPHF already exposes a lot of signal, but it is not always arranged in the most legible way.

What is missing:

- clearer pane headers
- stronger status-bar summaries
- better “what needs me?” prioritization
- less reliance on reading dense message content for simple state questions

## Recommended Direction

Do **not** do a major UI rewrite.

Do **not** try to make WUPHF feel like Hermes or OpenClaw.

Do:

- keep the office model
- keep channels
- keep the visible team
- make agent execution and setup health much more legible

In short:

- Hermes should influence the execution UX
- OpenClaw should influence onboarding and doctor UX
- WUPHF should stay visibly and unmistakably an office

## Implementation Priorities

### Priority 1: Runtime visibility

Add a normalized runtime status model for agents:

- idle
- reading
- thinking
- tool-running
- blocked
- waiting-human
- reporting

Surface that in:

- roster
- pane header
- `1:1` header
- message/event summaries

### Priority 2: Direct execution timeline

In `1:1`, add a compact execution strip / timeline for:

- action search
- schema lookup
- dry-run
- execute
- workflow create
- workflow schedule
- trigger create / activate

This should be rendered distinctly from chat content.

### Priority 3: Doctor/readiness view

Add `/doctor` or equivalent in the real WUPHF repo and make it show:

- Nex status
- action provider status
- connected account status
- tmux/runtime status
- what is missing
- exact next step

### Priority 4: Better slash-command UX

Upgrade autocomplete to include:

- categories
- clearer descriptions
- mode-aware command lists

### Priority 5: Status hierarchy cleanup

Refine:

- header copy
- status bar
- office summary lines

Focus on:

- what is happening
- what is blocked
- what needs the human

## What To Avoid

- do not replace the office with a single chat view
- do not add visual noise just because Hermes is polished
- do not add remote-session complexity as part of this pass
- do not cargo-cult OpenClaw's gateway model into WUPHF

## Proposed Next Pass

Implement in this order:

1. runtime status model + roster/pane display
2. `1:1` execution timeline
3. doctor/readiness panel
4. slash autocomplete categories and descriptions
5. header/status hierarchy cleanup

## Local Note

At the time of writing, the real WUPHF repo already has an unrelated local modification in:

- `internal/team/launcher.go`

That should be preserved and not overwritten by TUI work.
