# RFC: Large-I/O framework + platform/agent responsibility split

Status: DRAFT (for triangulation)
Author: workflow-press lane
Date: 2026-06-18
Branch: design/session-workflow-detection

## 1. Summary

Workflow steps call integrations (Composio) and pass data between steps. Those
payloads can be arbitrarily large. Today the platform handles large I/O badly and
hardcodes integration-specific knowledge in the broker. This RFC proposes:

- A **platform-level large-I/O framework**: fail-loud truncation, a size-budget
  reducer, and a lightweight-mode/pagination convention — generic, reusable by
  every integration and every step.
- A **responsibility split**: the platform owns *generic* large-I/O mechanics;
  the **workflow-builder agent** owns *integration-specific policy* (which query,
  which flags, which fields) by authoring it into the workflow **spec**, not into
  our Go.

It is motivated by a concrete production-shaped bug (below) and by the
architectural smell of `execGmailFetch`/`parseDigestEmails` living in the broker.

## 2. Motivation

### 2.1 The bug (symptom)

A frozen "daily email digest" workflow returned an **empty digest** even though
the inbox had 25 unread messages. Root cause:

- `GMAIL_FETCH_EMAILS` returns full message bodies + attachments (multi-MB of
  base64 per fetch).
- The Composio client read responses through
  `io.ReadAll(io.LimitReader(resp.Body, 2<<20))` (`internal/action/composio.go`).
  A response over 2 MiB was **silently truncated** mid-stream → invalid JSON →
  `parseDigestEmails` returned `nil` → "0 emails" → the AI honestly wrote "inbox
  empty."
- Volatile: small fetches stayed under 2 MiB and worked; larger ones silently
  zeroed. This is the worst failure mode — a silent, data-dependent wrong answer.

The shipped hotfix (`verbose:false` + per-message parsing + raising the cap to
32 MiB) makes *this* workflow work, but it is a domain-specific patch in
platform code.

### 2.2 The architectural smell

`internal/team/broker_workflow_actions.go` currently hardcodes:
`isGmailFetchAction`, `execGmailFetch` (Composio call + exact params),
`parseDigestEmails` (Gmail response shape), `execComposeDigest` (digest
heuristic). The broker now "knows" what a Gmail digest is. Every new integration
or workflow shape would grow another `execXFetch`/`parseX`. Integration quirks
(needs `verbose:false`; window to 7 days; this field layout) are **policy the
builder agent should discover and encode in the spec**, not platform code.

## 3. Goals / non-goals

**Goals**
- The platform NEVER returns silently-truncated/corrupt payloads. Oversize is an
  explicit, typed, recoverable signal.
- Large payloads are bounded before they enter an LLM prompt or a persisted run
  record, with the reduction recorded (honest, not silent).
- Integration-specific policy (params, flags, field selection) lives in the
  workflow spec, authored by the builder agent — removable from broker Go.
- Determinism of `shipcheck`/replay is preserved (the framework runs only in the
  live exec path; replay uses the pure recorder).

**Non-goals (this RFC)**
- A general streaming/chunked-execution engine for steps. Out of scope; we bound
  and reduce, we do not stream.
- Re-architecting Composio transport beyond the read path.
- Multi-tenant / hosted concerns (this repo is single-trust-domain OSS).

## 4. Responsibility split

| Concern | Owner | Mechanism |
|---|---|---|
| "Response too large → don't corrupt it" | **Platform** | typed `ErrResultTooLarge`, fail-loud reader |
| "Bound a payload before a prompt/record" | **Platform** | size-budget reducer + reduction marker |
| "Metadata-only / paginate" *mechanism* | **Platform** | lightweight-mode flag passthrough + cursor helper |
| "Use `verbose:false`, window 7d, these fields" *policy* | **Builder agent** | encoded in the spec action's `params` |
| "Run any spec-declared integration action" | **Platform** | one generic integration executor |
| "Learn from `ErrResultTooLarge` and re-freeze" | **Builder agent** | self-heal: amend `params`, re-freeze |

