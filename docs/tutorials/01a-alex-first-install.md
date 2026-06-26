# 01a — Alex's first install (Scenario 1: install + first look)

## Who and why

**Persona:** Alex Chen, solo dev, ex-Stripe. Has spent two weeks trying
to wire Python agents to coordinate. Saw WUPHF on a friend's Slack and
opened the README at 11pm on a Tuesday.

**Outcome they came for:** prove this is not another framework that
needs a weekend to evaluate. Either they see a coordinating office in
five minutes or they close the tab.

## Steps

### 1. Install

In a clean terminal, with `npx` available:

```bash
npx wuphf
```

#### Verify

- The CLI prints a single welcome line plus the URL it just opened.
- A browser tab opens at `http://localhost:7891`.
- The CLI process keeps running (no `Ctrl+C` required to keep the
  office alive).

### 2. Pick a runtime

The browser opens at `http://localhost:7891` with a single screen: three
runtime cards (Claude Code, Codex, Opencode).

#### Verify

- The screen shows exactly three runtime cards.
- Each card shows a detected version or "Not installed".
- A "I'll add one later" skip affordance is at the bottom.

Pick the detected runtime (or skip if none are installed).

### 3. Office opens — CEO greets

After picking, the office shell appears with a CEO DM open. CEO sends
the first form-fill card immediately.

#### Verify

- The office shell is visible with a sidebar.
- The CEO DM is open (`dm:ceo:onboarding`).
- A card asking for office name (or first question) is visible.
- No login screen. No "create your team" wizard.

Fill in the form fields. After all fields are committed, the broker
seeds the office and CEO presents the bridge: start an issue or look
around first.

### 4. Drop a goal into `#general`

Navigate to `#general` after the bridge step and type in the composer:

```
Ship the onboarding flow by Friday.
```

#### Verify

- The message appears in the channel.
- Within ~60s, CEO posts a reply that tags `@ENG` and `@DSG` with a
  decomposed plan.
- The participant column shows ENG transitioning from idle to "running".

## What success looks like

Within five minutes of running `npx wuphf`, Alex picks a runtime, fills
in the office name and blueprint through the CEO DM, and sees CEO mention
`@ENG` and `@DSG` with a decomposed plan. The pre-pick screen to first
agent reply is the activation arc.
