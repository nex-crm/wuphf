# Product Analytics & Session Replay

Status: APPROVED (2026-06-13) — implementation in progress on `feat/product-analytics-tracking`.

This is the source of truth for WUPHF's product analytics. It records what we
track, what we deliberately do **not** track, how consent works, and the
privacy guarantees we make to operators and end users.

## Goal

Understand how the **human operator** uses WUPHF so we can drive product
development: where activation succeeds or stalls, whether people delegate work
and come back, how they exercise the human-oversight gates (the load-bearing
trust surface of a multi-agent product), and whether the knowledge loop
compounds. Not over-tracked, not under-tracked — every event maps to a product
question, and rich properties let one event answer many.

## Decisions (approved)

1. **PostHog SDK adopted.** We add `posthog-js`, lazy-loaded, autocapture OFF.
   This supersedes the prior "no SDK ever" note in `lib/analytics.ts`; the
   reasons and the privacy guarantees below replace it. Documented in the
   README so the policy change is honest and visible.
2. **Two independent toggles, both default ON, both dormant without a key:**
   - **Product analytics** (anonymous usage events) — `analytics_telemetry_enabled`
   - **Session recording** (fully-masked replays) — `analytics_session_recording_enabled`
   Honest separation: a user can keep usage events on while turning recording
   off, or vice-versa.
3. **Strict replay masking.** All text and all inputs are masked in every
   recording (`maskAllInputs: true`, `maskTextSelector: "*"`). Recordings show
   layout, cursor, clicks, scroll, rage-clicks, and navigation — never readable
   content, customer data, agent output, or secrets. Stated plainly in the
   toggle copy and the README to win trust.
4. **Dormant by default, preserved.** No PostHog project key configured → every
   analytics call is a no-op. Stock source builds and forks never phone home.
5. **Full Tier 1–3 event taxonomy** (below).

## Privacy model

- **No PII except a consented email.** The only personal datum that ever leaves
  is the onboarding email, attached to the PostHog person exactly once at
  finish and only when the user left the keep-in-touch box checked — unchanged
  from the prior design.
- **Anonymous identity.** `distinct_id` is a random device id in localStorage,
  not derived from any user data. Workspaces are grouped via a PostHog group
  keyed by a **hashed** workspace id (never the raw id, never the name).
- **No content.** Event properties carry shapes and buckets (e.g.
  `length_bucket`, `mention_count`, `has_details`), never message text, wiki
  bodies, task titles, customer names, or secrets.
- **Strict-masked recordings** (decision 3).
- **Operator control.** A self-hosted operator can point at their own PostHog
  (`WUPHF_POSTHOG_KEY` / `WUPHF_POSTHOG_HOST`) or disable either channel at
  runtime via the toggles — no rebuild needed.

## Architecture

- **One analytics module** (`web/src/lib/analytics.ts`, extended in place):
  - Lazy `posthog-js` init, gated on a resolved key AND the telemetry toggle.
  - Typed `track(event, properties)` over a `PRODUCT_EVENTS` union.
  - `identifyWorkspace(hashedId, groupProps)` for group analytics.
  - Session recording started only when the recording toggle is on; strict
    masking config applied at init.
  - Dormant no-op path preserved; the 3 existing onboarding events keep working
    through the same module.
- **Key + flag injection (self-hosted aware).** Build-time
  `VITE_PUBLIC_POSTHOG_KEY` / `VITE_PUBLIC_POSTHOG_HOST` remain the default for
  Nex-shipped builds. The loopback `/api-token` endpoint is extended to also
  return an `analytics` block `{ configured, telemetry_enabled,
  session_recording_enabled, posthog_key, posthog_host }` so a self-hosted
  operator's env + the toggles take effect at runtime. Frontend prefers runtime
  values, falls back to build-time.
- **Bundle:** `posthog-js` is imported dynamically so the initial bundle is
  unaffected; the rrweb recorder loads on demand only when recording is active.
- **Pageviews:** wired manually to TanStack Router route changes (SPA), since
  autocapture is off. `$pageview` / `$pageleave` give surface reach and
  time-on-surface for free.

## Event taxonomy

Properties never contain content. `actor=human` is implied (these are
human-initiated UI events).

### Tier 1 — Core loop
| Event | Properties |
|---|---|
| `onboarding_started` | — |
| `onboarding_step_completed` | `step_id`, `step_index` |
| `onboarding_completed` | `blueprint_id`, `agent_count`, `skipped_first_task`, `telemetry_consent`, `recording_consent` |
| `task_created` | `source` (home/inline/subtask/onboarding), `owner_agent`, `provider`, `model`, `effort`, `start_mode` (start/backlog/routine), `has_details` |
| `task_status_changed` | `action` (release/complete/cancel/reopen/reassign/block/resume/archive) |
| `decision_submitted` | `action` (approve/request_changes/reject/defer), `surface` (inbox/packet/task_detail), `has_comment`, `is_plan` |
| `interview_shown` | `surface` (interview_bar/inbox) |
| `interview_answered` | `surface`, `has_custom_text` |
| `message_sent` | `channel_type` (channel/dm/task), `mention_count`, `is_reply`, `length_bucket` |
| `action_failed` | `action`, `status` |

