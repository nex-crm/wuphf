# Deterministic External Integrations — Connection Lifecycle State Machine

> ▶ RESUME HERE: ALL SLICES DONE — 1, 2, 3a, 3b (Connect card + hard timeout),
> 4a, 4b (structured payload + real envelope + connection-unverified), 5a, 5b
> (grant button + revoke list), 6. Draft PR #1049. The ONLY remaining gate before
> marking ready: triangulate the new wire shapes (connect/fallback decision
> kinds, humanInterview.Platform/LogoURL/action/connection_unverified,
> actionGrant) per repo CLAUDE.md + full-app screenshots via publish.sh. Full
> design rationale in the office-hours design doc (gstack projects dir,
> 2026-06-07).

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
- [x] **Slice 3b (web Connect card)** — `ConnectIntegrationCard.tsx`: a
  `connect`-kind blocking card with the integration logo + "Connect <Platform>"
  + the agent's reason; Connect drives the shipped Composio OAuth
  (`startIntegrationConnection` → `window.open(auth_url)` → poll
  `getIntegrationConnectStatus`). Each poll hits `/integrations/connect-status`,
  which fires the backend `fanOutConnected` that auto-answers this card — so on
  connection the card just refetches `["requests"]` and disappears, parked
  action resuming with no second prompt. Waiting + failed states; Skip answers
  `skip`. Wired into `HumanInterviewOverlay` (`connect` kind). 3-theme verified
  (`/tmp/connect-{light,dark,noir}.png`). Tests + story co-located.
- [x] **Slice 3b (hard timeout)** — `expireStaleIntegrationDecisionsLocked`
  auto-cancels connect/fallback cards older than `integrationDecisionTimeout`
  (60m) and audits `integration_connect_timed_out` /
  `integration_fallback_timed_out`, freeing the blocking channel. Hooked into the
  per-minute `runActivityWatchdog` tick so it fires regardless of who is polling.
  NOTE: "task back to backlog" reduces to cancel + audit — the connect flow does
  not park a task (the agent already got its tool error when blocked), so there
  is nothing to re-queue; the realized behavior is unblock + audit. The human can
  always Skip sooner; this is the backstop. Test: fresh card kept, stale card
  expired + audited.
- [x] **Slice 4a** — `ExternalActionApprovalCard` (web), reading the legacy
  approval-context parse. The Go side embeds `<action_id> via <Platform>` in the
  `Action:` footer and `<verb> via <Platform>` in the title, so the card
  recovers the platform, action id, verb, account, channel, why, and payload
  summary from `parseApprovalContext` with NO Go change. Layout: integration
  logo tile + platform eyebrow + verb headline + mono action_id; a "Why" rule
  with the agent's intent; an inset "What will be sent" panel with the
  secret-masked payload fields and a **Show/Hide raw** toggle (the raw view is a
  reformat of the SAME masked fields — never a new data source); a connected-
  account dot + channel meta; actions Approve / Approve & always allow / Reject /
  Dismiss. Token-only, verified across Nex Light, Nex Dark, and Noir Gold
  (Storybook screenshots /tmp/eac-{light,dark,noir}.png — gold theme auto-themes
  the accent rule + primary button). Wired into `HumanInterviewOverlay`
  (approval kind only; everything else keeps the generic interview body). Files:
  `web/src/components/messages/ExternalActionApprovalCard.tsx` (+ `.test.tsx`,
  `.stories.tsx`), `HumanInterviewOverlay.tsx`/`.test.tsx`, `web/src/api/client.ts`
  (AgentRequest +platform/logo_url, grant client fns), `web/src/styles/global.css`
  (`.eac-*`). tsc clean, biome clean, 214 messages tests pass, web build green.
- [x] **Slice 4b** — structured action-approval payload with the real masked
  HTTP envelope behind the raw toggle. New wire shape `humanInterview.action =
  {platform, action_id, verb, name, logo_url, account, raw_envelope{method, url,
  headers, data}}` + `connection_unverified`. The gate decodes the resolver's
  already-masked envelope (built for the `approve` decision) and threads it onto
  the approval request via `requireTeamActionApproval`; the broker **re-masks on
  store** (`sanitizeApprovalActionPayload`) and strips the internal connection
  key — defense in depth on the surface a human reads. The card prefers the
  structured payload for identity + shows the real envelope behind the raw
  toggle, falling back to the legacy parse when absent. Review **LOW #5** shipped
  with it: when the gate could not reach the resolver and degraded to
  approval-only, `connection_unverified` raises a "connection unverified" warning
  on the card. Files: `broker_approval_action.go` (new) + `broker_types.go` +
  `broker_requests_interviews.go` (store), `internal/teammcp/action_resolve_gate.go`
  + `actions.go` (thread), `web/src/api/client.ts` + `ExternalActionApprovalCard.tsx`
  + CSS. Tests: broker storage+mask, gate threading, card structured/envelope/
  unverified. Full Go suites green; 229 web tests green; 3-theme verified.
- [x] **Slice 5a** — Scoped grants, backend. Persisted `actionGrant{id,
  agent_slug, platform, action_scope, channel?, issue_id?, granted_by,
  granted_at, expires_at?, revoked_at?}`; the scope is ALWAYS a concrete
  action_id (wildcards rejected at the endpoint). The resolver evaluates
  `hasActiveActionGrant(agent, platform, action_id)` (exact match on all three,
  case-insensitive; expired/revoked/unparseable-expiry fail closed) for mutating
  actions and, when connected + granted, returns `proceed`. The gate
  (`actions.go`) splits `proceed` (granted → skip the approval modal) from
  `approve` (→ modal); `requireTeamActionApproval` gained a `preApproved` param
  and short-circuits AFTER the Issue/drafting gate (a grant for the integration
  can NEVER bypass an Issue still awaiting approval) with a synthetic `grant`
  audit marker so the run stays visible. CRUD via `GET/POST
  /integrations/grants`. **AUTH-MODEL FINDING (load-bearing):** the broker token
  is the host-trust boundary — the local owner's web app AND the MCP server both
  use it (broker kind); human-SESSION actors are shared-link guests that
  `withAuth` 403s off non-allowlisted routes. So a "require human kind" gate is
  BACKWARDS (rejects the owner). The real control that an agent cannot self-grant
  is that NO MCP tool reaches `/integrations/grants` — agents act only through
  the fixed teammcp tool surface. Files: `broker_action_grants.go` (new),
  `broker.go`/`broker_types.go`/`broker_persistence.go` (persist), `broker_
  integrations_resolve.go` (eval), `internal/teammcp/actions.go` (preApproved +
  bypass). Tests: `broker_action_grants_test.go` + a teammcp grant-bypass test.
  Persisted wire shape → triangulate before merge.
- [x] **Slice 5b** — Grant UI, complete. (1) The approval card's "Approve &
  always allow" button mints a grant via `createActionGrant(agent, platform,
  action_id, channel)` then approves; grant-write failure still approves once.
  (2) `ActionGrantsPanel.tsx` ("Always-allowed actions") in the Integrations app
  home: lists every active grant (logo + mono action_scope + agent + platform +
  since-date) with a Revoke button (`revokeActionGrant` → invalidate); renders
  nothing when there are no grants. Tests co-located.
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
