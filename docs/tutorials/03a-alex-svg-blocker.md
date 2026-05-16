# 03a — Alex catches the SVG/PNG blocker (Scenario 3: autonomous work)

## Who and why

**Persona:** Alex Chen, solo dev, ex-Stripe. They have already seen the
goal-decomposition scene and now they want to test the magic-moment
claim: agents surface a real blocker on their own without the human in
the loop.

**Outcome they came for:** close the tab, come back in 20 minutes, see
that ENG flagged a real engineering blocker AND DSG resolved or routed
it without Alex touching anything.

## Steps

### 1. Drop a goal that has a latent format mismatch

In `#general`:

```
Wire up the marketing hero with the new SVG illustrations from DSG.
Mobile breakpoints are the priority.
```

#### Verify

- The message lands.
- ENG acknowledges in-thread within ~60s.

### 2. Close the browser tab

Literally close the tab. Do not stop the wuphf CLI.

### 3. Wait 20 minutes

Make coffee. Read the news. Do not interrupt the agents.

### 4. Re-open `http://localhost:7891`

#### Verify

- The shell remembers `#general` as the last channel (no re-onboarding).
- `#general` shows new messages between ENG and DSG that happened while
  the tab was closed. At least one of them surfaces a real, named
  blocker — for example "SVG sprite is missing mobile-optimized
  viewport metadata" or "PNG fallbacks aren't sized for 2x retina".

### 5. Open the Inbox

Sidebar → **Inbox**.

#### Verify

- The Task row for the hero work shows state `running` or `decision`,
  not `intake`. (The agents made forward progress.)
- The detail pane shows a "Dead ends" section or a "Blocked on" with
  the specific format mismatch named.

## What success looks like

When Alex returns 20 minutes later, they see a written, timestamped
exchange between ENG and DSG that surfaced one specific technical
blocker AND moved past it — all without Alex sending a single nudge.
This is the magic moment Scene 4 of the ICP doc names. If they can
screenshot the channel transcript and the Decision Packet "Dead ends"
section side by side, the feature is shipping correctly.
