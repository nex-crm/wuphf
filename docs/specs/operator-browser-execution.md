# Operator browser execution (the EXECUTE half)

**Status:** building, Phase 1 (frontend-first). **Decision date:** 2026-06-30.
**Companion to:** `operator-demo-call-real.md` (the OBSERVE half — the demo call),
`operator-harness-clean-start.md` (S3 deterministic executor).

---

## 1. What we are building

The demo call lets the AI **watch** the operator and build an app from the
demonstrated workflow. This is the other half: letting the AI **run** that app /
workflow on the operator's own computer — **starting with the browser**. When a
workflow step has no API, the AI drives the operator's real Chrome the way a
person would: look at the page, click, type, read the result, move on.

## 2. Decisions (locked 2026-06-30)

- **Driver: the user's real Chrome via CDP.** We connect to the operator's own
  running Chrome (Chrome DevTools Protocol) — their browser, their logins — and
  drive it by screenshotting and dispatching input. Not a cloud sandbox (that is
  not their browser) and not the heavyweight cua native driver yet.
- **Loop: a computer-use agent loop now.** screenshot → model → action → execute
  → repeat, until the step's goal is met or a bound is hit. Gets it working on
  any site immediately.
- **Model: OpenAI `computer-use-preview`.** Same account/key as the realtime
  call; purpose-built to return browser actions from a screenshot.
- **Evolution: record + deterministic replay + CUA-heal.** The agent loop is the
  bootstrap. As steps run, we record the concrete UI actions and replay them
  deterministically next time; the computer-use model is only invoked to *heal*
  when the page has changed (the operator spec's "API-first → UI-replay →
  bounded CUA-heal"). Cheaper, faster, and auditable.

## 3. Architecture

```
┌─ Operator FE — BrowserRunModal ───────────────────────────────────────┐
│  live browser viewport (screenshots) + narrated action timeline       │
│  goal · status (running/paused/needs-you/done) · Pause / Stop         │
│  streams an ExecSession over SSE                                      │
└───────────────┬───────────────────────────────────────────────────────┘
                │  POST /execute/browser  (SSE: action · screenshot · status)
┌───────────────▼───────────────────────────────────────────────────────┐
│  Broker — browser executor                                            │
│   1. connect to the operator's Chrome over CDP                        │
│   2. API-first: Composio steps (HubSpot, Slack) run directly          │
│   3. UI step → computer-use loop:                                     │
│        screenshot → computer-use-preview → action → CDP dispatch      │
│        → screenshot → ... until the step goal is met / bound hit      │
│   4. record the concrete actions for deterministic replay later       │
└────────────────────────────────────────────────────────────────────────┘
```

The FE never drives Chrome (a web page cannot CDP another browser); the broker
does, because it is the local process with access to the operator's Chrome. In
the eventual desktop shell, the shell owns the Chrome connection.

## 4. Wire contract (the ExecSession)

The action vocabulary mirrors `computer-use-preview` so the backend maps 1:1:

- **ExecAction**: `navigate` (url) · `click` (x,y,button,target-label) ·
  `type` (text) · `keypress` (keys) · `scroll` (x,y,dx,dy) · `read`
  (what was observed) · `wait` · `done` (result). Each carries the model's short
  `reasoning` and the `screenshot` it acted on, so the timeline is auditable.
- **ExecStep**: one workflow step (trigger/enrich/ai/decision/action/branch) and
  the actions taken to satisfy it; API steps carry the call instead of actions.
- **ExecSession**: `goal`, ordered `steps`, live `status`
  (`running|paused|needs-you|done|error`), and a final `result`.

Phase 1 ships these types and a **mock runner** that reveals a realistic
inbound-routing execution over time, so the surface is real and clickable before
the backend exists (frontend-first).

## 5. Security & safety

- **Only runs on an explicit operator Start.** The AI never drives the browser on
  its own.
- **Confidential / external sends stay gated.** A step that posts to Slack, sends
  an email, or writes to a CRM still routes through the existing human-approval
  gate — browser execution does not bypass it. The run pauses to `needs-you`.
- **Bounded.** Each step has a max-action budget; the loop stops and asks rather
  than thrash.
- **Visible & stoppable.** Every action is shown live with its screenshot; Pause
  and Stop are always available.
- The real CDP connection requires the operator's Chrome started with remote
  debugging (or the desktop shell launching it); the long-lived OpenAI key stays
  on the broker exactly as for the realtime call.

## 6. Slice plan

- **E1 — Execute surface (frontend-first, this PR).** ExecSession contract +
  mock computer-use loop + BrowserRunModal + a "Run in browser" entry on a tool.
- **E2 — Real executor.** Broker `/execute/browser`: CDP to the operator's
  Chrome + the computer-use-preview loop, streamed over SSE; API-first for
  Composio steps; the approval gate for external sends.
- **E3 — Record + replay + heal.** Persist the concrete actions per step; replay
  deterministically; invoke computer-use only to heal a changed page.
- **E4 — Beyond the browser.** Extend the same loop to desktop apps (cua native
  driver) once the browser path is proven.
