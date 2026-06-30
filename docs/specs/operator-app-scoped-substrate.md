# Operator app-scoped substrate (reshape)

Branch: `operator/s7-app-build-quality` (off merged `main` @ a8ed33c8).
Decision (2026-06-30, founder): **the App is the first-class scope.** Stop
threading office-task primitives (task id, channel, CEO, governor) through the
operator app-build path. The operator FE addresses everything by **app id**;
task ids become a broker-internal detail.

## Why (the muddle this kills)

The operator app-builder is grafted onto the office-task substrate. "Build an
app" creates an office task (`OFFICE-N`) in a channel (`task-office-N`) owned by
the app-builder, which drags in the CEO (killed), the governor (disabled), and
the notifier. Build activity streams over `agent-stream/{slug}?task={taskId}` —
scoped by office task id. A refine returns an **edit channel** (`task-<id>`),
a *different* identifier than the stream's task scope. Five symptoms
(CEO-kill, governor-disable, task-id↔channel mismatch, connect card in the wrong
place, flaky refine stream) are one wrong shape. Fix the shape, not the symptoms.

## Target shape

An **App** is the unit. It owns its parts, scoped inside it: **UI**, **Workflow**,
**Data**, **Knowledge**, plus versions and a build/edit conversation. Building or
editing is an **app-scoped agent run**, addressed by `app_<id>`, streamed by app,
surfaced in the app's own chat. No office task, channel, CEO, or governor in the
app path. The FE keys off **one** identifier (the app), not three.

## Grounding facts (current backend)

- `CustomApp.EditChannel` (custom_app.go:91) already binds an app to its
  app-builder run's channel (`task-<id>`). This is the app→run mapping.
- `agent-stream/{slug}?task={taskId}` (broker_sse.go:269) scopes by **task id**;
  events are tagged with the task id. Office-side `AppBuildPreview` passes the
  task's `id` to `TaskActivity`.
- Build trigger today: FE `requestAppBuild` → `POST /tasks {owner:app-builder}`
  → broker creates an office task → dispatches the app-builder run → pre-scaffold
  (`maybePrescaffoldAppForCreate`) mints `app_<id>` → run publishes via
  `register_app`.
- The app-builder run is already detached from the CEO (notifier_targets.go gate)
  and the governor (disabled by default).

## Plan (slices, each ships a test + live verify)

### S1 — backend: app-scoped activity stream  ← START HERE
Add `GET /apps/{id}/activity` (SSE). Resolve the app → its backing app-builder
run (via `EditChannel` → the app-builder task whose `Channel == EditChannel` →
that task's id), then stream that run's agent events using the existing
`subscribeTaskWithRecent` machinery. Task id stays internal; the FE only sees
`/apps/{id}/activity`. Returns an empty/closing stream when the app has no active
run. Test: an app with a backing app-builder task streams its tagged events;
unknown app → 404; app with no run → empty replay then idle.

### S2 — frontend: AppActivity (app-scoped) + wire into the build chat
New `AppActivity({ appId })` mirroring `TaskActivity` but subscribing to
`/apps/{id}/activity` (app-scoped; no task id). Render it in `AppBuilderChat`'s
building state and in `OperatorAppDetail`'s building UI tab, so the thinking +
tool-call trace shows live during build AND refine. `AppBuilderChat` captures the
new app's id (already resolved by name→id correlation) and feeds `AppActivity`.
No task ids in the operator FE. Regression test: building state renders
`AppActivity` scoped to the app id (fails pre-S2).

### S3 — backend+frontend: app-scoped build trigger
`POST /apps/{id}/build { description }` starts a build run for a pre-scaffolded
app (name→id is deterministic), replacing the operator's `POST /tasks`. FE
`useBuildApp` calls it. The office-task creation becomes a broker-internal detail
of this endpoint (or is removed for app builds). Office work keeps `/tasks`.

### S4 — connect/approval cards as app events
Surface the Gmail-connect card and mutation-approval cards inside the app's chat
(app-scoped), not only in the app's UI tab. Reuse the operator `ApprovalCard`.

## Out of scope / retire later
- Office-task dispatch for app builds (S3 hides it; a later pass can remove it).
- Per-part scoping of the stream (UI vs Workflow vs Data) — nice-to-have once the
  app-scoped stream lands.

## Verification per slice
Rebuild `web/dist` + binary (the broker embeds dist via go:embed — `bun run build`
then `go build`), restart the sandbox broker, build the daily-digest app through
the operator chat, and confirm the live thinking/tool trace streams in the chat
scoped to the app. Plus Go + Vitest regression tests.
