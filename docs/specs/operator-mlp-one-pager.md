# Operator MLP — One Pager

**Status:** Draft for team absorption · **Date:** 2026-06-25 · **Branch:** `pivot/operator-mlp`

> We have product-market signal. This document sharpens WUPHF to the narrowest wedge
> that delivers it, and names what we delete to get there. Read it once; it's the spec.

---

## 1. The bet

Non-technical operators want the power of agentic automation but have no engineering
resources to get it. Today they live in spreadsheets, Slack threads, and manual
copy-paste between tools. They know exactly where it hurts. They cannot build their way out.

We give them one thing: **talk to an AI, and a real internal tool gets built with you** —
a scripted, deterministic workflow plus the UI to run and watch it. It runs without you,
asks you when it's genuinely stuck, and proactively suggests improvements as your business
changes. This is what Convey.dev does. We do it self-hosted, on top of an engine we've
already built.

**The operator never sees:** agents, multi-agent teams, skills, heartbeats, notebooks,
channels, lifecycle states, promotion/review queues. Those concepts are ours, not theirs.
(They *do* see a clean **Knowledge** wiki — but never the messy authoring machinery behind it;
an LLM writes and organizes it.)

---

## 2. ICP and the pain

- **Who:** A non-technical operator at a company — RevOps, ops, partnerships, CS, finance ops.
- **What they know:** ChatGPT / Gemini / Google. They start a chat and expect work to get done.
- **The pain they feel daily:** a repetitive multi-step process — lead routing, request
  triage, scoring, digesting, data movement between tools — that eats hours, demands
  constant attention, and breaks when they're away.
- **Their current "solution":** themselves, plus a spreadsheet and notifications. The
  status quo is human glue. That glue is the competitor.

---

## 3. The product the operator sees

Four surfaces. Nothing else.

| Surface | What it is |
|---|---|
| **Chats** | The home for talking to "your AI." **First run = the call** (screen-share + voice — show your workflow, watch the tool get built; this is the activation magic). **After the call, chat is the iteration surface** — refine the tool, answer clarifications, receive digests. One assistant, not a team. (CEO decision: the call is the primary activation/demo and the ship gate; chat is for ongoing improvement, not the front door.) |
| **Internal Tools** | The unit of value. Each tool has **3 tabs, Bubble-style: UI · Workflow · Data**. UI = the surface the operator monitors (tables, request queues, scores). Workflow = the deterministic, scripted automation (actions, conditions, triggers, branching, AI steps). Data = the tool's data model. |
| **Knowledge** | The company brain, shown as a clean Karpathy-style **LLM wiki** (Wikipedia-shaped entity / concept / process pages, citations, cross-links, lint). Knowledge powers workflows day-to-day and is **cross-cutting** — one fact powers many tools. The operator browses, monitors freshness, and curates here; an LLM does the writing/organizing. Backed by **gbrain** for retrieval. |
| **Integrations + Settings** | Connect the apps the workflow touches. Set notification channel (email/Slack), approvals, schedule. |

### The everyday happy loop

1. Operator jumps on a call (or chat) and **shows their workflow** the way they'd train a
   new hire — narrating while they work.
2. AI asks for the **integrations and files** it needs, and the operator **watches the
   tool get built** as they explain.
3. Operator **runs it on test data**.
4. Operator gives **feedback**; AI incorporates changes; operator watches them land.
5. Operator **publishes**.
6. Operator gets a **daily digest** (email/Slack) of runs, plus distinct **clarification
   messages** when the AI is blocked.
7. Operator gets **proactive improvement suggestions** when business context changes,
   with full reasoning. Approve / reject / edit.

---

## 4. What this is technically

**Core principle: agentic freedom to figure out the workflow; determinism to execute it.**
The two phases run in opposite modes:

- **Build / discover (agentic).** The model has full freedom — drive the operator's browser,
  probe, click around, sniff the app's APIs, write app-specific glue, try things, ask
  questions — to *figure out* the workflow *with* the operator. This is where exploration and
  understanding happen.
- **Execute (deterministic).** That agentic session crystallizes into a compiled
  **deterministic, scripted workflow** — not an AI tool-call chain. The control flow is fixed
  and auditable: states, events, guards (conditions), actions, branching, triggers. AI
  contributes only at **specific, declared steps** (classify, draft, summarize) as pure reads
  with deterministic-shaped outputs. Durability, retries, audit, and version history come from
  a Temporal/Inngest-style execution layer. The same thing, every time.

Agentic freedom is embraced at build and refused at execute (except the bounded CUA-healing
fallback). That split is the whole design.