## 5. Design

### 5.1 Fail-loud truncation (transport)

Replace the silent cap with overflow detection. Read `cap+1`; if the body fills
the cap, the response was truncated → return a typed error rather than corrupt
bytes.

```go
// internal/action/ (transport-level)
type ResultTooLargeError struct {
    Action  string
    Limit   int64
    AtLeast int64 // bytes observed (== Limit+1 when detected by sentinel read)
}
func (e *ResultTooLargeError) Error() string { ... }

// readCapped returns the body or *ResultTooLargeError if it exceeds limit.
func readCapped(r io.Reader, limit int64) ([]byte, error) {
    buf, _ := io.ReadAll(io.LimitReader(r, limit+1))
    if int64(len(buf)) > limit {
        return nil, &ResultTooLargeError{Limit: limit, AtLeast: int64(len(buf))}
    }
    return buf, nil
}
```

`ExecuteAction` returns the typed error. The default limit rises from 2 MiB to a
generous platform default (e.g. 32 MiB) AND is configurable; the point is that
*crossing* it is now an explicit, recoverable signal, not silent corruption.

### 5.2 Size-budget + reducer

A platform helper bounds any payload destined for an LLM prompt or a persisted
run record. It reduces *structurally* (drop array tail, project fields, truncate
strings) and stamps a marker so consumers/operators see that reduction happened.

```go
// internal/workflow/ (or a new internal/largeio)
type Budget struct{ MaxBytes int; MaxItems int; StringCap int }

// Reduce returns a bounded view of v plus a Reduction describing what was cut.
func Reduce(v any, b Budget) (reduced any, r Reduction)
type Reduction struct{ ItemsOmitted int; BytesBefore, BytesAfter int; Truncated bool }
```

The live LLM executor renders prompts through `Reduce` and records the
`Reduction` in the run outputs (`_reduction` key). Persisted run records also get
reduced so `runs.jsonl` cannot grow unbounded from one fat fetch.

### 5.3 Lightweight-mode + pagination convention

The platform exposes the *mechanism*, the agent picks the *policy*:

- **Lightweight mode**: a normalized request hint (`metadata_only: true`) the
  integration executor translates to the provider's flag where known
  (Gmail `verbose:false`, etc.). When unknown, it is a no-op (and the size
  framework still protects us).
- **Pagination**: a cursor helper (`next_cursor` in/out) so a step can request
  another page deterministically rather than one giant fetch.

### 5.4 Generic integration executor (moves domain logic to the spec)

Extend `workflow.Action` so an integration step is fully described by data:

```go
type Action struct {
    ID, Kind, Description string
    Platform, ActionID    string         // already exist (external sends)
    Params  map[string]any `json:"params,omitempty"`  // NEW: provider call args
}
```

A single `execIntegrationAction` runs ANY action that declares
`Platform`+`ActionID`: it calls Composio with `Params`, applies §5.1/§5.2, and
threads the raw result into step `data` under the action id. This replaces
`isGmailFetchAction`/`execGmailFetch`/`parseDigestEmails`. `compose_digest`
becomes a pure LLM step (already real AI). The broker stops knowing about Gmail.

Kind handling becomes: `deterministic`+(Platform/ActionID set) → integration
read; `llm` → provider call (existing); `external` → approval gate (existing);
bare `deterministic` → no-op/transform.

### 5.5 Builder-agent role

When `execIntegrationAction` returns `ResultTooLargeError`, the run records it and
the **workflow-builder agent** (not the platform) reacts: it amends the action's
`Params` (e.g. add `metadata_only:true`, lower `max_results`, add a query window)
and re-freezes via `workflow_improve`. The agent's prompt/tooling gains: (a) the
spec-action `Params` field, (b) guidance to set `metadata_only` on large reads,
(c) `ResultTooLargeError` as a self-heal signal.

## 6. Wire-shape change + back-compat

