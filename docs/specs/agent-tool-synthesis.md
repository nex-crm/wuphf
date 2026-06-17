# Agent Tool Synthesis — observe → spec → author → verify → expose

Status: **Proposed.** A strategic capability spec. Grounds out the
investigation of `badlogic/pi-mono` (the pi coding agent) and
`mvanhorn/cli-printing-press` (both MIT) into a concrete, phased plan for
WUPHF. Execution is sequenced so the **sandboxed execution layer lands first** —
it is the prerequisite that makes everything after it safe.

> The repo `badlogic/pi` we were first pointed at is a vLLM GPU-pod manager and
> is NOT the basis here. The relevant prior art is `badlogic/pi-mono`
> (`packages/coding-agent`) + `badlogic/pi-skills`, and the
> `cli-printing-press` discovery/verification architecture.

## The gap

WUPHF agents can already author one kind of artifact — a sandboxed single-file
React **App** that renders in an iframe and reads workspace data through the
postMessage bridge (the App Builder). That is the whole current ceiling:

- They can build a **UI**, but not a **server-side tool/capability** (a script,
  a CLI, a reusable skill) for a workflow that has no tool yet.
- They cannot integrate a service that exposes **no API** — there is no path
  from "the office needs to talk to X, and X has no clean API" to "an adapter an
  agent can call."

The goal: a general loop where the office can **synthesize a callable capability
for a workflow that has neither a tool nor an API**, safely and with the human
in the loop on anything that mutates the world.

## The loop

```
workflow with no tool / no API
   │  OBSERVE     record the workflow once (WUPHF already has browser-harness/CDP)
   ▼
WorkflowSpec      provenance + trust-tier + auth + quirks   (borrow printing-press)
   │  AUTHOR      App Builder emits a SKILL.md + script / adapter, not just a React App  (pi model)
   ▼
runnable capability
   │  VERIFY      static proof → live behavioral check → bounded fix-loop  (borrow printing-press)
   ▼
EXPOSE            callable by agents, gated by the existing approval cards on writes
```

Neither source repo is the whole answer; they are complementary halves.
**pi** gives the authoring + relocatable-execution model; **printing-press**
gives the observe → canonical-spec → verify model. Most of the plumbing
(broker, claude-code harness, App Builder, browser-harness, `propose_app`
approval, `ExternalActionApprovalCard`, the Phase-3 verify-gate culture) already
exists in WUPHF.

## Prior art we stand on (both MIT — code is borrowable, keep the notices)

### From the pi coding agent (`badlogic/pi-mono`, MIT)
- **Capabilities are files in discovered directories, not registered code
  paths.** A "tool" is a `SKILL.md` (the open *agentskills.io* standard:
  markdown + scripts, progressive disclosure) or a typed extension written into
  a skills/extensions dir. Authoring a tool = writing a file; it is then
  hot-discoverable by every agent.
- **Execution is relocatable via a swappable `Operations.exec()` seam** — host
  shell today, an OS sandbox (`@anthropic-ai/sandbox-runtime`) or micro-VM
  (`gondolin`, container) tomorrow, with the agent loop unchanged.
- **Self-extension is literal:** the agent's own `write` + `bash` tools author a
  skill, then it reloads. No special "make a tool" tool.
- WUPHF already runs **claude-code**, which reads the **identical SKILL.md
  format** and already has `bash`/`write`. So we adopt pi's *model and the
  standard*, NOT a second agent runtime.

### From cli-printing-press (`mvanhorn/cli-printing-press`, MIT)
- **Converge every discovery method onto one provenance-tagged canonical spec.**
  Browser-sniff a live session, mine community SDKs, scrape docs, or parse an
  official OpenAPI — all produce the same validated struct.
- The spec is a **superset of OpenAPI**: it carries `provenance`/`trust-tier`
  (`official | community | sniffed | docs`), per-endpoint confidence, recovered
  auth, rate class, and quirks — and trust-tier is **load-bearing** (a `sniffed`
  spec auto-selects defensive behavior).
