# cua migration plan — unified computer-use for execution

**Status:** plan, pre-build. **Decision date:** 2026-06-30. **Supersedes:** the
"computer-use-preview vision loop" and "browser-harness sidecar" options in
`operator-browser-execution.md` §2. **Companion to:**
`operator-browser-execution.md` (the EXECUTE surface, frontend-first),
`operator-demo-call-real.md` (the OBSERVE call).

---

## 1. Decision

Commit to **[trycua/cua](https://github.com/trycua/cua)** as the single engine
for the AI to **execute** apps and workflows on the operator's own computer —
**browser first, the rest of the desktop next** — with **no intermediate
framework**.

We considered starting with `browser-use/browser-harness` (browser-only, Python,
self-healing) and explicitly rejected it as a starting point: it is a *different*
framework we would integrate and then rip out, which is throwaway work and exactly
the "disconnected dependencies" we want to avoid. cua already covers the browser
(its native driver controls the real desktop, including the real Chrome with the
operator's real logins), so "starting with browser" is just *pointing cua at the
browser first* — same engine, no migration. We accept that cua does not give the
self-heal/skill-writing for free; we build record + deterministic replay + heal on
top of cua ourselves, which was always the spec's plan.

## 2. What cua gives us

- **cua-driver** — a native macOS/Windows **background** computer-use driver. It
  "drives native apps without stealing focus" and **speaks MCP over stdio**.
  Installed and registered as an MCP server:
  ```
  claude mcp add --transport stdio cua-driver -- cua-driver mcp
  ```
  It exposes screenshot/click/type-style tools over MCP. This is how we drive the
  operator's **real** desktop and Chrome.
- **cua agent SDK** — the computer-use loop (screenshot → model → action →
  execute), model-agnostic (Claude, OpenAI `computer-use-preview`, others), with
  **custom computer handlers** so the loop can drive any "computer" — including
  cua-driver on the real machine. cua has **no default model**; you pass one
  explicitly. **We use OpenAI `computer-use-preview`** — confirmed accessible on
  the operator key, same account as the realtime call, purpose-built for browser
  actions. The model string stays config-swappable so we can A/B Claude later.
- **cua sandboxes / Lume** — isolated VM desktops. Not the operator's machine;
  kept in reserve for safe/headless runs, not the primary path.

## 3. Architecture

```
┌─ Operator FE — BrowserRunModal (already built, mock) ─────────────────┐
│  live viewport (cua screenshots) · narrated actions · permission +    │
│  external-send gates · Pause/Stop — consumes an ExecSession over SSE   │
└───────────────┬───────────────────────────────────────────────────────┘
                │  POST /execute (SSE: action · screenshot · status)
┌───────────────▼───────────────────────────────────────────────────────┐
│  Broker (Go)  /execute  — launches + proxies the cua runner            │
└───────────────┬───────────────────────────────────────────────────────┘
                │  spawns, streams stdout/SSE
┌───────────────▼───────────────────────────────────────────────────────┐
│  cua runner (Python sidecar) — the committed cua integration          │
│   ComputerAgent(model, tools=[cua-driver computer]) loop for a goal    │
│   emits {action, screenshot, status} per step                         │
└───────────────┬───────────────────────────────────────────────────────┘
                │  MCP over stdio
┌───────────────▼───────────────────────────────────────────────────────┐
│  cua-driver — drives the operator's REAL Chrome / desktop (their       │
│  logins), in the background, no focus stealing                         │
└────────────────────────────────────────────────────────────────────────┘
```

**Why a Python sidecar (not Go talking MCP directly):** the cua **agent SDK** is
Python and already implements the loop + cua-driver computer handler. A thin
Python runner is the cua-idiomatic integration and far less code than
reimplementing the loop in Go. The broker stays the single FE entry point: it
spawns the runner, proxies its stream as SSE into the Run modal, and owns the
OpenAI/Anthropic key (never sent to the browser). In the eventual desktop shell,
the shell bundles cua-driver + the runner.

## 4. The first browser slice (E2-cua)

Outcome: click **Run** on a workflow → cua drives the operator's **real Chrome**
to accomplish one step → every action streams live into the Run modal, gated.

1. **Install + smoke-test cua-driver** locally; confirm it drives the real Chrome
   (screenshot + a click) outside the product first.
2. **cua runner** (`runner/cua_exec.py`): a `ComputerAgent` loop over a goal +
   start URL, computer = cua-driver, **model = OpenAI `computer-use-preview`**
   (config-swappable). Emits
   one JSON line per step: `{type:"action"|"status"|"done"|"error", ...}` with the
   screenshot and the action label/reasoning — the **same ExecSession shape** the
   mock already uses, so the FE is unchanged.
3. **Broker `/execute`**: spawn the runner with the goal, stream its lines to the
   FE as SSE; resolve the model key server-side; enforce the bound.
4. **FE**: swap `BrowserRunModal`'s mock loop for the SSE when a real run is
   available; keep the **browser-permission gate** (ask before the first cua
   action) and the **external-send approval gate** (Slack/email/CRM). Mock stays
   as the keyless/cua-absent fallback.
5. **Verify** on a real goal in a real Chrome, watching the window move + the
   modal mirror it.

## 5. Add / remove (the "disconnected dependencies" audit)

**Add:**
- `cua-driver` (local install; later bundled by the desktop shell).
- `runner/` — the Python cua runner (the committed cua integration).
- Broker `/execute` SSE endpoint that spawns + proxies the runner.

**Remove / keep:**
- **Removed already:** the throwaway TS Playwright + `computer-use-preview`
  executor (`executor/`) — never use a second browser engine.
- **Do NOT add:** `browser-use/browser-harness`, raw `chromedp`/Playwright drivers,
  or any second computer-use path. One engine.
- **Keep:** `BrowserRunModal` + `browserExec.ts` — the FE surface and the
  ExecSession wire shape cua streams into (connected, validated). The mock runner
  stays only as the fallback.
- **Audit at switch time:** once cua lands, sweep for anything left disconnected
  (unused exec/vision scaffolding, stale deps) and delete it in the same PR.

## 6. Security & safety (unchanged from the surface)

- **Explicit Start only**; the AI never drives on its own.
- **Permission gate** before the first browser/desktop action ("Let Nex control
  your browser?").
- **External-send approval** (Slack/email/CRM) still pauses to `needs-you` —
  cua does not bypass it.
- **Bounded** per step; **visible + stoppable** (every action + screenshot shown;
  Pause/Stop always live).
- Long-lived model key stays on the broker; cua-driver runs on the operator's
  machine under explicit OS screen-recording/accessibility grants surfaced in
  onboarding.

## 7. Phases

- **C1 — cua browser execution** (§4): the real Run, browser-first.
- **C2 — record + deterministic replay + heal (DONE):** `cua_exec.py` records a
  live run's actions as a trajectory keyed by each element's STABLE identity
  (role + label, never the per-snapshot index), emitted as a `trajectory` event.
  `--replay` matches each step by role+label and executes it with NO model call;
  only a step whose element is gone HEALS (one scoped model call), and the
  corrected step is re-recorded so the trajectory self-improves. Broker
  `POST /execute/replay` runs it; `BrowserRunModal` replays a saved trajectory
  (localStorage, keyed by tool+goal) on the second run instead of driving live,
  badging healed steps. Turns a repeat run from ~12s of LLM clicking into
  near-instant element-matched replay.
- **C3 — beyond the browser:** the same cua-driver loop on desktop apps.
- **C4 — desktop shell:** Electron/Wails bundles cua-driver + the runner; one-click
  install + permissions.
- **C5 — reframe OBSERVE on cua (IN PROGRESS):** the demo call sent only
  `input_image` screenshots, so the AI guessed from pixels — it never read the
  DOM/components. Replace that with cua reading the real page. **Slice 1 done:**
  `runner/cua_observe.py` polls the FRONTMOST window every ~2.5s and captures the
  **AX component tree** (roles + labels) + **visible text** (`page get_text` on
  browser windows), diffing across ticks to emit `navigate` events; the broker
  will stream these and assemble the build handoff alongside the verbatim
  transcript. Voice path untouched → no call latency.
  - **Finding:** `start_recording` only logs cua-driver's OWN actions, so it
    captures nothing while a human demos. The event log therefore comes from
    snapshot diffs, not the recorder. (The recorder is for C2 — recording cua's
    *execution* for replay.)
  - **Finding:** on macOS `query_dom` just re-returns the AX tree (no real HTML
    attrs); real selectors + URL need `page execute_javascript`, which requires a
    one-time "allow JS from Apple Events" opt-in + Chrome restart. Deferred as a
    follow-up; AX components + text are rich enough to stitch a workflow.
  - **Latency guards:** bounded per-call timeouts that degrade to a partial
    snapshot instead of stalling; `page` reads only on browser windows; capture
    runs off the voice path and feeds the (async) handoff.
  - **Slices 2-3 done:** broker `POST /observe/browser` (SSE) runs the observer
    (no key; 503 → call proceeds without it); `RealCallModal` runs it alongside
    the call off the voice path, accumulates snapshots, shows a live "N pages
    read" count, and at Build folds the reduced screens into the capture;
    `capturePromptSeed` renders a "Real page structure Nex read (ground truth)"
    section before the verbatim transcript. The model's screenshot-based draft
    is kept as its interpretation; the cua capture is the ground truth.
  - **Remaining:** the live e2e on the operator's box (broker up + a real
    multi-screen demo), and the optional `execute_javascript` upgrade for real
    HTML selectors + URLs (gated on the Apple-Events-JS opt-in).

## 8. Open questions

1. **Model for the loop:** ✅ DECIDED — OpenAI `computer-use-preview` (confirmed
   accessible on the operator key; cua has no default). Kept config-swappable so
   Claude computer-use can be A/B'd later.
2. **Runner transport:** the broker spawns the Python runner and reads its stdout
   (newline-JSON) → SSE. Confirm packaging (uv/venv) and how the desktop shell
   ships Python (or a frozen binary).
3. **Real Chrome vs a dedicated profile:** drive the operator's main Chrome, or a
   cua-managed Chrome profile they log into once (cleaner, isolates the agent from
   their personal tabs). Decide at C1.
4. **cua-driver licensing/bundling** for the shipped desktop app (C4).