**How actions execute (deterministic, including computer control).** Each action runs
API-first: if capture caught the network call behind it, the workflow calls the API directly.
For steps with no API, it does **deterministic UI replay** — replaying the *captured*
selectors/inputs against the live app via a computer-use driver (CUA). Only when replay
breaks (selector moved, layout changed) does CUA *heal* — bounded, to re-find the target —
and the new target is written back as an overlay (version+1), surfaced to the operator. CUA
is the fallback + healing layer under a deterministic engine, **never** a free-roaming agent
deciding each run. Sensitive actions stay behind the approval gate.

Every run is recorded. Every workflow change is versioned with the context behind it.
Every learning from a clarification is folded back into the workflow and **visible in it**.

---

## 5. Keep & build on, replace, delete

Every existing piece was built for the *multi-agent office* product. Each passes one test:
**does an inbound-routing operator need this?** Three outcomes — keep and build on, replace,
or delete. The net codebase gets *lighter*, but we do not throw away what genuinely fits.

### Keep & build on (these are foundations, not just kernels)

| Capability | What we keep | What we repoint for the new goal |
|---|---|---|
| Deterministic workflow engine | contract, runner, `shipcheck`, Inngest adapter, run-audit JSONL, overlays, refreeze (`internal/workflowpress`, `internal/workflow`) | nothing — kernel fits as-is |
| **Workflow detection — now core** | the detector (`workflow_detect.go`) | input changes: observe the **operator's** activity + context changes + integration events (not a roster of agents). It *proposes* workflows. |
| **App builder — the Internal Tool UI tab (production-grade, lift intact)** | the whole build-with-you-via-chat experience: refine+Mantine single-file, build gate, server-side build ownership, build-time guards, live hot-reload preview, persistent edit chat, select-to-edit, source introspection (anti-hallucination), version snapshots, Bridge v2 | **minimal repoint** (Spike 6): structural-singleton + human-gated proposal already fit one operator; the "narrate to a watching human" persona *fits the magical call*; read-mostly stays for the UI tab. Bridge v2 complements the workflow engine (UI reads/reasons; engine executes; shared approval card). |
| Integrations + approval-on-mutation | Bridge v2 (reads run, writes gated), Composio | shed per-agent/roster metering assumptions |
| **Knowledge surface — the karpathy-wiki reader (lift S6 only)** | from **`feat/karpathy-wiki`**: the Wikipedia-shaped reader + design + IA + lint view + schema (entity/concept/process pages, categories) | keep the **reader/surface**; **retire the S1-S5 Go compile backend → gbrain owns it** (Spike 7). Point the reader at gbrain's pages. |

### Replace