- **Three-tier verification + bounded fix-loop:** static structural proof (does
  the generated code only call paths in the spec? is auth wired right?) → live
  behavioral check (run read-only against the real thing, assert relevant
  output) → auto-fix loop (max ~3 iterations). This is how you ship code
  inferred from unstructured observation with confidence.
- Self-contained, directly liftable Go files:
  `internal/browsersniff/{classifier,schema,redact}.go` (traffic → spec:
  endpoint templating, count-based-nullability schema inference, secret
  redaction) and `internal/crowdsniff/patterns.go` (endpoint/auth extraction).
  Leave behind its 166-template Go-CLI **generator** (wrong artifact for us) and
  its 9-phase publish pipeline (overkill).

## Core contracts

### `WorkflowSpec` (broker, Go)
The convergence point. Trim/port `cli-printing-press`'s `spec.APISpec`:

- Operations: path/method/params/request+response schemas (the OpenAPI subset).
- `provenance`: `official | docs | community | sniffed` + per-operation
  `confidence` (how many sources agreed).
- `auth`: recovered scheme (bearer/header-key/query-key) + the header names
  observed; never silently promote a guess — record low-confidence as a
  reviewable warning.
- `quirks`: non-typeable gotchas (pagination shape, required-but-undocumented
  headers, bot-evasion transport hints).
- Stored as a first-class broker object alongside agents/tasks/apps.

### SKILL.md substrate (agentskills.io)
A capability = a directory with `SKILL.md` (`name` + `description` frontmatter)
plus freeform scripts/refs. Only `name`+`description` go in the system prompt as
`<available_skills>`; the full body is `read` on demand (progressive
disclosure). Stored under a per-agent / office `skills/` tree.

### `Executor` (the sandbox seam — the load-bearing contract)
A Go interface modeled on pi's `Operations.exec`:

```
Executor.Run(ctx, cmd, cwd, limits) -> {exitCode, stdout, stderr}
  limits: filesystem allow-list, network allow-list, cpu/mem/time caps
```

Backends, swappable behind the interface: `host` (dev only) → `container` →
`microVM`. Everything an agent-authored skill executes flows through this.

## Phase 0 (FIRST) — Relocatable sandboxed execution

**This gates the entire initiative.** Today the only sandbox WUPHF has is the
**iframe** boundary, and it covers *UI Apps only* — claude-code's `bash` and any
agent-authored server-side script run with the agent's full permissions. Letting
agents write-and-run arbitrary tools without first building this boundary would
be a serious regression. Both source repos flag this explicitly (pi ships *no*
built-in sandbox by design).

Deliverables:
- The `Executor` interface + a first real backend (container or micro-VM) with
  filesystem + network allow-lists and resource caps.
