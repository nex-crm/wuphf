# Workflow Press

Status: **Proposed.** The product architecture for WUPHF, sharpened by deep
reads of `mvanhorn/cli-printing-press` and the `browser-harness` self-improving
agent (both MIT). Supersedes the earlier "agent tool synthesis" framing.

## Positioning — what WUPHF is, and is not

WUPHF is a **workflow press for revenue operators**:

> Discover messy repeated work → freeze it into a reviewed contract → generate
> the internal tool → prove it works → keep improving it from live usage.

WUPHF is **not** "workflow memory" (a place that merely remembers runs) and
**not** a giant always-on workflow engine. It is a *press*: it takes the messy,
repeated, undocumented work a RevOps operator does by hand and stamps out a
proven, improvable internal tool with a reviewed contract behind it.

ICP: the revenue operator (RevOps) — the person doing the same multi-step,
cross-system work over and over with no tool and no API.

## Two load-bearing principles (from the research)

**1. Discovery does not become code. It becomes an evidence-backed IR first.**
(from cli-printing-press.) Observation is messy and lossy. So discovery is
distilled into a narrow, structured contract *with its evidence attached*, and
**only that contract** drives generation, verification, scoring, and
improvement — all deterministically. Never wire the model's raw observation
straight into a tool; freeze it into a spec first.

**2. Self-improvement lives outside a small, protected kernel.** (from
browser-harness.) Do not bolt a learning loop into a giant engine. Keep a
**small protected kernel** (the contract schema, the generator, the shipcheck,
the runner runtime, the overlay-apply/replay/accept machinery — frozen,
reviewed, versioned). Persist everything mutable **outside** it: run
observations, failures, operator edits, durable playbooks/skills, proposed
overlays. Improvements arrive as **overlays/patches → reviewed → replayed
against fixtures → accepted** — never as direct mutations of the kernel.

## The five artifacts (the press pipeline)

```
            messy repeated RevOps work (no tool, no API)
                              │
     ┌────────────────────────┴───────────────────────────┐
     │  1. workflow-research.json   (raw, evidence)        │  ← discovery
     └────────────────────────┬───────────────────────────┘
                              │  FREEZE (operator-reviewed)
     ┌────────────────────────┴───────────────────────────┐
     │  2. workflow-spec.json       (canonical contract)   │  ← the reviewed contract
     └────────────────────────┬───────────────────────────┘
                              │  deterministic generation
     ┌────────────────────────┴───────────────────────────┐
     │  3. generated local workflow (the internal tool)    │  ← runner + inngest + tests
     └────────────────────────┬───────────────────────────┘
                              │  mechanical proof
     ┌────────────────────────┴───────────────────────────┐
     │  4. workflow-shipcheck       (proof it works)       │  ← gate to ship
     └────────────────────────┬───────────────────────────┘
                              │  live usage
     ┌────────────────────────┴───────────────────────────┐
     │  5. improvement loop         (overlays, reviewed)   │  ← prefer UPDATE over new
     └──────────────────────────────────────────────────────┘
```

### 1. `workflow-research.json` — raw discovery
Everything observed, kept messy and evidence-rich: session context, operator
notes, sample records, exceptions seen, operator edits, tool traces. This is the
**outside-the-kernel** evidence store — append-only, never the source of truth
for generation.

### 2. `workflow-spec.json` — the canonical contract
The frozen, operator-reviewed contract that everything downstream is generated
and verified against. It is a **workflow state machine**, not just an API spec:

- `goal` — what the workflow accomplishes.
- `operator` — whose work this is (the RevOps human in the loop).
- `entities` — the domain objects it moves.
- `states` / `events` / `guards` / `actions` — the state machine: transitions,
  the conditions that gate them, the side-effecting actions.
- `exceptions` — the known failure/edge cases and how they're handled.
- `slas` — timing/freshness expectations.
- `verification_scenarios` — the fixtures + expected transitions used by
  shipcheck (the contract carries its own tests).
- `improvement_signals` — what to watch in live usage that should propose an
  overlay (recurring exceptions, operator edits, SLA misses).

Each field carries **provenance / trust-tier** (`observed | operator-stated |
inferred`) and a confidence — borrowed from cli-printing-press, where trust-tier
is load-bearing (an `inferred` write degrades to a human-approved one).

**Freezing the spec is the human gate** — the operator reviews and accepts the
contract. This is the "freeze into a reviewed contract" step.

### 3. Generated local workflow — the internal tool
Produced **deterministically** from the spec (templated, not LLM-freeform):
`runner`, `types`, `exceptions`, `state`, an **inngest adapter** (durable
execution), `fixtures`, `docs`, `tests`. The artifact is the operator's tool.

### 4. `workflow-shipcheck` — mechanical proof
A deterministic gate run before a workflow ships or an overlay is accepted. It
proves the generated tool honors the contract:

- **fixture replay** — the `verification_scenarios` run and pass.
- **transition coverage** — every state/transition in the spec is exercised.
- **idempotency** — re-running an action doesn't double-apply.
- **duplicate handling** — duplicate events are absorbed.
- **stale handling** — stale events/records are rejected per the SLAs.
- **audit completeness** — every action leaves an audit trail.
- **adapter parity** — the inngest adapter behaves identically to the local
  runner (no drift between the two execution paths).

