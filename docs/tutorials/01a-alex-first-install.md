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

### 2. First look

In the browser:

#### Verify

- The shell shows a left sidebar with channels under a default office,
  a primary **Inbox** entry, and a small **Access & Health** entry.
- `#general` is selected by default.
- Four agent rows are visible in the participants column on the right:
  CEO, ENG, DSG, CMO.
- No login screen. No "create your team" wizard.

### 3. Drop a goal into `#general`

In the message composer:

```
Ship the onboarding flow by Friday.
```

#### Verify

- The message appears in the channel.
- Within ~60s, CEO posts a reply that tags `@ENG` and `@DSG` with a
  decomposed plan.
- The participant column shows ENG transitioning from idle to "running".

## What success looks like

Five minutes after running `npx wuphf`, Alex sees CEO mention `@ENG`
and `@DSG` by name with a decomposed plan that names a dependency.
That single moment is the activation — every other step is supporting
infrastructure.
