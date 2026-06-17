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

## Wire-format versioning & compatibility

The two contract artifacts (`workflow-spec.json` and `workflow-research.json`)
are wire shapes other code reads — the generated tool decodes the spec it was
generated from, the published JSON Schema describes it to cross-language
consumers, and overlays patch it. A wire shape that can change silently is a
foot-gun: a removed or renamed field that a lenient decoder zero-values can drop
a `guard` or flip a `RequiresApproval` flag to `false` without anyone noticing.
So both artifacts carry an explicit, asserted **wire-format version**.

**`schema_version` is distinct from the content `version`.**

- `schema_version` versions the *serialized shape* of the artifact. It is a
  package constant (`SchemaVersionWorkflowSpec`, `SchemaVersionWorkflowResearch`,
  both currently `1`) serialized as the JSON key `"schema_version"`.
- `version` is the per-spec *content counter* — it bumps when an overlay is
  accepted (`v3 → v4`), and says nothing about the wire shape.

A spec can sit at content `version: 7` while still on `schema_version: 1`; an
overlay bumps `version` and leaves `schema_version` untouched.

**Fail closed on unknown/newer.** `WorkflowSpec.Validate` checks
`schema_version == SchemaVersionWorkflowSpec` *before any field-level check* and
rejects anything else (`ErrUnsupportedSchemaVersion`), including a newer version
a producer ahead of this kernel might emit. An unknown wire format is never
decoded best-effort — the kernel cannot prove it understands it, so it refuses.

**The generated tool loads its embedded spec strictly.** The generated
`loadSpec` delegates to the kernel's `DecodeSpecStrict`: a `json.Decoder` with
`DisallowUnknownFields`, plus the `schema_version` assertion and the full
state-machine `Validate`. A removed/renamed field, a version mismatch, or
trailing data fails **loudly** at load instead of a lenient `json.Unmarshal`
silently zero-valuing a guard or an approval flag. The strict-decode logic lives
in the reviewed kernel, not in per-workflow generated code, so every generated
tool inherits it.

**The published JSON Schema is stamped and versioned.** The committed
`testdata/schema/*.json` carry a `$schema` dialect pin and a versioned `$id`
(`…/workflow-press/v1/…`). The `/v1` path segment tracks the `schema_version`
major; the byte-exact drift guard (`TestSpecSchemaMatchesType`) stamps the same
`$schema`/`$id` onto the schema it infers from the Go type, so the published
contract, its version stamp, and the kernel type cannot drift apart.

**Compatibility policy.**

- **Additive within a major is non-breaking.** Adding a new *optional* field (an
  `omitempty` Go field with a safe zero value) does NOT bump `schema_version`:
  an older reader tolerates it (the schema allows it; strict readers in the same
  major are regenerated in lockstep), and a newer reader fills the zero value.
- **Breaking changes bump the major.** Removing or renaming a field, changing a
  field's type, making an optional field required, or tightening an invariant in
  a way an old payload would fail — bump the `schema_version` const **and** the
  `/vN` segment of the schema `$id` in the same change, and regenerate the
  committed schema (the drift guard enforces this).
- **Never reuse a major for a breaking change.** Because `Validate` fails closed
  on a non-current version, a producer and this kernel must agree on the major;
  a silent breaking change under the same major would be rejected, not
  mis-decoded — which is the safe failure, but still a bug to avoid.

## Generated-tool ↔ kernel coupling policy

A generated tool is coupled to this kernel on **two** axes, and each gets an
asserted version. Triangulation architect #2 flagged this: the generated tool
both **imports the kernel** (the runner runtime, `DecodeSpecStrict`, the Executor
seam) *and* **embeds the spec**, and those two were unversioned with respect to
each other — a kernel change could silently break a committed generated tool with
nothing asserting the two still agree.

**The two axes, both stamped into every generated tool:**

- **Spec wire-format axis — `schema_version`.** Already covered above: the
  embedded spec carries `schema_version`, and the generated `loadSpec` →
  `DecodeSpecStrict` fails closed on an unknown/newer one.
- **Kernel axis — `KernelVersion`.** New. `KernelVersion` (a package constant in
  `version.go`) versions the *kernel itself*: the runner runtime, the strict
  loader, the Executor seam, and the **generator templates**. It is distinct from
  `schema_version` (the spec wire shape) and from a spec's content `version`. It
  bumps on any change that could alter generated output or the runtime contract a
  generated tool depends on — a template edit, a runner behaviour change, a new
  generated file, a guard-evaluation change.

**The stamp + the load-time assertion.** The generator emits two constants into
each tool's `workflow.go` — `generatedKernelVersion` and `generatedSchemaVersion`
— recording the kernel and wire-format versions it generated against. The
generated `loadSpec` calls `wp.RequireKernelCompat(generatedKernelVersion,
generatedSchemaVersion)` against the kernel it actually links. Both must match
**exactly** (not `>=`): the kernel cannot prove forward *or* backward
compatibility across a bump, so any difference fails closed with
`ErrKernelIncompatible` — the same fail-closed posture as the `schema_version`
gate. The assertion logic lives in the reviewed kernel (`RequireKernelCompat`),
not in per-workflow generated code, so every tool inherits it.

**Policy: regenerate-on-bump, NOT pin.** There is exactly one supported
`(KernelVersion, SchemaVersionWorkflowSpec)` pair at a time. WUPHF does **not**
keep old kernels around to run old tools (no version-pinning, no compatibility
shims). Instead, whenever either version bumps, **every generated tool is
regenerated** from its frozen spec against the new kernel — convergence, not a
fan-out of pinned variants. This matches the press's "prefer update over a new
workflow" stance: one kernel, one set of regenerated tools.

**The CI hook that enforces it.** The committed golden tree under
`internal/workflowpress/testdata/generated/<id>/` is the *exact* output the
current kernel emits for the three ground-truth example specs.
`TestGeneratedOutputMatchesCommitted` regenerates all three and asserts the bytes
are **byte-identical** to the committed golden (and that no committed file is
stale). A kernel/template/spec change that alters generated output — or would
break a committed tool — makes this test **fail**, forcing the author to:

```sh
go test ./internal/workflowpress -run TestGeneratedOutputMatchesCommitted -update
```

then review and commit the regenerated diff. CI runs **without** `-update`, so an
un-regenerated change fails the build. `TestDriftGuardCatchesTemplateTweak` is the
safety net for the safety net: it proves a perturbed template produces output that
differs from the committed golden, so the guard can never be silently satisfied by
drifted output. This is the regenerate-on-change enforcement — the kernel and
every generated tool stay in lockstep, by construction.

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