- New optional `Action.Params` (and a normalized `metadata_only` hint). Additive,
  `omitempty`; old specs without it load unchanged.
- Run-record outputs gain `_reduction` (optional). Additive.
- Migration: existing hardcoded Gmail behavior is preserved by a shim that, on
  load, treats a `gmail_fetch_emails`-shaped action without `Params` as
  `{platform:gmail, action_id:GMAIL_FETCH_EMAILS, params:{...defaults, verbose:false}}`
  — so the demo spec keeps working without an agent re-freeze. New specs are
  authored with explicit `Params`.
- Per repo rules, the new wire shapes (`Action.Params`, `metadata_only`,
  `ResultTooLargeError`, `_reduction`) require triangulation before merge.

## 7. Determinism & shipcheck

- All of §5 runs ONLY in the live exec (`makeWorkflowActionExecWithGate`).
  `shipcheck`/replay uses `recordingExec` (pure `{OK:true}`), so reduction and
  real provider/integration calls never touch the determinism/idempotency/parity
  proofs. This invariant must be preserved and tested.

## 8. Security / SRE

- Fail-loud avoids acting on corrupt data (a silent-truncation is a correctness
  AND a safety risk — an agent could "decide" on a half-payload).
- Reduction must not leak secrets into prompts more than the raw payload would;
  field projection should prefer allow-lists for known-sensitive shapes.
- Raising the read cap raises a memory ceiling per in-flight action; the cap stays
  bounded + configurable, and pagination is the escape hatch for genuinely huge
  data. Reads remain context-bounded (timeouts unchanged).
- `ResultTooLargeError` must be logged with action id + size for operability.

## 9. Rollout slices

1. **S1 — Fail-loud transport** (`readCapped` + `ResultTooLargeError`, raise +
   make cap configurable). Smallest; removes the silent-corruption class.
2. **S2 — Size-budget reducer** + wire it into the LLM executor and run-record
   persistence (`_reduction`). Bounds prompts/records generally.
3. **S3 — Generic integration executor** + `Action.Params` + migration shim; rip
   out hardcoded `execGmailFetch`/`parseDigestEmails`/`execComposeDigest`.
4. **S4 — Builder-agent ownership**: spec `Params` in the builder's tools/prompt;
   `metadata_only` guidance; `ResultTooLargeError` self-heal loop.

S1+S2 are the "platform framework for large inputs." S3+S4 are "the agent owns
the integration specifics." S3 carries the wire-shape change.

## 10. Open questions (for triangulation)

1. Where should `Reduce`/`Budget` live — `internal/workflow`, or a new
   `internal/largeio` shared with non-workflow callers?
2. Should reduction be **structural only**, or should an oversize payload trigger
   an *LLM map-reduce* (summarize chunks → combine)? Map-reduce is powerful but
   adds nondeterminism + cost + latency to a single step.
3. Is `metadata_only` rich enough, or do we need a small per-provider capability
   registry (which actions support which lightweight flags)? Registry = more
   power, more maintenance.
4. Migration shim vs. forcing a one-time re-freeze of existing specs — is the
   shim worth the permanent special-case it reintroduces?
5. Should `ResultTooLargeError` auto-retry once with `metadata_only:true` at the
   platform layer (pragmatic) or strictly defer to the agent (clean)? Tension
   between "demo just works" and "agent owns policy."
6. Persisted run-record reduction: do we reduce before AppendRun (bounded files,
   lossy audit) or keep full + reduce only for prompts (full audit, unbounded
   files)?
7. Does `Action.Params` (free-form `map[string]any`) widen the attack surface for
   a prompt-injected builder agent to craft arbitrary Composio calls? Do we need
   an allow-list of `action_id`s per workflow, or does the existing approval gate
   on mutating actions already cover it (reads are ungated)?

## 11. Triangulation outcomes (revisions to this RFC)

Reviewed by 5 orthogonal lenses (architecture, perf/SRE, security, types, wire-
contract/determinism). Findings flagged by 2+ lenses are treated as high-
confidence and are now decisions; direct disagreements are escalated.

