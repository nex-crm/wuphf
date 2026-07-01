# Browser execution as a workflow step (not a Run button)

**Status:** slices 1–3 done (authoring → bind → execution → in-chat approval).
Remaining: 5 (retire the standalone modal), 6 (compile-time step rendering).
**Supersedes** the standalone `BrowserRunModal` "Run in browser" button. **Builds
on** the cua execution engine (C1/C2/send-gating): `runner/cua_exec.py` + broker
`/execute` · `/replay` · `/approve` · `/observe`.

## Principle
A workflow runs on APIs (Composio) when it can. **When there is no integration
for what a step needs, that step becomes a `browser` step** — Nex drives the
browser to do it. Browser execution is therefore a *step type*, invoked by the
workflow engine when the run reaches it — never a standalone button/modal.
**Permission (browser control) and the send-gate are asked in the app chat**,
conversationally; the operator's reply resumes the paused run.

## How a browser step is created — "no integration available → browser step"
- **B (build/author time):** the plan-authoring model may emit `kind:"browser"`
  for a step that must touch an external system the app has no integration for;
  it describes the exact goal in `detail`.
- **A (compile/bind time):** if the model names an integration the app does NOT
  actually have on an action/send step, that step is converted to a `browser`
  step instead of being silently dropped — the honest "there is no API for this,
  so drive the browser" fallback.

A browser step: `kind:"browser"`, `integration:""`, no `action_id`, `detail` =
the natural-language sub-goal, `gated` carried through (a browser *send* still
needs approval).

## Execution (engine → cua)
When the frozen workflow runs and reaches a `browser` step, the engine hands the
step's `detail` sub-goal to the cua runner (the existing `/execute` machinery)
instead of a Composio action. cua records + replays (C2), so repeat runs of a
browser step stay deterministic; the model is invoked only to heal a changed
page. A gated browser step routes through the same send-gate.

## Permission + approval in chat (not a modal)
The run **pauses** at a browser step and posts into the app chat:
> "This step has no integration, so I'll do it in your browser: *<sub-goal>*. Let
> me control your browser to run it?"

The operator's chat reply (allow / not now) resumes or skips the step — reusing
the `run_id` + stdin back-channel already built for the send-gate
(`/execute/approve`). A gated send inside the step asks again, in chat, before it
sends.

## Slices
1. **Authoring — DONE.** The plan model + parser produce `browser` steps when
   there is no integration (A + B). `kind:"browser"` is a first-class kind.
2. **Bind + engine tolerance — DONE.** The resolver binds a browser step
   (`BoundStep{Type:"browser"}`, goal in the template); the Composio spec
   validates a `browser` type; `executeWorkflowStep` emits a deterministic marker
   (`{type:browser, goal, runs_in_browser}`) so a frozen run never breaks.
3. **Execution — DONE (3a).** The engine calls an injected `action.BrowserStepRunner`
   (a package-var hook, so the action package never imports the broker/cua). The
   broker wires a cua-backed impl (`runBrowserStepViaCua`, `broker_browser_step.go`)
   that drives cua for the step's goal on a REAL run. A **dry** run previews (no
   drive); an **unwired** host degrades to a marker; **sends are auto-denied**
   (they need 3b's chat approval). Chosen option (a) over broker orchestration —
   the engine stays agnostic.
4. **Chat permission + send-gate (3b) — DONE.** A live (non-dry) run now PAUSES
   at a browser step and asks the operator *in the app chat*: first for
   permission to control the browser (`kind:"control"`), then again before any
   external send inside the step (`kind:"send"`). The operator's reply resumes or
   skips the paused step. Implementation:
   - **Pause = an in-process rendezvous.** `browserApprovals` (registry,
     `internal/team/broker_browser_approval.go`) holds one channel per ask; the
     browser step BLOCKS on `ask(ctx, appID, kind, goal)` until resolved,
     cancelled, or timed out (3 min → deny). The run's HTTP request stays open
     across the wait — the same held-open model the send-gate already uses on
     the execute stream. Default is DENY on disconnect/timeout, and a browser
     restart mid-pause simply denies (safe); durable cross-restart run state is a
     later hardening, not required here.
   - **Surfaced to chat** by `GET .../workflow/browser/pending` (poll while a live
     run is in flight) and resolved by `POST .../workflow/browser/approve`
     (`{approval_id, decision}`). The app id is threaded on the run context
     (`browserStepAppIDKey`); a run with NO app id (scheduler/cron/headless) has
     no operator to ask, so the step is **skipped, never driven**.
   - **Runner send-gate reused unchanged:** the runner still emits
     `approval_request` + blocks on stdin; the broker now routes that ask to
     chat (instead of auto-denying) and forwards the decision to stdin.
   - **FE:** `AppWorkflowTab` gains a "Run live" action (dry_run=false); while it
     is in flight it polls the asks and renders a conversational Allow / Not now
     card per pending approval (`browserApprovals.ts`).
5. **Retire the modal:** remove the standalone `BrowserRunModal` "Run" button.
6. **FE polish:** render a `browser` step in the frozen workflow view with its own
   glyph + "runs in your browser" affordance (the run-time approval card is done
   in 3b; this is the compile-time step rendering).

## Reused, unchanged
`runner/cua_exec.py` (execute/record/replay/heal + `needs_approval`), broker
`/execute` · `/replay` · `/approve` · `/observe`, the `run_id` stdin
back-channel. Only the *surface* changes: from a modal button to a workflow step
+ chat approval.
