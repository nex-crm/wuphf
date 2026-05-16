# 05a — Alex requests a Day-2 postmortem (Scenario 5: memory)

## Who and why

**Persona:** Alex Chen, solo dev, ex-Stripe. They have run WUPHF for
24 hours. Yesterday's onboarding-flow goal led to a PR, a blocker
resolved without them, and a small copy disagreement between ENG and
CMO. They want a structured postmortem they can paste into their own
weekly review. The test: do the agents remember what happened, or do
they ask Alex to re-state the context?

**Outcome they came for:** type one sentence, get a postmortem draft
that names the specific artifacts (PR number, blocker, CMO copy thread)
from yesterday without prompting.

## Steps

### 1. Open `#general` 24 hours after the initial run

#### Verify

- The channel still has the message history from yesterday.
- Yesterday's Task row still appears in Inbox under the "Approved"
  bucket (it is now terminal).

### 2. Drop the postmortem request

```
Write a post-mortem on yesterday's onboarding launch. Include what
shipped, what blockers came up, and one lesson for next time.
```

#### Verify

- CEO acknowledges within 60s.
- A new "Decision" task appears in Inbox with the postmortem title.

### 3. Open the new Decision row in Inbox

#### Verify

- The detail pane shows a structured postmortem:
  - **What shipped:** references yesterday's PR by title/number.
  - **Blockers:** names the SVG/PNG format issue (or whatever the
    real blocker was) with a one-line description.
  - **Lesson:** at least one concrete sentence (not generic).
- The reviewer-grade panel shows at least one agent grade.

### 4. Click into the wiki article via the citation chip

#### Verify

- A wiki article exists under `wiki/postmortems/onboarding-...` (or
  similar) containing the same content as the Decision Packet.

## What success looks like

The postmortem references at least three specific events from the
previous 24 hours (the PR, the blocker, the copy resolution) without
Alex re-stating any of them in the request. If the agents reach back
into yesterday's transcript, the "agents remember context" claim is
real. If the postmortem is generic, the memory layer is not pulling
the prior session's facts.