### 11.1 High-confidence decisions (≥2 independent lenses)

- **D1 — Kill the §6 migration shim. (architecture + security + wire-contract)**
  A load-time shim that rewrites a `gmail_fetch_emails`-shaped action reintroduces
  the exact "broker knows Gmail" smell, and mutating a frozen, shipcheck-proven
  spec outside the freeze boundary breaks the "executed == proven == persisted"
  identity (the run no longer executes the bytes shipcheck proved, with no
  version bump). **Decision:** no shim. Gmail defaults stay ONLY in the live-exec
  read path (where `isGmailFetchAction` already is), never in `LoadSpec`; the
  kernel stays a pure decode. The demo spec gets a one-time explicit re-freeze
  with real `Params` as the first commit of S3 (dog-foods the data-driven path).
  Resolves Q4 = re-freeze, not shim.

- **D2 — Response→prompt PROJECTION is a third responsibility the split missed,
  and it is a security hole, not just a coupling. (architecture + security)**
  The LLM step consumes a *normalized* shape (`sender/subject/snippet/important/
  needs_reply`) produced by `parseDigestEmails`, not raw Composio JSON. S3 cannot
  just delete that function: either the projection becomes spec-authored or the
  prompt sees `(no prior context)` and the empty-digest bug returns by another
  route. Worse, without a field allow-list the generic executor would serialize
  RAW provider bodies (Drive files, Slack DMs) into LLM prompts — an exfiltration
  vector the old 5-field projection prevented. **Decision:** add a declarative
  **field-projection / `expose_fields`** to the integration action, authored by
  the builder agent at freeze, applied by the platform before any data reaches an
  LLM prompt. This MUST land inside S3. The §4 split is now THREE concerns:
  transport/size (platform), provider params (spec/agent), response→prompt
  projection (spec/agent, platform-enforced).

- **D3 — Per-action output keys + retire the stringly-typed Output protocol.
  (types + wire-contract)** The flat merge (`runner.go:100-106`) means a bare
  `_reduction` key collides/overwrites when two actions reduce, silently losing
  one — violating the "honest, not silent" goal. **Decision:** namespace per
  action (`a.ID+"_reduction"`), matching the existing `a.ID+"_sent"` convention.
  Further (types, "biggest rot risk"): the whole `ActionOutcome.Output
  map[string]any` convention (`digest`/`summary`/`<id>_note`/`_reduction`) is an
  implicit executor↔UI protocol that already forced `coerceEmails`. **Decision:**
  introduce a typed `ActionOutput` struct (well-known slots + a raw `extra` bag)
  before S2/S3 pile more magic keys on. `_reduction` becomes a typed field.

- **D4 — Lock the determinism invariant with a TEST before S3.
  (wire-contract + architecture)** The invariant (framework runs only in the live
  exec; shipcheck uses `recordingExec`) holds structurally today but nothing
  forbids a future edit from breaking it, and `adapter_parity` is blind to
  `Params`. **Decision (blocking S3):** a test that runs `Shipcheck` on a spec
  whose action carries `Platform`/`ActionID`/`Params` against a *poison*
  ActionExec/Composio seam that fails if ever invoked, asserts it is never called,
  and asserts the report is byte-identical with and without `Params`.

- **D5 — `ResultTooLargeError` is fail-loud; the agent (not the platform) reacts.
  (wire-contract + architecture + types)** A platform auto-retry with
  `metadata_only` mutates params mid-run, so the `RunRecord` is no longer
  reproducible from `(spec, events)` — breaking the kernel's core promise — and
  re-creates policy-in-platform. **Decision:** resolve Q5 = defer to the agent.
  An oversize read returns `OK:false` → `action_failed` audit → halts the chain
  (correct); the reason rides a structured `Output` field (not just `Err`) so the
  UI can distinguish too-large from needs-approval. The self-heal loop amends
  `Params`/`expose_fields` and re-freezes. "Demo just works" is served by the
  frozen live-exec default (D1), not a runtime retry. Required test: oversize read
  → `action_failed`, no state advance, no phantom `_sent:true`.

