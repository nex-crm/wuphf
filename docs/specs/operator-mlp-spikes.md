# Operator MLP — Spike Results

**Companion to:** `operator-mlp-plan.md` · **Branch:** `pivot/operator-mlp` · **Date:** 2026-06-26

Two gates carried almost all the risk in the plan. This records what was proven.

---

## Spike 1 — gbrain covers our retrieval shapes · ✅ PROVEN (fully offline)

**Question:** before we delete four bespoke context stores (`internal/embedding`,
`wiki_index_bleve/sqlite`, `broker_entity_graph`/`facts`, Nex graph), does gbrain actually
serve the retrieval our workflows/tools need?

**What was run** (real, not a brief):
- Installed gbrain `0.42.53.0` from source (`bun install && bun link`).
- Stood up an **isolated** brain (`GBRAIN_HOME` in scratch — no real brain touched) on
  **PGLite** (embedded Postgres, no Docker) with **ollama `nomic-embed-text` embeddings**
  (768d) — **no cloud API key**. Embedding probe: green, 33ms, 768 dims.
- Imported a 5-doc inbound-routing corpus modeling real operator context: the triage
  process, ICP scoring rules, AE routing map, CRM account notes, and a June run-decisions
  log with edge cases.
- Fired the queries the three real consumers issue: the workflow AI scoring step, the
  capture-call clarifier, and detection.

**Result — 5/5 queries top-ranked the correct document:**

| Query (consumer) | Top hit (score) | Correct? |
|---|---|---|
| score an inbound request for fit (AI step) | `inbound-triage-process` (0.93) | ✅ |
| which AE owns enterprise fintech (routing / entity lookup) | `ae-routing-map` → Dana Okafor (0.89) | ✅ |
| what fit score routes to an AE (guard threshold) | `icp-scoring-rules` → ">= 70" (0.91) | ✅ |
| what to do when company not in CRM (edge-case / learning) | `run-decisions-2026-06` (1.34) | ✅ |
| do we already have a routing process (create-safety / detection) | routing + process docs | ✅ |

**Conclusion:** gbrain serves all four retrieval shapes we need — semantic process, entity/
fact, learning recall, existence-check — and runs **fully local** (PGLite + ollama, sub-second,
$0). The hybrid stack (pgvector HNSW + BM25 + RRF + reranker + per-query graph signals)
matches our needs, and graph signals cover the entity-adjacency shape the old `entity_graph`
gave us. **Decision stands: replace the context engine with gbrain.**

**Cost/dependency notes for the real build:**
- Production embeddings: pick a provider. Local (ollama) = $0 but lower quality; hosted
  (Voyage ~$0.18/1M, ZeroEntropy ~$0.05/1M, OpenAI ~$0.13/1M) for quality. Switching the
  embedding model invalidates the vector index (re-embed) — pick once, deliberately.
- Storage plane: PGLite for single-operator local; Postgres+pgvector for scale.
- gbrain is a mature TS/Bun CLI with a contract-first op surface — integrate as a sidecar/
  CLI the broker shells out to, or via its programmatic API. Confirm the integration seam
  in implementation.

**Repro:** corpus + commands in scratch; `gbrain init --pglite --embedding-model
ollama:nomic-embed-text --embedding-dimensions 768`, `gbrain import <dir>`, `gbrain search`.

---

## Spike 2 — the magical call: low-latency voice + screen comprehension · ◐ MOSTLY PROVEN

**Question:** can the magical call — screen-share + free back-and-forth voice — feel like a
real conversation? Make-or-break = end-to-end turn latency.

**Correction to an earlier wrong claim.** An earlier draft said "no Wails desktop exists." That
was wrong. Desktop facts as they actually are:
- The shipping desktop shell is **Electron** (`apps/desktop`, `@wuphf/desktop`, Electron 42 +
  electron-vite) — the "WUPHF v1 desktop shell," an OS-level security boundary spawning the
  broker over loopback.
- **dmg / exe / AppImage are packaged by electron-builder** (`apps/installer-stub/
  electron-builder.yml`; pipeline on branch `feat/installer-pipeline`). Build today:
  `cd apps/desktop && bun run build` → `out/{main,preload,renderer}`; installer via the
  installer pipeline.
- **Wails is a reserved boundary**, not the shell: `desktop/oswails/` is the only Go package
  allowed to import Wails, scoped to OS verbs (notifications, tray, dock badge, deep-link,
  autostart, file pickers, single-instance). Currently a stub in the main tree.