### Tier 2 — Knowledge & expansion
| Event | Properties |
|---|---|
| `wiki_article_viewed` | `path_depth`, `source` |
| `wiki_article_edited` | `is_create`, `had_conflict` |
| `notebook_entry_promoted` | `reviewer_assigned` |
| `review_action` | `action` (approve/request_changes/reject/resubmit/comment), `target` (notebook) |
| `agent_created` | `source` (wizard/generate), `from_blueprint` |
| `skill_state_changed` | `action` (enable/disable/approve/reject/archive/restore/enable_for_agent/edit), `scope` (global/agent) |
| `routine_created` | `schedule_type` (cron/interval) |
| `channel_created` | `kind` (channel/dm/generated) |
| `integration_action` | `action` (connect/grant/revoke), `platform` |

### Tier 3 — Funnel detail & diagnostics
| Event | Properties |
|---|---|
| `onboarding_email_viewed` | — (existing) |
| `onboarding_email_started` | — (existing) |
| `onboarding_email_captured` | `$set.email` (consented; existing) |
| `onboarding_blueprint_selected` | `blueprint_id`, `agent_count`, `start_from_scratch` |
| `analytics_consent_set` | `channel` (telemetry/recording), `enabled`, `surface` (onboarding/settings) |
| `app_error` | `boundary`, `message_bucket` |
| `$pageview` / `$pageleave` | `route` (SDK; manual on route change) |

### Deliberately NOT tracked
Raw message/wiki/task content; message reactions; inbox-cursor pings; every
sidebar click; keystrokes; search query text; memory KV writes; and **all
agent-execution internals** (turns, tokens) — that is observability, not
human-behavior product analytics, and would multiply volume.

## Consent surfaces

- **Onboarding wizard:** two toggles on the final step, both default ON, with
  one-line honest copy ("anonymous usage" / "fully-masked recordings"). Choices
  ride `/onboarding/complete` and persist to config.
- **Settings:** the same two toggles, mirrored, so consent is revocable any
  time. Changing either fires `analytics_consent_set` and updates `/config`.
- Backend config fields (`*bool`, nil = default ON for legacy installs):
  `analytics_telemetry_enabled`, `analytics_session_recording_enabled`.

## PostHog dashboards (built via MCP after instrumentation)

1. **Activation funnel** — started → step_completed → completed → first task started.
2. **Core-loop engagement** — tasks created/started, DAU/WAU, week-N retention.
3. **Oversight & trust** — approve vs reject vs request_changes rates, decision
   latency, interview shown→answered abandonment.
4. **Knowledge compounding** — wiki reads/edits, promotions, review outcomes.
5. **Friction** — `action_failed` + `app_error` by surface.
6. **Product Health** — top-level overview tile set.

## Implementation phases

- [x] **P1 Backend** — config fields + resolvers (`IsAnalytics*`,
      `ResolvePostHog*`), `/config` GET+POST, `/onboarding/complete` body,
      `/api-token` analytics block; Go tests (config + team + onboarding green).
- [x] **P2 Frontend core** — `posthog-js@1.386` (dynamic-import, code-split into
      its own chunk so dormant builds ship 0 bytes), analytics module rewrite
      (lazy init, typed `track`/`trackOn`, workspace group, strict-mask
      recording, live `setAnalyticsConsent`), dormant-default + existing
      onboarding events preserved; analytics.test.ts 17 green.
- [x] **P3 Instrumentation + toggles** — events wired at the API-client layer
      (task/decision/message/interview/wiki/notebook/skill/channel/routine/
      integration) + component layer (onboarding funnel, interview_shown,
      wiki_viewed, agent_created, app_error, action_failed via MutationCache,
      route pageviews); two onboarding toggles (StepFirstIssue) + two Settings
      toggles (Privacy & Analytics section). tsc clean, full web suite green.
- [ ] **P4 Dashboards** — build the dashboards via PostHog MCP (project Prod
      185073). Insights reference approved event names; populate once a keyed
      instance runs.
- [ ] **P5 Verify + ship** — README + policy updated (done); screenshots of the
      consent surfaces; draft PR. Live event/replay verification needs a keyed
      running instance.

## Verification commands

```bash
go build -o wuphf ./cmd/wuphf
bash scripts/test-go.sh ./internal/team ./internal/config ./internal/onboarding
cd web && bunx tsc --noEmit && bun run build
bash scripts/test-web.sh web/src/lib/analytics.test.ts
```