- **D6 — Per-workflow integration-read ALLOW-LIST. (security CRITICAL + types)**
  The generic executor runs `deterministic`+`Platform/ActionID` reads UNGATED;
  the approval gate only fires on `ActionExternal`, and broker-token=host-trust
  bounds *who reaches the endpoint*, not *which `action_id` runs*. A prompt-
  injected builder agent could read any connected platform. **Decision (MUST land
  in S3):** the spec carries an explicit `allowed_read_actions`
  (`[{platform,action_id}]`) blessed by the human at freeze; the executor rejects
  any read not on it before calling Composio; `Validate()`/freeze require it to be
  non-empty for any spec with integration reads. Plus a `ValidateParams` hook
  (JSON-round-trippable; typed required fields for known action_ids).

### 11.2 Perf/SRE bounds (adopted)

- `readCapped` must **stream-detect** overflow (read through `LimitReader(r,
  limit)` + a 1-byte probe), never buffer `limit+1` — caps per-read allocation at
  exactly `limit`.
- Default read cap **4 MiB** (configurable up to 32 MiB per-action), NOT a 32 MiB
  global default — `N runs × M agents × 32 MiB` is unbounded.
- Reduce the run record before `AppendRun` to a **256 KB** budget (`MaxItems 50`,
  `StringCap 500B`); raise the `ReadRuns` scanner ceiling to match so a fat line
  can't silently truncate (the original bug class at the read-back layer).
- Add a **per-spec run semaphore** (serialize manual + scheduled + event
  triggers), an **aggregate run wall-clock timeout** (~5 min) on top of the 90 s
  per-llm-step timeout, a **max-8-llm-steps-per-run** cap, and a
  **last-100-runs** look-back window for `ProposeOverlays`.

### 11.3 Cut as over-engineering (architecture, concurred)

- Q2 LLM map-reduce reducer — CUT (contradicts non-goals; injects nondeterminism
  into a step). §5.2 is structural-only, stated as the decision.
- Q3 per-provider capability registry — CUT for v1. `metadata_only` is a best-
  effort hint, no-op when unknown; the self-heal loop is the mechanism. Resolves
  Q3.
- Q5 platform auto-retry — CUT (see D5).

### 11.4 ESCALATED disagreements (need a human call)

- **E1 — Where does `Reduce`/`Budget` live? (Q1)** Architecture: keep in
  `internal/workflow`, no new package (YAGNI; one caller; "no new packages unless
  truly shared"). Types: new `internal/largeio` (keeps `internal/workflow` a pure
  state-machine with no I/O imports; clean dep direction
  `workflow→largeio`, `action→largeio`). Both agree the *transport* piece
  (`readCapped`/`ResultTooLargeError`) belongs in `internal/action`. Lean:
  `internal/workflow` now (Reduce over `json.RawMessage` is pure, no I/O — it does
  not violate purity), extract to `internal/largeio` only if a second non-workflow
  caller appears. Flagged for the owner.

- **E2 — Reduce the persisted run record, or only the prompt? (Q6)** Architecture:
  reduce for PROMPT only, keep full record (transport cap bounds it), because a
  lossy data-dependent record makes the self-heal miner mistake `_reduction` for a
  recurring workflow fault. Perf/SRE: reduce BEFORE `AppendRun` (256 KB), because a
  32 MiB record is an OOM/file-growth hazard and `ReadRuns`' 8 MiB scanner
  silently truncates fat lines (reintroducing the bug). **Synthesis (proposed):**
  reduce the record (perf) AND make `ProposeOverlays` explicitly ignore platform
  markers (`*_reduction`, `ResultTooLargeError`) so the miner never treats
  platform truncation as a workflow signal (architecture's concern). Confirm.
