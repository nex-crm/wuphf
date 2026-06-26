# 00 — WUPHF onboarding flow

**Replaces:** the 9-step modal wizard removed in Phase 5.

This document describes the live onboarding flow from first install to
first approved issue.

---

## Overview

Onboarding has four stages:

1. **Provider pick** — one screen before the office opens.
2. **CEO greets** — CEO DM opens inside the empty office shell.
   No LLM tokens used until the `draft` phase.
3. **Bridge** — CEO presents two options: start an issue or look around.
4. **Issue draft + approve** — CEO drafts the issue with you; nothing
   runs until you approve.

---

## Stage 1 — Provider pick

The browser opens at `http://localhost:7891`. One screen shows three
runtime cards: Claude Code, Codex, Opencode.

- Pick a detected runtime to proceed.
- Click a missing runtime's card to open its install page.
- "I'll add one later" skips runtime selection and opens the office in
  sandbox mode with no dispatch capability.

After picking, the broker sets `phase = greet` and the office shell
appears with a CEO DM open.

---

## Stage 2 — CEO greets (deterministic form fills)

The CEO DM is open at `dm:ceo:onboarding`. CEO sends the first card
immediately. No LLM call at this stage.

Each card commits before the next appears:

| Card | Field | Kind |
|------|-------|------|
| Office name | `company_name` | `ceo_form_field` |
| One-line description | `description` | `ceo_form_field` |
| Your website | `website_url` | `ceo_form_field` (optional) |
| Blueprint pick | `blueprint_id` | `ceo_chip_row` |
| Team trim | `picked_agents` | `ceo_checklist` |
| Website scan | — | `ceo_scan_chip` (async, read-only) |

After all fields are committed, the broker runs
`seedFromBlueprintLocked` (atomic, once) and transitions to `bridge`.

---

## Stage 3 — Bridge

CEO posts two chip options:

- **Start an issue** — transitions to the `draft` phase.
- **Look around first** — marks onboarding complete. CEO posts a final
  line in the DM. Sidebar unlocks. The user lands on the last-visited
  channel or `#general` on the next session.

Choosing "Look around first" is a fully onboarded state.
`state.CompletedAt` is set at bridge regardless of which path is taken.

---

## Stage 4 — Issue draft and approve

CEO drafts the issue document with the user. Each spec section streams
in order: Goal, Context, Approach, Acceptance criteria.

The issue document is accessible at `/issues/:id`. Status pill shows
`drafting` (lavender, accent color).

Nothing executes until the user clicks **Approve & Start**. This sets
`LifecycleStateApproved` and dispatches the execution lineup.

`state.FirstIssueApprovedAt` is set on first approval for
activation-depth tracking.

---

## Resumption

If the user closes the browser mid-onboarding, the broker resumes from
the last committed phase:

- `state.Phase` records the current phase.
- `state.PendingSuggestion` re-emits the last card (idempotent by
  `Suggestion.ID`).
- The CEO DM transcript is the human-readable record; the machine reads
  `state`, not the transcript.

On reopen, `RootRoute` reads `/onboarding/state`. If `phase` is set and
`onboarded` is false, the Shell renders `OnboardingDMRoute` and
redirects generic URLs to `dm:ceo:onboarding`.

---

## Settings — Integrations

After onboarding, connect Nex in **Settings → Integrations → Nex**.
Enter your email, click **Connect Nex**. The broker calls `/nex/register`
which runs `nex-cli setup <email>`. If nex-cli is not installed, a link
to `nex.ai/register` appears instead.

Once you have your Nex API key, paste it in **Settings → API Keys →
Nex API Key**.