- Wire it so any agent-authored skill/script runs **inside** it, not on the host.
- Network egress and writes from inside the sandbox route through the existing
  **`ExternalActionApprovalCard`** (deterministic-integrations) — a state-changing
  action the human can veto (product principle 2: show, don't surprise).
- `security-reviewer` + an orthogonal triangulation pass on the boundary before
  anything authored runs in it (this is a security-boundary change).

No Phase 1+ work ships until an agent-authored script provably cannot escape the
sandbox or reach the network/filesystem outside its allow-list.

## Phasing (after the sandbox)

1. **SKILL.md substrate.** Adopt agentskills.io as WUPHF's skill format; align
   the existing per-agent skills concept (OwnerAgents / `request_skill_enable` /
   the SKILL.md editor) to it; inject `<available_skills>` via
   `prompt_builder.go`; point claude-code's skills dir at the same on-disk tree
   (zero new runtime — claude-code consumes the identical format). Optionally
   vendor `pi-skills` (MIT) as a seed library.
2. **App Builder → Tool/Skill Builder.** Add a second output target to the App
   Builder: a `SKILL.md` + script (a durable server-side capability) alongside
   the React App. Reuse `propose_app`'s non-blocking approval as the gate
   (authoring an executable skill is state-changing → keep the human veto).
   Authored scripts run only via the Phase-0 `Executor`.
3. **Spec recovery (observe → WorkflowSpec).** Wire browser-harness/CDP capture
   → port `browsersniff/{classifier,schema,redact}.go` → emit a `WorkflowSpec`
   for one real API-less target end-to-end. Redact at the boundary — and redact
   the **spec object itself**, not just on-disk samples (the gap printing-press's
   own analysis flagged).
4. **Adapter generation + verification.** Generate the adapter (a broker-side
   client and/or a generated App/skill) from the `WorkflowSpec`; gate it behind
   the ported three-tier stack — static proof (adapter only calls spec paths,
   auth wired right) → live read-only behavioral check → bounded fix-loop.
   Mark `sniffed`-provenance adapters lower-trust so writes route through the
   approval card.
5. **Expose.** The verified capability becomes callable by agents (an MCP tool /
   a registered skill). The loop closes: a workflow with no tool/API becomes a
   first-class office capability.

## Security model

- **The boundary is Phase 0's sandbox**, not the write-time validator or the
  prompt. Agent-authored code is hostile-by-assumption; it runs only inside the
  `Executor` with an explicit fs/network allow-list and resource caps.
- **Discovery captures live credentials.** Sniffing a workflow records real
  cookies/tokens; treat capture with the same care as the external-action
  boundary, and redact the spec graph (not just samples) before storage.
- **Untrusted community code is parsed, never executed** (crowd-sniff hardening:
  size/symlink/traversal caps, no eval) — keep printing-press's discipline.
- **Trust-tier drives runtime caution:** `sniffed`/`community` capabilities
  require human approval on any write; `official` may be looser.
- **Drift:** undocumented endpoints change silently. Ship a `doctor`-style
  self-check per adapter and a periodic re-validation routine, or these adapters
  rot.

## Reuse, not rebuild

| Need | Borrow from | Note |
|---|---|---|
| SKILL.md format | agentskills.io / pi (MIT) | Open standard; claude-code reads it already |
| Relocatable exec seam | pi `Operations.exec` (MIT) | Reimplement in Go as `Executor` |
| Sandbox backend | `@anthropic-ai/sandbox-runtime` / container / micro-VM | Verify sub-dep licenses |
| traffic → spec inference | printing-press `browsersniff/*` (MIT) | Liftable Go files |
| community-SDK mining | printing-press `crowdsniff/patterns.go` (MIT) | Liftable |
| canonical spec model | printing-press `spec.APISpec` (MIT) | Trim into `WorkflowSpec` |
| verification stack | printing-press `verify/live_check/fixloop` (MIT) | Adapt off our spec |
| capture (recorder) | WUPHF browser-harness/CDP | printing-press delegates this; we already have it |
| approval on writes | WUPHF `ExternalActionApprovalCard` | Already shipped |

Do NOT borrow: pi's TUI or agent runtime (claude-code already is our runtime);
`agent-tools`/`lemmy` (unlicensed/deprecated); printing-press's Go-CLI generator
or 9-phase publish pipeline (wrong artifact / overkill).

## Risks & open questions

- **Sandbox choice** (container vs micro-VM vs OS sandbox): cost, startup
  latency, isolation strength, host-OS portability. Phase 0 must pick and prove
  one.
- **Inference is lossy** — a field never seen null looks required; an unexercised
  endpoint is invisible. The verify + fix-loop is the mitigation; never trust the
  inference without the live check.
- **Language fit:** the vendorable code is Go (printing-press ✓) and TS (pi —
  ideas/format only, no runtime import). The `Executor`/skills wiring is new Go
  in the broker.
- **Overlap with claude-code:** claude-code already gives bash + the same
  SKILL.md loading. Our net-new is the sandbox boundary, the App-Builder skill
  output, and the observe→spec→adapter pipeline — not a new agent loop.

## Out of scope (v1)

A capability marketplace / cross-office sharing; a public catalog; BLE/hardware
device sniffing; auto-re-sniff on drift (ship the `doctor` self-check first);
multi-tenant hosting of synthesized adapters.
