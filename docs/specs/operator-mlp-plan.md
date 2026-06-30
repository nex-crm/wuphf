# Operator MLP — Build Plan

**Companion to:** `operator-mlp-one-pager.md` · **Branch:** `pivot/operator-mlp` · **Date:** 2026-06-25

Locked decisions (from the one-pager): wedge = **inbound routing + scoring**; **demo bar =
the magical screen-share + free-voice call** (CUA in the **Wails desktop app**); context =
**replace our engine with gbrain**; **keep & build on** the workflow engine, **workflow
detection**, and the **app builder**; data layer = **deferred to v1.1**; strategy = **simplify
in place**; object = **Internal Tool (UI · Workflow tabs)**.

Guiding rules: **(1) delete the office cruft and stay light** — but keep & build on what
genuinely fits (engine, detection, app builder). **(2) Nothing demos until the magical call
works.** The build sequence below front-loads everything that call depends on.

---

## Acceptance scenario (the spec to build against)

**Maya, RevOps at a 60-person SaaS co.** Inbound demo requests land in a shared inbox /
form. Today she manually reads each, checks the company in her CRM, scores fit, and
routes hot ones to an AE in Slack — 90 minutes a day, and it stops when she's out.

The loop we must deliver:
1. Maya opens a screen-share call, narrates "here's how I triage these," clicking through
   her inbox and CRM while a voice agent asks "what makes one hot?"
2. An **Internal Tool** appears: a **UI** tab (table of incoming requests + fit score +
   status) and a **Workflow** tab (trigger on new request → enrich from CRM → AI fit-score →
   if score ≥ threshold route to AE Slack, else nurture).
3. Maya runs it on 10 test requests, watches scores and routing fill in, tweaks the
   threshold in chat, republishes.
4. She publishes. Next morning she gets a **Slack digest**: "14 requests processed, 5 routed
   hot, 1 needs you — couldn't find the company in CRM."
5. She replies how to handle the edge case; the AI folds it into the workflow and shows the
   change in the Workflow tab's history.

Secondary ICPs to keep honest (not built first): a CS ops manager auto-triaging support
escalations; a finance ops analyst routing/approving expense exceptions. Same loop shape.

---

## Workstreams

- **WS-A — The deterministic loop** (engine → Internal Tool → run → digest → improve). The
  core value; what the call *produces*.
- **WS-B — The magical call** (Electron renderer + screen-share + CUA/CDP capture + free-voice
  → workflow draft). The demo gate. Built in parallel; the loop is its target.
- **WS-C — Knowledge brain (gbrain)** + **detection**. The substrate and the passive authoring
  path. gbrain owns the whole Knowledge backend (MCP: search/query/put_page); the karpathy-wiki
  reader is the human Knowledge surface over it.

WS-A and WS-C give the call something to build into; WS-B is the experience. **No external
demo until WS-B works** — but WS-A/WS-C are sequenced first so the call has a target.

---

## Phase 0 — Strip to the studs + stand up gbrain

**Goal:** a lighter codebase, the context substrate swapped, a thin operator shell — before
feature work.

1. **Write the keep-list.** Audit each subsystem against "does an inbound-routing operator
   need this?" **Keep & build on:** workflow engine (`internal/workflowpress`,
   `internal/workflow`), **workflow detection** (`workflow_detect.go` — repoint later, don't
   delete), **app builder** (lift intact, Spike 6), **the karpathy-wiki reader + design + IA**
   (the Knowledge surface, Spike 7 — keep S6, retire the S1-S5 Go compile backend → gbrain),
   integration bridge + approval + audit, the message/run/integration bus.
2. **Delete the office cruft** (engine + UI, not feature-flag): notebooks, skills (`skill_*`,
   SkillsApp), policies, promotion/review queue, channels/DMs surface,
   lifecycle/inbox-as-decision-queue UI, multi-agent roster + agent subspaces, company/team
   onboarding wizard. (The old messy wiki authoring/notebooks/promotion is already removed on
   `feat/karpathy-wiki`; the clean Knowledge reader stays.)
