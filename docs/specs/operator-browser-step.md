# Browser execution as a workflow step (not a Run button)

**Status:** plan + slice 1. **Supersedes** the standalone `BrowserRunModal` "Run in
browser" button. **Builds on** the cua execution engine (C1/C2/send-gating):
`runner/cua_exec.py` + broker `/execute` · `/replay` · `/approve` · `/observe`.

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
3. **Execution — the architectural bridge (NEXT, do carefully).** Run a `browser`
   step via the cua runner. The hard part: the deterministic workflow run
   (`ExecuteWorkflow`) is **synchronous/batch**, while cua is **streaming +
   interactive with a mid-run chat approval**. Options:
   - **(a) injected step-executor hook** — the action package calls an injected
     `browserStepRunner(ctx, goal)` (package-var, like `realtimeHTTPClient`); the
     broker supplies a cua-backed, chat-gated implementation. Keeps the engine
     agnostic; the runner blocks until cua finishes/approves.
   - **(b) broker orchestration** — the broker runs the workflow itself, calling
     `ExecuteWorkflow` for Composio steps and cua for browser steps, interleaved.
     More control, but re-implements the step loop + scope/data flow.
   Lean **(a)**. Either way the browser step pauses for the in-chat permission via
   the existing `run_id`/stdin back-channel before it drives, and a gated send
   inside it asks again.
4. **Chat permission + send-gate** surfaced in `AppBuilderChat` (part of slice 3's
   flow).
5. **Retire the modal:** remove the standalone `BrowserRunModal` "Run" button.
6. **FE:** render a `browser` step in the workflow view (its own glyph + "runs in
   your browser") and the chat approval affordance.

## Reused, unchanged
`runner/cua_exec.py` (execute/record/replay/heal + `needs_approval`), broker
`/execute` · `/replay` · `/approve` · `/observe`, the `run_id` stdin
back-channel. Only the *surface* changes: from a modal button to a workflow step
+ chat approval.
