# Real "Demo workflow to Nex" call (operator S5/S6)

**Status:** building, Phase A. **Decision date:** 2026-06-30. **Supersedes:** the
mock `CallModal` for accounts with a Realtime key. **Companion to:**
`operator-harness-clean-start.md` (S5 browser capture, S6 the magical call),
`operator-mlp-prototype-brief.md` (§10 deferred real backend).

---

## 1. What we are building

Replace the scripted mock call with a **real screen-share + realtime-voice
session**. The operator demonstrates a workflow on their screen while talking;
Nex watches the screen and listens, asks questions, and at the end **drafts (or
reworks) a deterministic tool** — handing its captured context straight into the
build engine that already exists (`capturePromptSeed` → `WorkflowBuilder`).

The mock stays as the keyless floor: with no Realtime key the call is the
honest scripted preview; with a key it is the real thing.

## 2. The cua decision (shell)

We evaluated [`trycua/cua`](https://github.com/trycua/cua) — open-source
infrastructure for computer-use agents. Its load-bearing piece for us is **Cua
Drivers**: a locally-installed native service that **observes and drives the
real macOS/Windows desktop in the background** (no focus stealing) and exposes
an **MCP server** (`cua-driver mcp`).

That settles the shell question: **true computer-use is a native-desktop
capability**. A cloud browser tab can screen-share a *picked window/screen* via
`getDisplayMedia`, but it cannot observe the full desktop, read the
accessibility tree, or later **drive** the desktop to execute the workflow.
Only a local cua-driver can. So the production home is the **desktop app** the
clean-start spec already plans (Wails/Electron).

But cua-driver is a **standalone local install** (a `curl` one-liner) reachable
over MCP — it does **not** itself require Electron. So we split the work:

- **Phase A — browser-first (now).** Real `getDisplayMedia` screen share + real
  WebRTC voice (`gpt-realtime`) + screen-frame vision, all in the operator web
  app. This is a real call, testable the moment a key is in Settings, and the
  exact FE logic ports into an Electron renderer unchanged.
- **Phase B — cua-driver (next).** Replace/augment browser capture with
  cua-driver native observation over MCP (full desktop + a11y tree), and **reuse
  cua-driver as the deterministic execute layer** (the spec's "bounded CUA-heal"
  on EXECUTE). Package in the desktop shell so install + screen-recording
  permission are one-click.

Electron earns its place at Phase B (packaging + OS-level capture + bundling the
cua-driver), not before.

## 3. Architecture (Phase A)

```
┌─ Operator FE (browser, ports to Electron renderer) ───────────────────┐
│  RealCallModal                                                        │
│   • getDisplayMedia() → live screen preview + frame sampler           │
│   • RTCPeerConnection ⇄ OpenAI Realtime (mic up, voice down, data ch) │
│   • sends sampled frames as input_image; renders live transcript      │
│   • model calls draft_workflow(tool) → DemoCapture → build engine     │
└───────────────┬───────────────────────────────────────────────────────┘
                │ 1. POST /realtime/session  (broker mints ephemeral key)
                │ 2. SDP offer/answer + media/data  ⇄  OpenAI directly
┌───────────────▼───────────────────────────────────────────────────────┐
│  Broker  /realtime/session                                            │
│   ResolveOpenAIAPIKey() (Settings/env, NEVER sent to the browser)     │
│   → POST OpenAI client_secrets → { ephemeralKey, model, sdpUrl }      │
└───────────────────────────────────────────────────────────────────────┘
```

- **The real key never reaches the browser.** The broker mints a short-lived
  **ephemeral** Realtime token from the operator's key; the browser does the
  WebRTC handshake with the ephemeral token only.
- **Model + endpoints are config**, not hardcoded: `realtime_model` (default
  `gpt-realtime`), the mint URL, and the SDP URL are env/config-overridable so
  the exact model string and API surface can change without a code change.
- **Vision**: frames are sampled from the screen-share `MediaStream` every few
  seconds and pushed as `input_image` conversation items, so Nex sees the screen
  while it talks. (Phase B swaps this for cua-driver screenshots + a11y tree.)
- **Handoff**: Nex is given one tool, `draft_workflow`, whose arguments map onto
  the existing `DemoCapture` shape. On call, the FE runs the same
  `capturePromptSeed` → `WorkflowBuilder` handoff the mock already uses.

## 4. Key handling (Settings + onboarding)

The Settings "OpenAI Realtime key" field (already present, mock) is wired to
persist `openai_api_key` (+ `realtime_model`) through the existing broker config
endpoint; onboarding gains the same optional paste step. Resolution stays
`WUPHF_OPENAI_API_KEY` env > `OPENAI_API_KEY` env > config file
(`ResolveOpenAIAPIKey`). With no key, the call falls back to the mock.

We never enter the key for the user; they paste it into Settings/onboarding and
the broker stores it (config file, same as the other provider secrets).

## 5. Security

- Ephemeral-token mint server-side; the long-lived key never crosses to the
  browser, logs, or URLs.
- `/realtime/session` is auth-gated like every other broker route
  (`requireAuth`).
- Frames and transcript stream browser ⇄ OpenAI directly over the WebRTC peer
  connection; the broker is only in the token-mint path.
- Phase B (cua-driver) runs on the operator's own machine; screen-recording and
  accessibility permission are explicit OS grants, surfaced in onboarding.

## 6. Slice plan

- **A1 — token endpoint.** `/realtime/session` mints the ephemeral key; config
  `realtime_model`; unit-tested with an injected HTTP doer. *(this PR)*
- **A2 — real call FE.** `realtimeClient.ts` + `RealCallModal.tsx`; screen +
  voice + frames + `draft_workflow` → existing handoff; mock fallback. *(this PR)*
- **A3 — Settings/onboarding key.** Persist + status; gate real vs mock. *(this PR)*
- **B1 — cua-driver capture.** MCP bridge to a local cua-driver for desktop
  observation; replaces frame sampling.
- **B2 — cua-driver execute.** Deterministic replay/heal of the compiled
  workflow via cua-driver.
- **B3 — desktop shell.** Electron/Wails packaging that bundles cua-driver and
  manages permissions.

## 7. Open questions

1. Exact GA Realtime model string + WebRTC SDP URL — kept configurable so a
   mismatch is a settings change, not a code change.
2. wuphf-hosted voice (metered proxy) vs BYOK only — BYOK first; hosted is a
   later broker proxy mode.
3. cua-driver licensing/bundling for the desktop shell (Phase B).