3. **gbrain for it all (Knowledge + context).** Stand up gbrain as an **MCP server** the broker
   connects to (teammcp already speaks MCP); it owns the entire Knowledge/context backend
   (sources, compile, index, graph, retrieval, self-maintenance). Migrate retrieval off
   `internal/embedding` + `wiki_index_*` + `broker_entity_graph`/`facts` + Nex graph, AND retire
   the karpathy-wiki S1-S5 Go compile backend. Wire office activity → gbrain `put_page`/ingestion;
   brain-first `search`/`query` in workflow AI-steps + the build agent. (Confirm gbrain covers our
   retrieval + page shapes before deleting the old paths — Spike 1 proved retrieval.)
4. **Collapse to one assistant persona.** One "your AI" identity backed by the provider layer.
5. **Stand up the thin operator shell** (stub OK): **Chats · Internal Tools · Knowledge ·
   Integrations · Settings**. Fresh refine-based surface; the Knowledge tab renders the
   karpathy-wiki reader over gbrain; route the old app's bundle out.
6. **Decide app-builder reuse depth** (Spike 6 says lift intact): minimal repoint (structural
   singleton + human-gated proposal fit one operator; livestream-narration persona fits the call).

**Exit:** Go + web build green; suites green for what remains; shell shows the four surfaces;
gbrain serves context for a smoke query; grep confirms wiki-product/skills/notebook/roster are
gone, not hidden; detection + app builder + engine still compile; LOC down.

---

## Phase 1 — The deterministic loop, hand-authored (WS-A)

**Goal:** prove build → run → digest for one inbound-routing tool, spec written by hand
(no capture, no chat-authoring yet). De-risks the engine↔UI↔integration wiring.

1. **Internal Tool object** = workflow spec + the app-builder UI + a Data store, persisted
   together. **Three tabs** (eng-review #6 pulled Data into the MLP): **UI** (app-builder lifted
   intact, Spike 6 — refine+Mantine, build-with-you-via-chat, live preview, select-to-edit, build
   gate, version snapshots; reads via Bridge v2), **Workflow** (the spec + live run view, our
   engine), and **Data** (operator-owned typed tables for run-state/entities — request rows with
   mutable status across runs; distinct from gbrain). The UI table + digest project over run
   records + the Data store.
2. **Author one routing spec by hand** against the kernel: trigger (new inbound, manual
   test-fire) → enrich (integration read) → `ActionLLM` fit-score → guard (score ≥ threshold)
   → route (gated external write to Slack) / else nurture.
3. **Run on test data**: fire 10 fixtures, surface transitions + actions + AI outputs in the
   UI, gate the external write through the existing approval card.
4. **Daily digest**: scheduled job summarizes the run log → email/Slack. Reuse scheduler +
   notify path.
5. **Run audit + version history** surfaced read-only in the Workflow tab (from the existing
   run JSONL + shipcheck).

**Exit:** Maya-scenario steps 2–4 work end-to-end with a hand-written spec and test data,
including the morning digest.

---

## Phase 2 — Operator authoring via chat (WS-A)

**Goal:** the operator builds the tool from a chat conversation; no hand-editing.

1. **Build chat** → AI drafts the workflow spec + UI from a described process; compiles via
   the kernel; shipcheck must pass before publish (reuse the build gate).
2. **Chat-to-edit** on a live tool: rebuild the edit panel against the Internal Tool object
   (not the App-Builder-agent flow). "Lower the threshold to 70," "also notify me on misses."
3. **Publish** = freeze + version. Edits after publish = overlays (leaf) or refreeze
   (structural), version+1, visible in history.
4. **Detection as a third authoring path** (repointed): the detector watches the operator's
   activity + context changes + integration events and surfaces "I noticed you do this — want
   a tool?" The wire bridge already exists (Spike 6): `propose_app(fingerprint, observed_steps)`
   → human-gated approval → build. The proposal drops into the same build chat. Passive
   complement to the call.

**Exit:** Maya builds the inbound-routing tool from chat alone, iterates, publishes — no
developer in the loop. Detection proposes at least one real candidate from observed activity.