**Implication (better than the original plan):** the call rides the **Electron renderer**, not
Wails. Chromium gives first-class screen capture (`desktopCapturer` / `getDisplayMedia`) and
can host the WebRTC Realtime voice session directly. Wails/oswails stays for thin OS verbs.
"CUA on Wails" in the plan should read **"capture + voice in the Electron renderer."**

**Latency — measured for real against the GA `/v1/realtime` API** (key provided; OpenAI
Realtime, model `gpt-realtime`). Note: the legacy Beta Realtime shape is retired — must use
the GA `/v1/realtime` event schema (`response.output_audio.delta` etc., no `OpenAI-Beta` header).

| Turn type | Method | First audio back | Full spoken response |
|---|---|---|---|
| Voice (text-in proxy) | text → audio | **410ms** | 1.66s |
| **Voice (real audio-in → audio-out)** | TTS-synthesized speech → audio, 5 turns | **620ms median** (550–803) | 2.3s median |
| Screen + voice (full-res frame) | 2940×1912 screenshot + question → audio | **2196ms** | 3.5s |

The model correctly identified the captured screen ("a code editor, specifically something
like Visual Studio Code, with a terminal running").

**What the numbers say:**
- **Voice round-trip is conversational.** 620ms speech-to-speech (incl. input transcription)
  is inside the sub-800ms bar. The make-or-break unknown passes.
- **Naive full-frame screen-share is too slow** (2.2s). Architecture finding: do **not** send
  full high-res frames every turn. Downscale + throttle frames, and only attach a frame when
  the operator references the screen. Continuous screen-share is a frame-strategy problem, not
  a voice-latency problem.

**Still genuinely needs a human + the Electron shell (can't be faked autonomously):**
- **Live feel** — a person actually speaking and judging barge-in / turn-taking naturalness.
  We can measure ms; "feels alive" is a human call at a real mic.
- **Server-VAD endpointing** adds latency on top of the 620ms (default ~500ms; `semantic_vad`
  is faster). Real perceived turn = 620ms + endpointing, tunable.
- **Continuous screen video** in the actual Electron renderer (vs. one screenshot here).

**Status:** voice latency **proven good**; screen comprehension **works but needs a frame
strategy**; live human feel + in-app integration remain. The demo gate is no longer a black
box — the riskiest number (voice round-trip) is green.

**Repro:** harnesses in scratch (`realtime-audio.ts` audio-in→out, `realtime-vision.ts`
screen+voice) against GA `/v1/realtime`.

### Runnable spike built — `apps/desktop/spikes/cua-call/`

An isolated Electron app (NOT the sealed shell) that does the real thing end-to-end:
WebRTC two-way voice + throttled, downscaled screen frames in **one** Realtime session
(no separate computer-use/CUA model — vision is native to the session). Token minting
stays in the main process; the renderer only gets an ephemeral secret. Run:
`cd apps/desktop/spikes/cua-call && export OPENAI_API_KEY=… && bun install && bun start`,
then Start call, grant mic + screen recording, and talk. A "turn metrics" panel shows
speech-stop → AI-audio per turn so the live feel is measurable, not just felt.

**Architecture decision recorded:** the call rides the **Electron renderer**; CUA
(computer-use, screen *control*) is NOT needed for capture (observe-only). Production keeps
`apps/desktop` sealed by routing the session through the Go broker over loopback.
JS verified to parse; full run needs a display + mic + screen-recording grant + a key
(human-in-the-loop).

---

## Spike 3 — does CUA give STRUCTURED capture (selectors/URLs/network)? · ◐ MOSTLY YES

**Question:** vision (screenshots) lets the AI *see* the screen but can't build a *replayable*
workflow — no real URL, no selector, no idea what actually happened on click. Does CUA
(`trycua/cua`) provide the structured observation a deterministic workflow needs?

**What was checked:** cloned `trycua/cua`, inspected its Computer interface + drivers.

**Found — cua exposes the structured signals, verified in its API:**
- `get_accessibility_tree` — cross-app element tree (roles, names → selectors).
- `get_current_url` — the real URL.
- `get_active_title` / `get_application_windows` / `get_window_title` — which app/**browser**.
- First-class browser drivers (Chrome/Safari/WebKit/Electron tests, browser-JS execution) →
  DOM selectors reachable. Plus `screenshot`/`click`/`type` (the pixel layer).

**Two gaps / caveats:**
- **No network capture** in the method surface (a11y + URL + window yes; network/HAR no). The
  network request behind a click is the strongest deterministic signal — add **CDP Network**
  capture (browser-side) on top. This is the one piece CUA doesn't hand us.
- **cua is sandbox/VM-first** (lume, cua-sandbox) — designed for the AI to *control* a
  computer in a sandbox. Capture needs the inverse: *passively observe the user's real
  browser/apps*. cua-driver drives local browsers, so the path exists, but "observe real
  machine, don't control sandbox" + the OS accessibility-permission flow must be validated.

**Decision implied:** capture = **voice + vision (conversation) FUSED with structured
observation (replayable trace)**. CUA supplies a11y + URL + app/window; add CDP for browser
selectors + network. The structured trace — not the pixels — compiles into `workflow-spec.json`.
Open: cua-driver vs own CDP/extension vs both; real-machine passive observation. (See plan
Phase 3 "Open decisions for the observation layer.")

---

## Spike 4 — capture → sniff → deterministic replay, end-to-end · ✅ PROVEN (with real liftable code)

**Question:** can we capture an operator action in a no-API app and turn it into a
deterministic, replayable workflow step? And is `cli-printing-press`'s `browsersniff` the
right thing to lift for the "discover the private API" piece?

**What was run** (real, end-to-end):
1. **Capture** — a stand-in "no-API CRM" (a lead + "Route to AE" button whose click fires
   `POST /api/leads/L-1042/assign` with a Bearer header + JSON body). Playwright drove system
   Chrome and captured: the clicked **selector** (`#routeBtn`, fallback `[data-action=route-to-ae]`),
   the exact **network request**, and a standard **HAR**.
2. **Sniff → spec** — fed that HAR through `cli-printing-press`'s **actual `browsersniff` Go
   code** (MIT). It synthesized a `spec.APISpec`: `base_url`, `spec_source: sniffed`, the
   **`POST .../assign` endpoint** with body params (`assignee`, `reason`), and **detected the
   Bearer auth** → a sensitive per-call env var. (Honest limit: with one sample it kept the
   literal `L-1042` instead of `/{id}`; more samples parameterize it — by design.)
3. **Replay** — reproduced the action two ways against the live server:
   - **API-first**: re-fired the sniffed request with the credential injected at runtime
     (env, not stored) → assignment reproduced (✓).
   - **UI-replay fallback**: re-clicked the captured selector in Chrome → assignment
     reproduced (✓). Server logged 3 identical assignments (capture + API replay + UI replay).

**Conclusion:** the full no-API → deterministic-action pipeline works with real, liftable
code. **Lift `browsersniff` (HAR → spec) into the Go broker** — it's the one piece we lack.

### Build-vs-lift verdict (us vs printing-press)

| Capability | printing-press | us | verdict |
|---|---|---|---|
| API discovery from traffic (HAR → spec) | ✅ `browsersniff` (MIT, deterministic) | ✗ Composio official specs only | **LIFT theirs** |
| Deterministic multi-step engine (states/guards/branching/durability/audit) | ✗ flat per-API clients | ✅ `internal/workflow` (+ Inngest) | **keep ours** |
| Read/mutate classification + masked approval gate | "safety class" (weak) | ✅ server-side, app-can't-lie | **keep ours** |
| Verify / self-heal | scorecard heuristics | ✅ shipcheck proofs + overlay propose | **keep ours** |
| Workflow detection (mine recurring shapes) | ✗ none | ✅ `workflow_detect.go` | **keep ours** |
| Operator UI generation | ✗ CLI/MCP only | ✅ app-builder (refine/Mantine) | **keep ours** |

**Fused pipeline:** capture (CDP/HAR + selectors) → **browsersniff (lifted)** → spec →
register as a workflow action → **our engine** orchestrates (guards/gating/durability/audit) →
**our app-builder** UI + **our detection** + **our overlay self-heal**. We lift exactly one
package; everything else we already have and ours is stronger.

**Repro:** `scratchpad/capreplay/` (`fakecrm.mjs`, `capture.mjs`, `replay.mjs`) +
`cli-printing-press/cmd/sniffdemo/main.go`.

---

## Spike 5 — browser-use/browser-harness: lift the technique, not the code · ◐ TAKE THE PATTERN

**Question:** browser-harness is "good at API detection / building from a browser perspective."
Should we lift something?

**What it is:** a thin (~1k LOC, 4 files), **MIT, Python** harness that attaches an LLM to your
**real, already-logged-in Chrome** over one raw CDP websocket (`chrome://inspect`, Chrome 144+
per-attach allow popup). It's an **agentic** harness — the LLM drives the browser with "complete
freedom" and writes its own helper functions + per-site `domain-skills` as it goes; "the harness
improves itself every run." CDP primitives in `helpers.py`: `js()` (Runtime.evaluate),
`wait_for_network_idle` (drains `Network.requestWillBeSent`/`loadingFinished`), input dispatch,
screenshots, `http_get`.

**Findings vs our needs:**
- **Don't lift the code.** It's Python (our broker is Go) and it's an *agentic-freedom* harness —
  the opposite of our deterministic goal. Its "API detection" is an agent watching network +
  hand-writing per-site skill notes; it has **no deterministic HAR→spec synthesizer**. For spec
  synthesis, `browsersniff` (Spike 4) is strictly better.
- **Lift the technique:** attach to the operator's **existing, logged-in Chrome** over CDP and
  read the **Network domain** for the live API traffic. This is the precise structured-capture
  substrate — and crucially it sees the operator's **real authenticated sessions** (they're
  already signed into their CRM), with no re-auth and no embedded webview. Better than a separate
  Playwright Chrome (no session) or cua's sandbox-first model for the browser case. We implement
  it in Go via **chromedp** (already a `cli-printing-press` dep), feeding the HAR to `browsersniff`.
- **It reinforces the thesis:** its `world-bank` skill literally says "find the API → call it
  directly, no browser needed." Same API-first deterministic principle.
- The **per-site `domain-skills` accretion** pattern (reusable captured knowledge per app) parallels
  our wiki/learnings + overlay self-heal; ours is more deterministic (spec + overlay vs prose notes).

**Verdict (corrected — agentic freedom is desired at BUILD time):** the principle is *agentic
freedom to figure out the workflow, determinism to execute it*. So browser-harness's agentic
exploration is the **right tool for the build/discover phase**, not a rejected pattern:
- **Build/discover (agentic):** drive the operator's real logged-in Chrome, probe, sniff,
  write app-specific glue, explore the process with the operator. browser-harness is purpose-built
  for this. Its CDP-Network capture during exploration produces the HAR.
- **Compile:** `browsersniff` turns the explored traffic into a deterministic spec; the agreed
  steps/branches/guards + selectors complete it.
- **Execute (deterministic):** our engine runs the compiled spec; agentic freedom is refused here
  (except bounded CUA-heal).

**DECIDED (founder):** the build-phase agentic explorer = **Go/chromedp wired into our existing
broker agent loop**. We borrow browser-harness's techniques (real-browser CDP attach, Network-domain
capture, per-site helper accretion) but build them as browser tools in-stack — no Python sidecar.
The agentic build phase runs under our existing gating + audit; `browsersniff` (Go/chromedp) is the
deterministic synthesizer; our engine is the deterministic executor. One stack, two modes.

---

## Spike 6 — app-builder branch (`feat/apps-build-gate-and-select`): the Internal Tool UI is mostly built · ✅ KEEP/LIFT INTACT

**Question:** trace detection → creation → execution on the final app-building branch; what's
liftable for the Internal Tool's UI tab + build-with-you experience?

**Detection → proposal:** apps originate from `propose_app(name, description, fingerprint?,
observed_steps?)` (agent, human-gated) or explicit `/create-app` (human). **The proposal API
already carries `observed_steps` + `fingerprint`** — the wire bridge from workflow-detection →
app creation exists. (The detector itself lives on the workflow-detection branch, not here.)

**Creation — production-grade, not a POC:** host pre-scaffolds → App Builder implements
refine+Mantine single-file → **build gate** (`bun run verify`) → `register_app(html_path,
source_path)` → **host runs the build server-side** (agent can't paste bundles; host overwrites
protected files: bridge/CSP/config) → validates (sandbox policy + deterministic **build-time
guards**: efficiency, stack conformance, theme depth, card-pile) → version snapshot. Plus **live
preview** (per-app Vite dev server + broker reverse proxy + HMR), **persistent edit chat** bound to
the task channel, **select-to-edit** inspector, and **source introspection** (`get_app` returns
the app's REAL shape so re-edits can't hallucinate).

**Execution:** sealed iframe (`sandbox` + CSP `connect-src 'none'`) + **Bridge v2** —
`callIntegration` (server-side READ/MUTATE classification, mutations → shared approval card,
metered), `ai()` (bounded one-shot, no loops), `createTask` (one gated write), read allowlist.
Apps are stateless + read-mostly by design.

**Lift verdict — keep this intact for the Internal Tool UI tab:**
- The whole **build-with-you-via-chat** experience is real and production-grade: edit chat, live
  hot-reload, select-to-edit, source introspection, build-time guards, server-side build ownership,
  version snapshots. **Lift as-is.**
- The App Builder's **"narrate like a livestream to a human watching"** persona is a *perfect* fit
  for the magical call (the operator IS watching) — keep it, don't strip it.
- **Bridge v2 is complementary to our workflow engine, not overlapping:** the UI tab reads/reasons
  at run time via Bridge v2; the Workflow tab runs the deterministic automation via our engine;
  mutations from both go through the **same** approval card.
- **Minimal repointing:** App-Builder-as-structural-singleton + human-gated proposal already suit a
  single operator. Read-mostly is a design choice, not a blocker.

**So the Internal Tool (UI · Workflow) is mostly already built across two branches:** UI tab +
build-with-you = **app-builder** (lift intact); Workflow tab + execution + detection =
**workflow-detection**; glue = the `propose_app(observed_steps)` API + Bridge v2 + shared approval
card. **Net-new is the magical capture call, `browsersniff`, and fusing both into one
operator-simple Internal Tool surface** — not the UI builder, not the engine.

---

## Spike 7 — Knowledge section: gbrain brain + Karpathy wiki (karpathy-wiki branch) · ✅ LARGELY BUILT

**Why knowledge is first-class:** entities + concepts + processes power workflows day-to-day,
and they're **cross-cutting** — the "AE routing map" / "ICP scoring rules" power *many* workflows
(exactly the corpus proven in Spike 1). So Knowledge is its own operator surface, not workflow-scoped.

**gbrain's company-brain model = a realized Karpathy LLM wiki.** Both are the same three-layer
shape: immutable raw **sources** → LLM-compiled **entity/concept/process pages** → **schema** +
index/log + graph + lint/maintenance. gbrain adds multi-source federation, per-user OAuth scoping,
a graph layer (97.6% recall), and a maintenance daemon (`gbrain doctor`/autopilot). Karpathy adds
the *writing/organizing discipline* that keeps it readable (compile-once, cross-link, flag
contradictions, lint).

**The karpathy-wiki branch (`feat/karpathy-wiki`) already implements the Karpathy wiki, deterministically:**
- **Sources** (S1-S2): immutable hashed `SourceRecord`s, auto-captured from office activity
  (tasks/decisions/chats/docs) — no manual filing.
- **Compile** (S3): extract (LLM per source, cached by content-hash — zero calls on unchanged input)
  → merge (deterministic) → author Wikipedia-shaped articles citing source IDs (`^[source-id]`).
- **Finalize** (S4): interlink (`[[wikilinks]]`), `index.md`, append-only `log.md`, citation
  validation — all pure-Go, idempotent.
- **Maintenance** (S5): `CompileSweep` daemon, cheap when idle.
- **Reader** (S6): full Wikipedia-shaped UI (`ArticleReadView`, infobox, citations, wikilinks,
  backlinks, categories, **lint view**, sources browser) + a polished warm-paper design system.
- **Schema** (`WIKI-SCHEMA.md`, `wiki-wikipedia-ia.md`): entity vs concept pages, facts JSONL,
  categories (many-to-many), deterministic slug rules, rebuild-from-markdown guarantee.
- It **removed** the old notebooks/promotion (Pam-curated) model — the "writing/organizing is bad" surface.

**This IS the Knowledge section.** gbrain has no human reader (CLI/MCP only); the karpathy-wiki
reader + schema + design is exactly the "show knowledge digestibly, monitor + maintain it" surface
the operator needs (incl. the lint view = the "maintain" half).

**DECIDED (founder): gbrain for it all.** gbrain is a mature superset of the karpathy-wiki backend
(federated git-markdown + Postgres/PGLite + embeddings + relationship graph + think/search retrieval
+ MCP + scheduled crons + autopilot self-maintenance + `doctor --remediate` self-heal + 60+ skills).
The karpathy-wiki author confirmed gbrain materially exceeds what that branch built. So:
- **gbrain owns the entire Knowledge backend:** sources, compile/synthesis, index, graph, citations,
  retrieval, and the self-maintenance/lint loop. The karpathy-wiki Go backend (S1 source store, S3
  compile, S4 finalize, S5 sweep) is **retired** in favor of gbrain. Office activity is wired into
  **gbrain ingestion**, not our own source store.
- **We keep ONLY the karpathy-wiki reader (S6) + design + IA** as the human "Knowledge" surface —
  gbrain is CLI/MCP-only with no human reader. The reader points at gbrain's pages.

**Consequences to handle (honest):**
1. **Reader ↔ gbrain page shape:** reconcile the reader's expected IA (entity/concept/process pages,
   frontmatter, categories) with gbrain's page conventions (`_brain-filing-rules.md` + skills). Either
   shape gbrain's output to the reader's IA, or adapt the reader. The integration work.
2. **Compile determinism trade:** our S3/S4 had deterministic, citation-validated, idempotent,
   cheap-when-idle compile; gbrain's is LLM-synthesis (but with citations + incremental sync +
   doctor). Acceptable — the wiki is for human reading + retrieval, NOT deterministic execution; the
   workflow engine's determinism is separate and untouched.
3. **Runtime:** gbrain is a TS/Bun + Postgres/PGLite process the broker uses via CLI/MCP. Consistent —
   gbrain is the *decided* dependency (unlike the build-explorer, where we avoided a NEW foreign runtime).
4. gbrain's full federation/OAuth-scoping is **team-scale** (Nex cloud); the single-operator MLP uses
   gbrain's brain (sources/compile/graph/retrieval/self-heal), not the org-chart layer yet.

**How the broker connects (from gbrain's connect-coding-agent tutorial):** gbrain is an **MCP server**
— local stdio (`gbrain serve` subprocess) or remote HTTP (bearer token). Our broker already speaks MCP
(teammcp), so it connects as an MCP client. Agent tool surface: **`search`** / **`query`** (synthesized
+ citations) for reads, **`put_page`** for writes, **`find_experts`**, `get_brain_identity`, `list_skills`.
Two patterns map exactly to our needs:
- **Brain-first read:** workflow AI-steps, the build/capture agent, and the assistant call `search`/`query`
  *before* answering — this is "knowledge powers workflows day-to-day."
- **Ambient capture (write):** agents `put_page` decisions/entities back; office activity feeds gbrain
  ingestion. Knowledge compounds without manual filing.

**Lift verdict:** **Knowledge backend = gbrain entirely** (MCP: search/query/put_page); **Knowledge
surface = the karpathy-wiki reader + design + IA (lift)**. Net-new is the MCP wiring (broker ↔ gbrain),
office activity → `put_page`/ingestion, pointing the reader at gbrain's pages, and adding Knowledge as the
5th operator surface.

---

## Net

- **gbrain: proven.** Replace the context engine; it covers our shapes and runs local.
- **Capture → deterministic replay: proven end-to-end.** Lift `browsersniff` (HAR→spec) — the
  one gap; our engine + gating + detection + self-heal + UI stay (and beat printing-press).
- **Browser capture substrate:** CDP-attach to the operator's real logged-in Chrome
  (browser-harness's proven technique), in Go (chromedp) → HAR → browsersniff. Take the pattern,
  not the Python/agentic code.
- **The Internal Tool UI is mostly built.** app-builder (`feat/apps-build-gate-and-select`) is
  production-grade — lift the build-with-you-via-chat experience intact for the UI tab; its
  Bridge v2 complements (doesn't overlap) our workflow engine. Net-new is the capture call,
  `browsersniff`, and fusing UI + Workflow into one operator-simple surface.
- **The magical call: the riskiest number is green.** Real speech-to-speech round-trip is
  **620ms** — conversational. Screen comprehension works but needs frame downscaling/throttling
  (full frames = 2.2s). The vehicle is the **Electron renderer** (`apps/desktop`), not Wails —
  Chromium does screen capture + WebRTC voice natively. Remaining: structured observation
  layer (Spike 3), VAD/barge-in, and a human judging the live feel.
- **Capture needs structure, not just pixels.** CUA gives a11y tree + URL + app/window
  (verified); add CDP for browser selectors + network. The structured trace compiles into the
  deterministic workflow — vision is only for the conversation.
- **Plan correction:** "CUA on Wails" → "capture + voice in the Electron renderer"; Wails/
  oswails stays for OS verbs only.