- **Context AND Knowledge backend → [gbrain](https://github.com/garrytan/gbrain), for it all.**
  Today context lives across `internal/embedding`, `wiki_index_bleve/sqlite`,
  `broker_entity_graph`/`facts`, and the Nex graph — and the karpathy-wiki branch built its own
  Go compile/source backend. Replace **both** with **gbrain as the single brain** (sources,
  compile, index, graph, citations, retrieval, self-maintenance), reached over **MCP**
  (`search`/`query`/`put_page`). gbrain is a mature superset; we keep only the karpathy-wiki
  *reader* as the human Knowledge surface. One brain instead of four bespoke stores + a parallel
  compile engine.

### Delete — remove, don't hide. Stay light.

Removed from the codebase, not flagged off. The bar: if it only existed to serve the
multi-agent office, it goes.

- Multi-agent team, agent roster, agent subspaces → one assistant persona remains.
- **The OLD messy wiki authoring** — notebooks, promotion/review queue, Pam-curation (already
  removed on `feat/karpathy-wiki`). The clean Karpathy **Knowledge** wiki replaces it; gbrain
  is the retrieval brain.
- Skills catalog, skill compile/synth/guard, policies.
- Channels / DMs surface / lifecycle pills / inbox-as-decision-queue → replaced by **Chats** +
  a tool-scoped run/approval view.
- Company/team onboarding wizard → replaced by **"what do you want to automate?"**.

The broker shrinks to a message + run + integration + approval bus for one operator and their
tools. The operator never meets the words "broker," "agent," or "wiki."

---

## 7. What we build new

1. **The magical capture call (the demo bar)** — **screen-share video call with your AI**,
   free back-and-forth voice. Operator narrates their workflow live; CUA-style observation
   watches the screen; a realtime voice agent (ElevenLabs / GPT realtime) talks back and asks
   clarifying questions. Output is a structured workflow draft the engine compiles. **Runs in
   the Electron desktop renderer** (`apps/desktop` — Chromium does screen capture +
   WebRTC voice natively; Wails/oswails stays for OS verbs). This is the experience —
   nothing demos until it works. (Voice round-trip measured at 620ms — conversational.)
2. **Operator shell** — a thin web app: Chats · Internal Tools · Integrations · Settings.
   A fresh, small refine-based surface on the *smaller* base left after deletion.
3. **Internal Tool object = UI + Workflow** (Data deferred to v1.1), fusing the app-builder
   render stack + workflow kernel into one unit with one build/edit chat.
4. **gbrain context store** — stand up gbrain as the single context substrate; AI steps and
   the capture call retrieve from it; runs and learnings write to it.
5. **Detection → proposal** — repoint the detector at operator/context signals so the system
   can say "I noticed you do this — want a tool for it?" alongside the capture path.
6. **Daily digest + clarification messages** — scheduled run-summary to email/Slack; a
   separate blocked/needs-approval channel.
7. **Proactive improvement suggestions** — improvement signals (context change, recurring
   exception, SLA miss) → operator-facing "suggested change + why" card.

---

## 8. Locked decisions

1. **First wedge: Inbound routing + scoring.** One workflow shape nailed end-to-end (inbound
   request/lead → score → route → notify). Matches the engine's example and the RevOps/Brex
   anchor. No owned data layer required.
2. **Demo bar = the magical call.** No demo until real screen-share + free back-and-forth
   voice with your AI works, building a tool before your eyes. Capture is the headline, not a
   deferrable pole. **Capture + voice ride the Electron renderer** (`apps/desktop`), which
   builds our dmg/exe today via electron-builder; Wails/oswails is OS-verbs only.
3. **Capture + detection, both first-class.** Active (the call) and passive ("I noticed you do
   this") authoring paths. Detection stays core, repointed to operator/context signals.
4. **Context: replace our engine with gbrain.** One semantic context store for all
   workflow/tool context, retrieval, runs, and learnings.
5. **App builder: build on top.** Keep the render/build/edit foundation; rewrite only the
   read-mostly + roster-agent assumptions; from-scratch only where it clearly wins.
6. **Data Model: IN the MLP** (eng-review #6). The acceptance scenario needs persistent
   per-request entity state (status across runs) + the digest counts — run-records alone can't
   show a live worklist. Internal Tool ships **3 tabs (UI · Workflow · Data)**; Data = operator-
   owned typed tables for tool run-state/entities (distinct from gbrain, which is the knowledge
   brain, not a run-state DB).
7. **Build strategy: simplify in place** in this worktree. Keep & build on the engine,
   detection, and app builder; replace context with gbrain; delete the office cruft (wiki
   product, skills, notebooks, agent roster, channels, lifecycle/review). Net codebase lighter.
8. **Naming: "Internal Tool"** (Bubble-style tabs).
10. **Activation + voice economics (CEO review).** The **call is the primary activation + ship
    gate** (the magic; chat is post-call iteration) — founder decision, with the concentrated-risk
    bet named and owned. **Voice = BYO-key** (operator's OpenAI Realtime key in Settings) **or
    Nex-cloud hosted/metered**; **no key → the call is optional and chat-authoring is the keyless
    entry** (graceful degradation keeps the source-available tool usable). **Unattended digest**
    works for Composio-covered workflows (server-side); sniffed/UI-replay fallback is
    operator-present/manual. **gbrain** ships as a bundled subprocess (PGLite + local ollama
    embeddings default, BYO cloud-embedding-key optional).
9. **Knowledge = a 5th surface.** **gbrain owns the entire Knowledge backend** (sources, compile,
   index, graph, citations, retrieval, self-maintenance — it's a mature superset of what
   `feat/karpathy-wiki` built, so that Go compile backend is retired). **We keep only the
   karpathy-wiki Wikipedia reader + design + IA** as the human surface over gbrain. Office activity
   feeds gbrain ingestion; the broker uses gbrain via CLI/MCP. Knowledge is cross-cutting (powers
   many workflows). gbrain's federation/OAuth-scoping is team-scale (Nex cloud), not needed for the
   single-operator MLP.

---

## 9. Out of scope for MLP

- Marketplace / templates gallery, team collaboration, role permissions.
- Multi-tenant hosting (that's Nex cloud, not this OSS repo).
- **Free-roaming autonomous agent execution** — CUA controls the computer at execution, but
  only as deterministic UI replay + bounded healing under the workflow engine (see §4), never
  an agent improvising the whole task each run.

---

## 10. The one number that matters

An operator goes from "here's how I do X" to a **published tool that ran on real data and
emailed them a digest** — without writing code, and without learning a single internal
concept (agent/wiki/skill). If we can't get one real operator through that loop, nothing
else matters.