### 5. Improvement loop — overlays, reviewed, prefer update
Operator edits and recurring exceptions become **proposed overlays** (patches to
the spec), surfaced for review, replayed against fixtures by shipcheck, then
accepted (folded into the spec, version-bumped). **WUPHF prefers updating the
existing workflow over creating a new one** — convergence, not proliferation.
The kernel never changes; only the per-workflow spec + overlays do.

## Mapping onto WUPHF — reuse, not rebuild

| Layer | Build on / borrow | Note |
|---|---|---|
| Discovery → `workflow-research.json` | WUPHF **browser-harness/CDP** + session context + operator notes + tool traces | We already have the recorder cli-printing-press lacks |
| Inference (evidence → structure) | cli-printing-press `browsersniff/{classifier,schema,redact}.go`, `crowdsniff/patterns.go` (MIT, liftable) | endpoint templating, count-based-nullability schema inference, secret redaction |
| `workflow-spec.json` contract | cli-printing-press `spec.APISpec` model, **extended to the state machine** | provenance/trust-tier is load-bearing |
| FREEZE (review) gate | WUPHF `propose_app` non-blocking approval | operator accepts the contract |
| Deterministic generation | the **App Builder**, generalized from "one React App" to "generate from the contract" | templated, deterministic — not freeform |
| Durable execution | **inngest adapter** | durable workflow runtime |
| `workflow-shipcheck` | the Phase-3 **verify-gate culture**, expanded to the mechanical-proof list | static + behavioral, bounded fix-loop |
| Improvement overlays | the **kernel + overlays** model (browser-harness) + the **wiki/notebook curation** + `ExternalActionApprovalCard` | reviewed → replayed → accepted |
| Safe execution | **Phase 0 sandbox** (below) | runs generated runners + any authored overlay code |

Do NOT borrow: cli-printing-press's 166-template Go-CLI generator (wrong
artifact — we want in-process/inngest, not shipped binaries) or its 9-phase
publish pipeline (overkill); pi's TUI/runtime (claude-code already is our
runtime); `agent-tools`/`lemmy` (unlicensed/deprecated).

## Phase 0 (FIRST) — relocatable sandboxed execution

Still the load-bearing prerequisite. The generated runners execute actions
against external systems, and accepted overlays may carry authored code; both
need an isolation boundary WUPHF does not yet have (the iframe sandbox covers
*UI Apps only*). Deliver an `Executor` seam (host → container → micro-VM) with
filesystem + network allow-lists and resource caps; route network/writes through
the existing `ExternalActionApprovalCard`; `security-reviewer` + triangulation
before anything generated/authored runs in it. **Nothing downstream ships until
this boundary is proven.**

## Execution sequencing (after the sandbox)

1. **Contracts + kernel boundary.** Define `workflow-research.json` and
   `workflow-spec.json` schemas (the state machine + provenance) and draw the
   protected-kernel line: schema, generator, shipcheck, runner runtime, overlay
   machinery inside; research/observations/edits/overlays outside.
2. **Discovery → research.** Wire browser-harness/CDP capture + operator
   notes + tool traces → `workflow-research.json`; port the cli-printing-press
   inference files. Redact the **spec object itself**, not just samples.
3. **Freeze → spec.** Synthesize `workflow-spec.json` from research; surface it
   for operator review/approval (the human freeze gate).
4. **Generate.** Deterministically emit the local workflow (runner, types,
   exceptions, state, inngest adapter, fixtures, docs, tests) from the spec via
   the generalized App Builder.
5. **Shipcheck.** Run the mechanical proof; gate ship on it.
6. **Improvement loop.** Operator edits + recurring exceptions → proposed
   overlays → review → replay (shipcheck) → accept; prefer updating the existing
   workflow over a new one.

## Security model

- The boundary is **Phase 0's sandbox**, not the validator or the prompt;
  generated runners + authored overlays are hostile-by-assumption.
- **Trust-tier drives caution:** `inferred`/`observed` actions require human
  approval on writes; `operator-stated` may be looser.
- **Discovery captures live credentials** — redact the spec graph (not just
  on-disk samples) before it is stored.
- **Untrusted community code is parsed, never executed** (crowd-sniff hardening).
- **Overlays never touch the kernel** — they patch the per-workflow spec and are
  replayed against fixtures before acceptance.

## Risks & open questions

- **Sandbox choice** (container / micro-VM / `sandbox-runtime`) — Phase 0 must
  pick and prove one.
- **Inference is lossy** — the freeze step + shipcheck + the human review are the
  mitigation; never trust observation without the review and the proof.
- **Contract drift** — undocumented systems change silently; `improvement_signals`
  + a periodic re-validation routine catch it, otherwise workflows rot.
- **State-machine expressiveness** — the spec must cover real RevOps workflows
  (multi-entity, long-running, human-in-the-loop) without becoming a
  general-purpose engine. Keep the kernel small.
- **inngest fit** — confirm inngest as the durable adapter vs a thinner
  broker-side runtime; the "adapter parity" shipcheck guards the dual path.

## Out of scope (v1)

A capability marketplace / cross-office sharing; a public catalog; auto-re-sniff
on drift (ship `improvement_signals` + the manual review first); multi-tenant
hosting of synthesized workflows; a general-purpose workflow engine (the press
generates *specific* proven tools, it is not the runtime for arbitrary graphs).
