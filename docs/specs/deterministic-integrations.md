# Deterministic External Integrations — Connection Lifecycle State Machine

> ▶ RESUME HERE: Build order below. Slices 1, 2, 3a, and 6 are DONE (backend
> deterministic spine + `connect` raise/fan-out + `fallback` handoff kind).
> Remaining backend: slice 5 (scoped-grant record + resolver `HasGrant` eval)
> and slice 3b's hard connect-timeout→backlog. Then ONE /frontend-design session
> for the web surfaces: slice 3b Connect card, slice 4 ExternalActionApprovalCard,
> slice 5 grant modal + revoke UI. Each slice ships behind the prior,
> E2E-verified. Full design rationale in the office-hours design doc (gstack
> projects dir, 2026-06-07).

## Why

External integrations fire indeterministically. The action gate
(`internal/teammcp/actions.go` — `requireTeamActionApproval` →
`handleTeamActionExecute`) executes with **no pre-flight connection check**: a
missing/expired connection only surfaces as a tool error *after* the human
approved. And `internal/action/registry.go` `preferredProvidersFor` returns
`["one", "composio"]`, so a Composio-connected action can misroute to One CLI.
The product is Composio-only but execution routing is not.

Most plumbing already shipped (PR #1001 + `fa744955`): the Composio catalog,
`/integrations/connect` + `/connect-status` + `/disconnect` + `/audit`
endpoints (`internal/team/broker_integrations.go`), the web Integrations app
with `window.open(auth_url)` OAuth + 2s polling, and logos. We extend it.

## Architecture

**ConnectionResolver** (`internal/action/resolver.go`, new): provider-aware
service owning connection state + action classification.

State machine (per platform, per workspace):
`unknown → checking → {connected | pending | missing | failed | unsupported | indeterminate}`.
`indeterminate` = the probe CALL failed (Composio API unreachable), distinct
from `missing`/`failed` — never downgrade an outage to `connect`.

Classification — `Resolve(agent, platform, action_id, args) → Decision`:

| Condition | Decision |
|---|---|
| read-only action | `proceed` (no human) |
| mutating + connected + covered by grant | `proceed` (grant auto-approve, re-checked pre-execute) |
| mutating + connected + no grant | `approve` (dedicated modal) |
| mutating + missing\|failed (supported) | `connect` (typed connect decision → OAuth → re-resolve) |
| mutating + pending | `wait` (attach to existing connect decision; no new card) |
| mutating + unknown\|checking | `wait` (block briefly, re-resolve when probe settles) |
| mutating + indeterminate | `fail-safe` (last-known-good `connected` within TTL; else block-with-retry; never `connect`) |
| mutating + unsupported | `fallback` (manual handoff) |

A non-`connected` state can never reach `provider.ExecuteAction`.

**Connection registry** (persisted map in broker state, NOT a projection over
the action log — `ledger.go` `b.actions` is a 150-entry in-memory ring reset on
restart). Keyed platform → `{state, connection_key, account_name, health,
last_verified_at}`. Refreshed by probe (`ListConnections` /
`GetIntegrationConnectionStatus` → `connectionState()`) + connect/disconnect
events. Rides the existing broker save path; rebuilt-by-probe on cold start.

**`POST /integrations/resolve`** (broker): the MCP gate calls it with full
action args; returns the `Decision` + structured render payload. Full args (not
a digest) cross the wire so the resolver can build the preview `raw_envelope`
via `ExecuteAction{DryRun:true}` (returns the request envelope without sending).

**New decision kinds** (`broker_requests_interviews.go`: `normalizeRequestKind`,
`requestOptionDefaults`, `requestNeedsHumanDecision`):
- `connect` — options `[connect, skip]`. Dedupe on `(platform, workspace)`; on
  registry → `connected`, ALL parked tasks re-resolve. Honors
  `actionApprovalTimeout` → task back to backlog + `integration_connect_timed_out`.
- `fallback` — options `[mark_done, skip]`. `mark_done` records human-completed.

**Structured action-approval payload** (replaces regex `parseApprovalContext`):
`request.action = {platform, action_id, verb, logo_url, account{name,key},
summary, details[], raw_envelope{method,url,headers_masked,body_masked}}`. Keep
legacy string for back-compat. New wire shape → triangulate.

**ExternalActionApprovalCard** (frontend, /frontend-design): logo + name; verb +
raw action_id; why; payload summary (secrets redacted); **raw-payload toggle**
(masked); friendly account; channel. Actions: Approve / Approve & grant scope /
Reject.

**Scoped grants** (persisted): `actionGrant{id, agent_slug, platform,
action_scope, channel?, task_id?, granted_by, granted_at, expires_at?,
revoked_at?}`. Button writes the specific `action_id` (no `*`). Re-checked
immediately before `proceed` (TOCTOU window accepted). Revocable from the
Integrations app.

**Provider routing**: flip `preferredProvidersFor` to Composio-first. Retiring
`one.go` is a follow-up (quarantine this PR).

## Build order

- [x] **Slice 1** — `ConnectionResolver` + persisted registry +
  `/integrations/resolve` (backend, unit-tested incl. indeterminate/wait/fail-safe).
  Commits `d291ebb4` (core) + `cdf016ee` (registry + endpoint).
- [x] **Slice 2** — Wire the gate (`internal/teammcp/actions.go` +
  `action_resolve_gate.go`): a mutating action is classified before approval/
  execute; connect/wait/fail_safe/fallback block the provider call; proceed/
  approve inject the resolver-verified connection key. Provider routing flipped
  Composio-first (`registry.go`). **Decisions:** (a) only MUTATING actions hit
  the gate — read-only bypasses it to avoid doubling provider calls on the hot
  lookup path; the resolver still *supports* gating reads (`Classify`), it is
  just not wired on that path. (b) If `/integrations/resolve` is unreachable the
  gate degrades to the existing human approval gate rather than bricking all
  actions. **Follow-up:** resolver re-probes Composio on every mutating action;
  add a "skip probe when registry entry is fresh" path for hot-path latency.
- [x] **Slice 3a** — `connect` decision kind + raise/dedupe + connection
  fan-out (backend). `requestOptionDefaults("connect")` → `[connect, skip]`;
  `requestNeedsHumanDecision` treats `connect` as a blocking human decision (the
  user's "block on a typed Connect decision" call). The resolver raises ONE
  blocking Connect card per platform (workspace-wide dedupe via
  `connect:<platform>`) on a `connect` decision and returns its `request_id`;
  the gate surfaces that card to the agent instead of telling it to retry. When
  `/integrations/connect-status` observes the OAuth completion,
  `fanOutConnected` flips the registry to `connected`, auto-answers the open
  card (`choice=connect`), and runs the standard unblock cascade so the parked
  action resumes with zero re-asking. Disconnect flips the registry to
  `missing`. New typed `humanInterview.Platform`/`LogoURL` fields anchor the
  card to a toolkit (additive wire-shape extension → triangulate pre-merge).
  Files: `broker_integrations_connect.go` (new), `broker_requests_interviews.go`,
  `broker_types.go`, `broker_connection_registry.go` (extracted
  `upsertConnectionRegistryLocked`), `broker_integrations_resolve.go` (+
  `request_id`), `broker_integrations.go` (connect-status fan-out + disconnect),
  `internal/teammcp/action_resolve_gate.go` (+ `request_id` in block message).
  Tested: `broker_integrations_connect_test.go` (decision kind, raise+dedupe,
  fan-out resume + idempotence, connect-status E2E).
- [ ] **Slice 3b** — web Connect card (reads `humanInterview.Platform`/`LogoURL`,
  drives the shipped `IntegrationsApp` `window.open(auth_url)` + 2s poll; the
  backend fan-out auto-resolves on completion, so the card is mostly wiring) +
  hard connect-timeout → task back to backlog + `integration_connect_timed_out`
  audit (the connect card currently rides the standard reminder/follow-up
  lifecycle; a hard timeout-to-backlog is a separate scheduler-tick concern,
  deferred from 3a to keep the commit atomic).
- [ ] **Slice 4** — `ExternalActionApprovalCard` reading legacy parse first;
  (4b) swap to structured payload (the long pole; triangulate).
- [ ] **Slice 5** — Scoped grants (record + modal action + resolver eval +
  revoke UI). De-scope candidate if the PR runs long.
- [x] **Slice 6** — `fallback` manual-handoff decision kind (backend; done
  early, alongside 3a, since it mirrors the connect card). On a `fallback`
  decision (platform has no Composio path) the resolver raises a blocking
  handoff card — options `[mark_done, skip]` — scoped to `(platform, action_id)`
  so retries collapse but distinct action types each get a card. No fan-out:
  the human answers via the normal decision path, which runs the standard
  unblock cascade. The card-mint path is shared with connect via
  `ensureIntegrationDecisionLocked` (one mint path, no drift). Files:
  `broker_integrations_fallback.go` (new), `broker_integrations_connect.go`
  (extracted shared mint helper), `broker_requests_interviews.go`,
  `broker_integrations_resolve.go` (+ ensure-call on fallback),
  `internal/teammcp/action_resolve_gate.go` (+ card ref in block message).

## Verification

- `bash scripts/test-go.sh ./internal/action ./internal/team ./internal/teammcp`
  (internal/team needs `-timeout 900s`).
- `bash scripts/test-web.sh` for web slices.
- E2E: missing-connection → connect → resume → approve, with zero re-asking;
  revoked-token → `failed` → `connect`; Composio-down → fail-safe not connect;
  grant suppresses in-scope repeats + revocable; unsupported → manual handoff.
- Unit: no mutating action reaches `ExecuteAction` without `connected`; a
  Composio-connected platform never routes to One.