---

## Phase 3 — The magical call (WS-B, the demo gate; build in parallel from Phase 0)

**Goal:** real screen-share + free back-and-forth voice with your AI, building a tool before
the operator's eyes. **This is the demo bar — nothing ships externally until it works.**

1. **Electron desktop host.** The shipping shell is Electron (`apps/desktop`, `@wuphf/desktop`),
   which packages our dmg/exe/AppImage via electron-builder. Add screen capture
   (`desktopCapturer`/`getDisplayMedia`) + audio in the renderer; host the WebRTC Realtime
   voice session there. (Wails/oswails stays for OS verbs only.) Measured: voice round-trip
   620ms; full-frame vision 2.2s → downscale + throttle frames, attach only on screen reference.
2. **Two capture layers, fused** — vision alone can't build a replayable workflow:
   - **Voice + vision** (the spike): the conversation + rough context. Screen frames as
     `input_image` so the AI can talk about what it sees.
   - **Structured observation (CUA + CDP)** — the replayable trace. Per operator action,
     capture `{app/browser, real URL, element selector + accessible name, input value
     (PII-redacted), and the network request the action fired}`. Sources: `trycua/cua`
     gives `get_accessibility_tree` (cross-app element tree), `get_current_url`,
     `get_active_title`/`get_application_windows` (which browser/app) — verified in its API;
     **add CDP Network capture** for the network-request signal (cua's gap), since the API
     behind a click is the strongest deterministic step (replay the API, don't puppet the UI).
3. **Realtime free-voice agent** (OpenAI Realtime): genuine back-and-forth — the operator
   talks naturally, the AI asks "what makes one hot? where does it go then?" Latency and
   turn-taking are the experience; proven ~620ms server-side (Spike 2).
4. **Capture → workflow draft**: the CDP network trace is a HAR; run it through **lifted
   `browsersniff`** (from `cli-printing-press`, MIT Go — proven in Spike 4) to synthesize the
   app's private-API spec (endpoints, params, auth). Fuse that + narration (why) + selectors
   (UI-replay fallback) + vision (context) + gbrain context into a `workflow-spec.json` draft
   that drops into the Phase-2 build chat. Capture proposes; the operator confirms; the engine
   compiles. No silent auto-build.
   **Build-vs-lift (Spike 4):** lift exactly `browsersniff`; our workflow engine, read/mutate
   gating, shipcheck/overlay self-heal, detection, and app-builder UI all stay and beat
   printing-press's equivalents.

   **Observation layer (refined by Spike 5):**
   - **Browser path (most operator work):** CDP-attach to the operator's **real, logged-in
     Chrome** (browser-harness's proven technique), in Go via **chromedp** → capture the Network
     domain (HAR) + DOM selectors → `browsersniff` → spec. Sees their real authenticated
     sessions, no re-auth, no embedded webview.
   - **Native-app path:** cua / OS accessibility (`get_accessibility_tree` + URL + window) for
     non-browser desktop apps.
   - **Decided:** the build-phase **agentic explorer = Go/chromedp wired into our broker agent
     loop** (borrow browser-harness's techniques, build in-stack; no Python sidecar). Lift one Go
     package (`browsersniff`). Agentic at build, deterministic at execute (see one-pager §4).
     Open: cua passive-observe-real-machine validation + the macOS/Windows accessibility +
     Chrome 144+ per-attach permission flows.

**Exit (and the go/no-go demo gate):** Maya gets on a call, talks through her triage while
clicking her screen, and watches the inbound-routing tool take shape — then refines and ships
it. **Risk note:** longest, least-certain pole (CUA maturity, voice latency/cost, fusing
observation+narration into a clean spec). WS-A/WS-C still build first so the call has a target,
but the *product* is not demoable until this lands.

---

## Phase 4 — Proactive improvement

**Goal:** business-context change → suggested workflow change with reasoning → operator
approves → folded in and visible.

1. Wire improvement signals (recurring exception, SLA miss, operator-edit, context change)
   to an operator-facing **"suggested change + why"** card.
2. Approve → overlay/refreeze, version+1; reject → dismissed; edit → operator amends.
3. The clarification reply from the digest (Maya's CRM-miss edge case) flows through this
   same path and shows in history.

**Exit:** a context change produces a suggestion Maya approves and sees in the workflow's
history.

---

## Cross-cutting

- **Capture discovers; Composio builds; custom is the fallback.** The session capture's job is
  **discovery** — which apps the operator uses, the workflow shape, and the decision logic. It does
  NOT itself become the execution path. Once capture reveals the apps (e.g. "this happens in
  HubSpot"), the workflow's actions are built **Composio-first**.
- **Execution model — deterministic, incl. computer control. "Integrations over session
  hijack."** Source hierarchy per action, most-robust first:
  1. **Composio integration (preferred).** When capture identifies the app, **prompt the operator
     to connect it (OAuth)** and build/run the action through Composio's tools — permanent,
     refreshable creds, well-formed APIs (also shrinks the operator-can't-validate risk, A4/#3),
     reusing Bridge v2 + the deterministic-integrations connect flow. Composio is what we reach for
     first when building out the discovered workflow.
  2. **Sniffed private API** (browsersniff) — only when Composio doesn't cover the app. Auth is
     **classified** (A4): stable key → stored credential; rotating session → flagged "needs a live
     session", not a dead-token replay.
  3. **Deterministic UI replay** of captured selectors/inputs via the cua-driver — no usable API.
  4. **CUA heal** (bounded re-find) when replay breaks → write the new target back as an **overlay
     (version+1)**, reusing the engine's self-healing path.
  CUA is fallback + healing under the deterministic engine, never a free-roaming agent. **CUA is
  dual-use: observe at capture (Phase 3), control at execution here.** Mutating actions at BOTH
  build-explore and execution hit the shared approval card (CQ1). The inbound-routing wedge is
  mostly Composio-covered (CRM + Slack), so it proves the integration path first; sniffed-API +
  UI-replay + CUA-heal are the fallback layers for apps Composio doesn't reach.
- **Notification settings** (email/Slack, digest cadence, approval routing) in Settings.
- **Approvals** reuse the existing gate, reskinned operator-plain (no agent vocabulary).
- **No new top-level concepts** surface to the operator beyond Chats / Internal Tools /
  Integrations / Settings.

---

## Sequencing & demo cadence

```
Phase 0 (strip + gbrain) ──┬─► Phase 1 (loop, hand-authored) ─► Phase 2 (chat + detection) ─► Phase 4 (improve)
                           └─► Phase 3 (the magical call) ──────────────────► [DEMO GATE] ◄─┘ (lands into P2)
```

Internal builds land continuously, but **the first external demo is gated on Phase 3 — the
magical call.** WS-A/WS-C (Phases 0–2) are sequenced first only so the call has a real tool to
build into; they are not the demo. Internal milestone (team-only): end of Phase 1, a tool runs
on test data + digest. **External "this is the product" demo: Phase 3 working.**

## Open questions to resolve during Phase 0

- **gbrain fit:** does it cover our retrieval shapes (semantic + entity/fact lookups) before we
  delete the old indexes? API, embedding model, hosting. Migration path off the four current stores.
- **Wails screen/audio capture:** can the existing desktop app host CUA + low-latency voice, or
  does it need a capture sidecar? Spike inside the current Electron app (`apps/desktop`).
- **`trycua/cua` fit + licensing**; fallback observer if it doesn't hold.
- **Realtime voice** (OpenAI Realtime): where it runs, latency/cost, turn-taking, how it hands
  structured output back. This is the make-or-break of the demo.
- **App-builder reuse depth:** build on top vs. rewrite read-mostly/roster-agent parts.
- **Detection input:** which operator/context signals replace agent telemetry as the source.
- Whether one inbound-routing spec generalizes to the secondary ICPs or each needs its own
  shape (resist generalizing in v1).

---

## Eng-review hardening (2026-06-26) — folded decisions

- **A1** gbrain swap: add a **Phase-0 verification gate** — parallel-run the karpathy-wiki compile
  backend vs gbrain, prove gbrain produces reader-compatible entity/concept/process pages + meets
  retrieval/lint bar, THEN retire the old backend. One-way door made reversible.
- **A2** gbrain hot-path: all broker→gbrain MCP calls **off-lock** (prepareLocked → release → I/O →
  re-acquire, per the `writeSkillProposalLocked-deadlock` learning); gbrain unreachable **degrades
  gracefully** (AI-step proceeds sans brain context, Knowledge shows "offline", capture queues) —
  never deadlock, never hard-fail a run.
- **A3** capture HAR: **scope to active tab + strip secrets (reuse piiplaceholders) + ephemeral**
  (in-mem/short-TTL); auth recorded as a typed credential ref, never the value on disk.
- **A4 / "Integrations over session hijack"** capture **discovers**, Composio **builds** (preferred:
  prompt to connect the discovered app), browser/session + custom APIs are the **fallback**. Sniffed
  auth classified: stable key stored, rotating session flagged "needs a live session".
- **CQ1** build-phase agentic explorer is **observe/reason-freely, gated-to-act** — mutating actions
  during build hit the SAME human approval card as execution.
- **#6** **Data tab pulled into the MLP** — operator-owned typed tables for per-tool run-state/entities
  (request rows with mutable status); UI table + digest project over run records + the Data store.

## CEO-review decisions (2026-06-26)

- **Activation (#1/#2):** the **call is the primary activation + ship gate** (the magic; chat is
  post-call iteration). Founder decision; concentrated-risk bet (MLP rides on the call working +
  being magical + converting a chat-native audience) is **named and owned**.
- **Voice economics (#5):** **BYO OpenAI Realtime key** (Settings) **or Nex-cloud hosted/metered**;
  **no key → call is optional, chat-authoring is the keyless entry** (graceful degradation; OSS tool
  stays usable). Chat-authoring is therefore the no-key floor, also covered in Phase 2.
- **Unattended (#3):** Composio path runs unattended (server-side); sniffed/UI-replay fallback is
  operator-present/manual. Documented honestly, not pretended.
- **gbrain packaging (#4):** bundled subprocess the Electron app spawns (like the broker), PGLite
  embedded, **local ollama embeddings default** ($0, self-hosted-consistent), BYO cloud-embedding-key
  optional. Phase-0 task.
- **Exec-CUA risk (#9):** accepted as a known unvalidated risk — now fallback-of-fallback after
  Composio-first + UI-replay; spike it during implementation (semantic API changes = re-discovery,
  not bounded re-find).

## GSTACK REVIEW REPORT

| Review | Trigger | Why | Runs | Status | Findings |
|--------|---------|-----|------|--------|----------|
| CEO Review | `/plan-ceo-review` | Scope & strategy | 1 | decided | 5 strategic items resolved (activation, voice $, unattended, gbrain packaging, exec-CUA accepted-risk) |
| Codex Review | `/codex review` | Independent 2nd opinion | 0 | — | — |
| Eng Review | `/plan-eng-review` | Architecture & tests (required) | 1 | folded | 6 findings (A1-A4, CQ1, #6) folded; 5 critical tests required at implementation |
| Design Review | `/plan-design-review` | UI/UX gaps | 0 | — | — |
| DX Review | `/plan-devex-review` | Developer experience gaps | 0 | — | — |

- **CROSS-MODEL:** outside voice (Claude subagent; Codex auth expired) found the section review's blind spot — wedge↔tech coherence. Resolved: capture = discovery, Composio = preferred build surface, browser/custom = fallback. CEO review then settled the 5 strategic items it surfaced.
- **VERDICT:** CEO + ENG CLEARED — strategy decided, 6 eng findings folded. Ready to implement the first vertical slice; the 5 critical tests (A1-A4, CQ1) must land with it. The call-as-ship-gate is an accepted, named concentrated-risk bet.

NO UNRESOLVED DECISIONS
