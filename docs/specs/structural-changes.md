# WUPHF Structural Changes — Running Tracker

> Living doc for a series of deep structural changes to how WUPHF works.
> Changes arrive one at a time from the user. We capture each as a numbered
> entry with its requirement, the context we discovered, the decisions made,
> the tasks arising, and the final disposition. This file is the source of
> truth across context resets — read it top to bottom on session resume.

## Session setup

- **Worktree:** `/Users/najmuzzaman/Documents/nex/WUPHF/.claude/worktrees/structural-changes`
- **Branch:** `worktree-structural-changes`
- **Base:** `origin/main` @ `46f06e54` (`feat(inbox): add Needs action filter as default tab (#1012)`)
- **Started:** 2026-06-02
- **Mode:** one change at a time, user-driven. Do not batch ahead of the user.

## Conventions

- Each change gets a `## Change N — <title>` section below, newest at the bottom.
- Within a change: **Requirement** (verbatim/paraphrased ask) → **Context**
  (what the code currently does, files involved) → **Decisions** → **Tasks**
  (checklist) → **Status** (`PLANNING` / `IN PROGRESS` / `DONE` / `DEFERRED`).
- Keep `docs/specs/` other docs in sync if a change invalidates them.
- Repo commands (run from worktree root unless noted):
  - Go build: `go build -o wuphf ./cmd/wuphf`
  - Go tests: `bash scripts/test-go.sh` (or scoped: `bash scripts/test-go.sh ./internal/team`)
  - Web tests: `bash scripts/test-web.sh` (Vitest; never `bun test` inside `web/`)
  - Web build: `cd web && bun run build` (broker embeds `web/dist` at build time)
  - Type check: `cd web && bunx tsc --noEmit`
- Hard rule reminder: broker embeds `web/dist` at build time — always
  `bun run build` before rebuilding the binary when verifying UI changes.

## ▶ RESUME HERE — current state (2026-06-03)

**Read this section first on session resume, then the Change log below.**

- **Branch:** `worktree-structural-changes`. **HEAD:** `4a1ef8bf`. Base `origin/main` @ `46f06e54`.
- **Commits so far:** …Phase 0–2 · `5e43ceb3` Phase 3a · `d5b10eb8` Phase 3b · `35012a1d` Phase 3c ·
  `9473517c` teammcp fix · `b5faabb8` tracker · `96a48401` **per-task runtime + backlog/Auto (backend)** ·
  `4a1ef8bf` **same (frontend)**. All green, all committed.
- **PHASE 3 REVISION DONE (per-task runtime + backlog/Auto):** the LLM model/provider is now a
  property of the TASK, not the agent (teamTask gains `provider`/`model` next to `effort`; dispatch
  prefers task runtime over the owner's soft-default binding via `effectiveProviderKindForAgent`/
  `taskModelForKind`, cross-kind isolated; threaded through create paths + team_task/team_plan MCP).
  Composer sends provider/model/effort ON THE TASK (no more `POST /office-members` binding mutation;
  agent picker stays a soft default). Every task is assigned: owner chip defaults to **Auto**
  (CEO triages → `requestAutoAssignmentLocked` posts a human-authored @ceo-tagged msg → CEO reassigns
  → runs); **Backlog** create sends `park=true` → task lands in `Drafting` (Backlog stage, assigned,
  NOT dispatched), activated via the FE "Approve & Start" (Drafting→Running, wakes owner; Auto→triage
  on approve). Live-verified all 4 flows + disk persistence.
- **NEXT: Parallel instances per agent (deferred concurrency follow-up — user wants this).** Then
  Phase 4 (Librarian = Pam), Phase 5 (spec→wiki Specs/), Phase 6 (migration, LAST). See the
  "PARALLEL INSTANCES PICKUP" note below for the exact worker-pool changes.
- **⚠ REGRESSION LESSON (2026-06-03):** Phase 2a (channel-per-task) silently broke 5
  `internal/teammcp` tests because 2a verification ran only `./internal/team`. Fixed in
  `9473517c`. **On every phase, run the FULL Go + web suites, not just the package you touched**
  — channel-per-task ripples into any test that assumed tasks live in #general.
- **DONE:** Phase 0 ✅, Phase 1 ✅, naming scrub ✅, Phase 2a (i+ii+iii) ✅, Phase 2b ✅,
  **Phase 3 (a+b+c) ✅, teammcp regressions ✅**.
  Backend is fully task-scoped: **every real top-level task mints its own `task-<id>`
  channel** (2a-iii dropped the keyword heuristic on 2026-06-03 — only System / incident /
  sub-tasks stay shared; verified live + the human and @ceo always retain channel access
  so the primary user is never locked out). #general is owned by the archived "Backup &
  Migration" system task; ~141 `general` refs untouched. **Frontend is pure task-scoped
  too** (sidebar = Tasks by stage, landing → /tasks, DM + per-agent-subspace removed from
  the navigable product, dedicated Agents tool, task detail = tabbed Channel|Spec|Activity).
- **DECISION (2026-06-03): bundle Phases 2b + 3 + 4 + 5 into ONE build-and-test pass**
  (commit at each phase checkpoint for bisectability), then test the integrated result
  against 3 ICP tutorial scenarios. **Phase 6 (persisted-state migration) stays ISOLATED
  and LAST** — it is the only irreversible-on-real-user-data step and must be written
  against the final settled shape + tested on a legacy fixture. Design forks all LOCKED
  (see "LAYOUT FORKS LOCKED" + Phase 3 in the Change log).
- **PARALLEL INSTANCES PICKUP (next concurrency follow-up):** let one agent run multiple
  tasks at once, each on its own per-task model. The per-task runtime already lands the model
  on the task; this is purely the headless scheduler. Changes (from exploration 2026-06-03):
  (1) `headlessWorkerPool` in `headless_codex.go` is keyed by SLUG — nest `workers`/`active`/
  `queues` by `(slug, taskID)` and thread `taskID` through enqueue/dequeue/worker-spawn
  (`headless_codex_queue.go`). (2) Relax the exclusive-owner lane
  (`queueTaskBehindActiveOwnerLaneLocked` + `taskRequiresExclusiveOwnerTurn` in
  `broker_tasks_worktrees.go`) — gate it behind a per-mode toggle (keep serialized for
  `local_worktree` unless per-task worktrees are confirmed collision-free). (3) The ~11
  `agentActiveTask(slug)` callers assume one active task per slug — they need a taskID to
  disambiguate; `headlessTaskWorkspaceDir` must read the turn's task, not "the" active task.
  ~800–1200 lines; its own pass + dedicated concurrency verification.
- Then Phase 4 (Librarian = Pam), Phase 5 (spec→wiki Specs/).
- **LAST (separate):** Phase 6 (persisted-state migration + E2E — now also folds `provider`/
  `model`/`effort` task keys + the "auto" owner sentinel into the legacy-state migration).
- **HARD RULES still active:** (1) NO external-app name anywhere in
  PR/wiki/docs/branch/code (my work scrubbed; 4 pre-existing competitive docs left per
  user). (2) Keep persisted JSON wire keys stable through Phase 5; migrate in Phase 6.
- **Gotchas:** browser-harness↔Chrome CDP is BROKEN (stale :9222 ws) — live screenshots
  blocked until Chrome is restarted (don't kill the user's Chrome unprompted). Dev boot:
  `bash scripts/dev-mvp.sh --reset` (web :7891 / API :7890, API needs auth → use browser
  not curl). The :7891 broker is stale (built pre-2a) — rebuild before any live test.
  Verify Go with `golangci-lint run ./internal/team/...` too (agents only run go vet,
  which misses gofmt + dead code).
- **User working style:** terse, wants momentum ("do it" / "move on"); commit + brief
  check-in per phase increment. Big design forks → ask; routine → just do it.

## Open questions / parking lot

- _(none open)_

---

## Change log

## Change 1 — Tasks as the primary primitive (Issues → Tasks), channel-per-task

**Status:** PLANNING (exploring codebase; no code yet)

### Requirement (from user, 2026-06-02)

1. **Rename "Issues" → "Tasks" everywhere.** Tasks become first-class and the
   primary primitive. (NB: this reverses an earlier Tasks→Issues consolidation —
   see memory `project_mvp_session_2026_05_28`. Confirm intent / migration story.)
2. **Every channel is tied to a Task. There is no default channel.** (Today there
   is a "general"/default channel — must be removed.)
3. **Default screen = a chatbox-style home composer** that creates a new Task. On that composer
   the user selects **provider**, **effort**, and **agent** for the task.
   - In our version the selected agent is the **owner agent**, and it can summon
     other agents as needed.
4. **2 default agents available in every Task:**
   - **CEO agent** — always present by default.
   - **Librarian agent** — this is our current **"Pam"** agent that maintains the wiki.
5. **Librarian agent's expanded role:** responsible for **writing, formatting, and
   organizing the wiki**. Notebooks are still written by the **owning agents**.
   The **Librarian takes over wiki promotions and reviews from the CEO** (it has
   better context on what already exists in the wiki).
6. **Task creation options (launcher style):** start **right away**, put in
   **Backlog**, or create as a **Routine** (routines already implemented).
7. **Simplified Task stages:** `Backlog`, `In-progress`, `Requires human input`,
   `Done`, plus **`Archive`** (for tasks done-and-archived OR archived before done).
8. **Keep the spec-first experience:** every Task starts by writing a **spec**,
   guided by proper questions. Show the user the spec (lives **inside the wiki
   under "Specs", always linked to the Task**) and ask them to approve or suggest
   changes. **Implementation starts only after approval.**
9. **Every Task spins up its own channel**, working like channels do today but
   with a Task's lifecycle. If a Task **requires human input**, it moves to the
   `Requires human input` stage.

### Context (codebase map, 2026-06-02)

**CRITICAL framing — two "issue" concepts exist today:**
- `teamTask` (`internal/team/broker_types.go:183`) — the lifecycle work-item, ID
  `task-N`, carries `IssueDraftSpec`, owner, channel, reviewers, lifecycle state.
  **This is what the UI currently calls an "Issue"** (IssueDocument, IssuesList,
  useCreateIssue, `/issues` route). Internally it is already a "task". So the
  rename is mostly UI/route/component-facing: `Issue*` → `Task*`, `/issues` → `/tasks`.
- `agentIssueRecord` (`broker_types.go:75`, `agent_issue.go`, ID `issue-N`) — a
  DIFFERENT concept: agent-reported problems / self-heal errors. The word "issue"
  here collides with the rename. Decision needed: rename to "incident"/"report".

**A. Tasks / Issues domain (Go backend)**
- Model: `teamTask` struct `broker_types.go:183`; spec `IssueDraftSpec` (Goal/
  Context/Approach/Acceptance) `broker_types.go:267`. Persisted as part of the
  monolithic `brokerState` JSON blob at `~/.wuphf/team/broker-state.json`
  (`broker_persistence.go`), NOT a separate table. `Counter` field issues IDs.
- Lifecycle enum `broker_lifecycle_transition.go:66-107`: Unknown, Intake, Ready,
  Running, Review, Decision, BlockedOnPRMerge, QueuedBehindOwner,
  ChangesRequested, Approved, Rejected, Drafting. Single write chokepoint
  `transitionLifecycleLocked()` (line ~453); public `TransitionLifecycle()`.
  Dispatch gate `isExecutableTeamTaskStatus()` (line 46) allows only Running/Approved.
- IDs: `fmt.Sprintf("task-%d", b.counter)` (`broker_tasks_mutation_service.go:228`);
  agent issues `issue-%d` (`agent_issue.go:86`). (PREFIX-N is a display-layer
  concern, not in the raw ID.)
- Routes: `GET|POST /tasks`, `/tasks/ack`, `/task-plan` (`broker_route_contracts.go:153`);
  inbox/decision `GET /tasks/inbox`, `GET /tasks/{id}`, `POST /tasks/{id}/block|decision|comment`
  (`broker_inbox_handler.go`). Create → `MutateTask(action=create)`
  (`broker_tasks_mutation_service.go:136`). List → `ListTasks()` (`broker_tasks_service.go:16`).
- Spec/approval gate: CEO draft writer `broker_ceo_draft.go` runs ONLY in
  `Drafting` state, one LLM call, fills `IssueDraftSpec`; user approves
  (Drafting→Approved/Running) before dispatch. `IssueDraftSpec` lives ON the task,
  not in the wiki yet.

**B. Channels & messaging**
- Model `teamChannel` `broker_types.go` (Slug, Name, Members, Disabled, Surface).
  Office channels in `b.channels`; DMs in separate `channel.Store`
  (`internal/channel/store.go`), types Public "O" / Direct "D" / Group "G".
- DEFAULT-CHANNEL special-casing to remove: `normalizeChannelSlug("")→"general"`
  (`broker_defaults.go:135`), auto-add new members to #general (`broker.go:909`),
  "general" undeletable (`broker_office_channels.go:810`), `defaultTeamChannels()`/
  `ensureDefaultChannelsLocked()` (`broker_defaults.go:53,145`), onboarding seeds
  "general" (`broker_onboarding.go:220`), and every `||"general"` default in
  `broker_messages.go` (lines 83,440,517,555,621) + web (`client.ts:358,365`,
  `MessageFeed.tsx:41`, `Composer.tsx:37`).
- `teamTask.Channel` already ties a task to a channel; owner auto-joins via
  `ensureTaskOwnerChannelMembershipLocked()` (`broker_channel_access.go:99`). But
  NO reverse `teamChannel.taskID` link yet, and channels are not 1:1 with tasks.
- Messaging: `POST/GET /messages` (`broker_messages.go:57,590`), polling (no SSE for
  messages). Web: `MessageFeed.tsx`, `Composer.tsx`, `ChannelList.tsx`.
- Routing: `/channels/$channelSlug` (`router.ts:16`); index `/` redirects to
  `/agents/ceo` (`router.ts:140`), NOT #general (already demoted).

**C. Agents (CEO, Pam→Librarian, owners, summoning)**
- `officeMember` struct (slug, name, role, expertise, provider binding, Watching).
  Spawned into tmux panes (`pane_lifecycle_spawn.go`). Prompts built in
  `prompt_builder.go` (CEO/lead branch ~140-270; specialist ~271-371).
- CEO: slug `"ceo"`, reserved channel access (all channels), owns delegation +
  **wiki promotion/review today** (prompt rules 8/8b/8c, `prompt_builder.go:217-219`:
  `team_notebook_review` + `notebook_promote`, "the broker auto-writes; you curate").
- Pam (`pam.go`, slug `"pam"`, "the Archivist"): NOT a roster member; headless
  one-shot wiki-maintenance actions triggered from the wiki UI (`broker_pam.go`:
  `GET /pam/actions`, `POST /pam/action`). No prompt/channel/task. **This is the
  agent to become "Librarian"** and absorb wiki write/format/organize + promotion/review.
- Owner agent: `teamTask.Owner`; assigned via intake `autoAssign` (`broker_intake.go`)
  or `team_task(action=assign_to_me)`. Auto-joins task channel.
- Summoning: CEO creates agents (`team_member`) + channels (`team_channel`), adds
  members; `team_bridge` (CEO-only) carries context across channels. Skills via
  `team_skill_run` (`broker_skills.go`).
- Wiki promotion authority TODAY = CEO (reviewer). Demand pipeline
  `promotion_demand.go`, sweep `broker_wiki_lifecycle.go` / `promotion_sweep.go`,
  state machine `promotion_state.go` (JSONL at `~/.wuphf/wiki/.reviews/reviews.jsonl`).

**D. Wiki / notebooks / specs**
- Notebooks: per-agent drafts at `agents/{slug}/notebook/*.md`, author-only writes
  (`internal/teammcp/notebook_tools.go`), cross-agent reads. Owning agents write them. ✅ keep.
- Wiki: markdown on git + index; promotion notebook→wiki via `notebook_promote`
  → pending → reviewer approve/request-changes/reject → `Repo.ApplyPromotion()`.
  Web: `ReviewQueueKanban.tsx`, `ReviewDetail.tsx`.
- Specs: today live ON the task (`IssueDraftSpec`), rendered by `IssueDocument.tsx`
  (streams `issue_draft_section` SSE: goal→context→approach→acceptance; "Approve &
  Start" only in Drafting). NO separate wiki "Specs" section yet — this must be built.

**E. Web shell / home / routines**
- Entry `main.tsx`→`RootRoute.tsx`: not-onboarded→PrePickScreen→OnboardingChat;
  onboarded→Shell. Index `/` redirects to `/agents/ceo` (`router.ts:140`).
- Routes registry `routeRegistry.ts:40-75` (ROUTE_PATHS): channel, dm, app,
  `issues`, `issues/new`, `issues/$issueId`, `agents/$slug`, wiki, notebooks,
  reviews, inbox, etc. Derivation `useCurrentRoute.ts`.
- Sidebar `Sidebar.tsx`: Agents / Channels / Tools (SIDEBAR_APPS in `constants.ts`:
  overview, issues, wiki, console, graph, policies, calendar, skills, activity,
  receipts, health-check, settings) / Recent.
- Create entry points: `useCreateIssue.ts` → `createTasks()`; `IssueCreateDialog.tsx`
  (+/- channel/assignee), `IssueNewForm.tsx` at `/issues/new`.
- Routines (ON MAIN): `RoutinesApp.tsx` + `routines/*` (ScheduleBuilder,
  RoutineComposer, RoutineDetailRoute, RoutineChannelSelect…) + backend
  `broker_scheduler.go` + `schedulerJobClassification.ts`. "Create as Routine" reuses this.
- Provider/effort: `ProviderBinding {kind, model}` on members; provider kinds in
  `internal/provider/types.go` (claude-code, codex, opencode, ollama, vllm, …).
  **No "effort"/reasoning field exists yet** — must be added for the composer.

### Proposed decisions (confirm or override)

- **D1 — Naming collision:** UI "Issue" → "Task" (routes/components/hooks). Rename
  the unrelated `agentIssueRecord` (agent self-reported problems) to "incident" or
  "report" internally so "issue" disappears as a domain word. (Default: "incident".)
- **D2 — Lifecycle collapse** to 5 user-facing stages:
  - `Backlog` ← Intake, Ready, Drafting(pre-approval), QueuedBehindOwner
  - `In progress` ← Approved, Running, Review, Decision, BlockedOnPRMerge
  - `Needs human input` ← ChangesRequested + a new explicit "blocked on human" + decision-pending
  - `Done` ← completed
  - `Archive` ← new terminal; Rejected also routes here
  Spec-draft + approval is a **gate at the Backlog→In progress boundary**, not its
  own stage. Dispatch gate stays: execution only after approval.
- **D3 — Spec in wiki:** the approved spec is written to the wiki under `Specs/`
  (e.g. `team/specs/<task-id>-<slug>.md`), authored by the Librarian, and linked
  from the task. The on-task `IssueDraftSpec` stays as the draft buffer during the
  interview; on approval the Librarian materializes + links it.
- **D4 — Home = new-task composer:** post-onboarding landing becomes the new-task
  composer (provider/effort/owner-agent selectors + Start now / Backlog / Routine).
  Index `/` → `/` (composer), not `/agents/ceo`. Onboarding flow preserved.
- **D5 — Default agents per task:** owner agent (selected) + CEO + Librarian always
  members of every task channel. Owner can summon more.
- **D6 — Librarian = renamed/promoted Pam:** becomes a first-class agent that owns
  wiki writing/formatting/organizing AND takes promotion+review from the CEO.
  Notebooks still written by owning agents.
- **D7 — Effort field:** add a task-level `effort` (e.g. low/medium/high) that maps
  to provider reasoning settings; persisted on the task and passed to the owner run.

### Resolved forks (2026-06-02)

- **D8 — Interaction model: PURE TASK-SCOPED.** The only channels are task
  channels. Remove free-standing office channels (#general/#product), standalone
  agent DMs, AND per-agent subspace pages. Every human↔agent interaction happens
  inside a task. (User chose this over keeping DMs/subspaces.)
- **D9 — Migration: MIGRATE EXISTING workspaces (for shipped USERS, not dev).**
  Refined 2026-06-02: the dev's own `~/.wuphf-dev-home` is disposable (blast
  freely). The concern is real users who upgrade WUPHF on their machines — their
  `~/.wuphf` must come up clean with no data loss. So Phase 6 migration is
  PRODUCTION code: treat as irreversible, gate with a verification agent + present
  the fold strategy before executing. Because target is pure task-scoped,
  migration FOLDS legacy office channels + DMs into archived/done Tasks
  (preserving message history), remaps old lifecycle states → new 5-stage set, and
  migrates persisted `broker-state.json` keys + `agentIssueRecord`→incident rename.
- **D10 — Execution: PHASE-BY-PHASE with check-ins.** Build + verify one phase at
  a time, update this tracker, pause for user review between phases.

### Implementation plan (phased — each phase is a check-in + verification gate)

> Ordering = dependency order, foundation first. JSON wire keys that are
> persisted in `broker-state.json` stay backward-compatible until Phase 6 does the
> one-shot state migration, so we never break loading mid-stream.

- [x] **Phase 0 — Rename Issues → Tasks + disambiguate the duplicate surface. ✅ DONE 2026-06-02.**
  - **Result:** 61 files changed, +864/−2492 (net −1628; removed the duplicate raw
    board). Gates ALL green (verified independently, not just by the sub-agents):
    `go build ./...` ✅, `go vet ./...` ✅, `cd web && bunx tsc --noEmit` ✅ (0 errors),
    `bash scripts/test-web.sh` ✅ (179 files / 1729 pass / 40 pre-existing skips),
    `bash scripts/test-go.sh ./internal/team` ✅.
  - **Go side (agent):** `agentIssueRecord`→`incidentRecord`, `AgentIssues`→
    `Incidents` (kept `json:"agent_issues"` tag), `ReportAgentIssue`→`ReportIncident`,
    new IDs `incident-N`, `agent_issue.go`→`incident.go`. Wire tags preserved.
  - **Web side (agent):** deleted `TasksApp.tsx`+`TaskDetailModal.tsx`(+test);
    renamed 22 files Issue*→Task* (lifecycle dir, issues/→tasks/ dir, 3 cards,
    useCreateIssue→useCreateTask, issueTitle→taskTitle); `/issues`→`/tasks` is now
    the live first-class surface with `/issues*`→`/tasks*` legacy redirect stubs;
    route kinds collapsed to task-board/task-detail/task-new; `tasks` moved to
    FIRST_CLASS_APP_IDS. Wire keys (`parent_issue_id`, `issue_draft_spec`,
    `task_type`/`"issue"`, API paths) preserved verbatim.
  - **Deferred-by-design (NOT bugs — revisit later if desired):**
    - CSS class names `issue-*`, `data-testid="issue-*"`, React Query cache keys
      `["issues"]`/`["issue",id]` kept as-is (internal, not wire/user-facing).
    - `subIssues`/`SubIssue` fields on `DecisionPacket` (lifecycle.ts) kept — they
      are camelCase WIRE fields from the Go broker.
    - Go `agentIssueMessageKind` value `"agent_issue"` kept — it's a persisted
      message Kind read by the SPA renderer (wire).
  - **Committed** as `461b578d` (checkpoint).
  - **Live-tested ✅ (2026-06-02)** on built broker `:7891` (`dev-mvp.sh --reset`):
    web+Go build clean, broker boots with no panic, SPA mounts with no JS/console
    errors, index redirect fires, `/tasks` live, `/issues`→`/tasks` redirect works
    (param preserved — `legacyIssueDetailRoute` confirmed in code). Populated board +
    sidebar "Tasks" label covered by component tests (not re-shot live — fresh
    workspace gates on onboarding; avoided drifting into the user's separate `:7899`
    instance). Broker left running on `:7891` (fresh onboarding-gated workspace).
  - **Dev-boot recipe:** `bash scripts/dev-mvp.sh --reset` → web :7891 / broker API
    :7890 (API requires auth — use the browser via browser-harness, not curl).
  - **DISCOVERY (2026-06-02): two live "task" surfaces collide.**
    - "Issues" surface = human work-items, `task_type="issue"` only, with
      lifecycle/spec/approval/owner: `IssuesList`, `IssueDocument`,
      `IssueDocumentRoute`, `IssueNewForm`, `IssueDetailTabs`, `IssueActionToolbar`,
      `IssueActivity*`, `IssueDescription`, `ParentIssueBreadcrumb`,
      `ReopenIssueButton`, `SubIssuesList`, `IssueCreateDialog`, cards
      `Issue{Created,Comment,Lifecycle}Card`, hook `useCreateIssue`, `lib/issueTitle`,
      route `/issues` (+`/issues/new`,`/issues/$issueId`), route kinds
      `issues-list`/`issue-detail`/`issue-new`, first-class app id `issues`. **← this
      is the canonical "Task" per the user's vision.**
    - "Tasks" (raw office board) = ALL task_types incl. internal automation, no
      lifecycle/spec UX: `TasksApp.tsx`, `TaskDetailModal.tsx`, `useOfficeTasks.ts`,
      app id `tasks` (`/apps/tasks`), route kinds `task-board`/`task-detail`, Console
      "Open task board". The retired `/tasks` + `/apps/tasks/$taskId` routes already
      redirect to `/issues` (`router.ts:36-66`). Predates the Issues surface.
  - **API layer is already canonical `Task`** (`web/src/api/tasks.ts`): `Task`
    interface, `/tasks` + `/task-plan` endpoints. So the rename is presentation-layer.
  - **Wire/back-compat:** keep JSON field values stable (`parent_issue_id`,
    `issue_draft_spec`, `task_type:"issue"`) through Phases 0–5; migrate in Phase 6.
    `IssueDraftSpec`→`TaskDraftSpec` is a TYPE-name change only (wire tag stays).
  - Decouple the Go collision: `agentIssueRecord`/`issue-N`/`ReportAgentIssue` →
    "incident" concept (`incident-N`) so "issue" disappears as a domain word.
  - Gate: `go build`, `bunx tsc --noEmit`, web + go tests green.
  - **FORK RESOLVED (2026-06-02): Issues = canonical Task; RETIRE the raw office
    board.** Delete `TasksApp.tsx`, `TaskDetailModal.tsx` (+test), `useOfficeTasks.ts`,
    and repoint refs (`ConsoleApp` "Open task board", `SkillsApp` navigateToApp,
    `ChannelHeader`/`StatusBar` appTitle, app-panel switch). Reuse the existing
    `/tasks` route + `task-detail`/`task-board` kinds + `tasks` first-class app id
    for the canonical (ex-Issues) surface; add `/issues`→`/tasks` redirect stubs.
  - **Web rename map (1:1, drop "Issue"):** IssuesList→TasksList, IssueDocument→
    TaskDocument, IssueDocumentRoute→TaskDocumentRoute, IssueNewForm→TaskNewForm,
    IssueDetailTabs→TaskDetailTabs, IssueActionToolbar→TaskActionToolbar,
    IssueActivity*→TaskActivity*, IssueDescription→TaskDescription,
    ParentIssueBreadcrumb→ParentTaskBreadcrumb, ReopenIssueButton→ReopenTaskButton,
    SubIssuesList→SubTasksList, components/issues/IssueCreateDialog→
    components/tasks/TaskCreateDialog, cards Issue{Created,Comment,Lifecycle}Card→
    Task{…}Card (+Payloads), IssueSpec→TaskSpec, IssueButton→TaskButton,
    useCreateIssue→useCreateTask, lib/issueTitle→lib/taskTitle, TaskIssueDraftSpec→
    TaskDraftSpec, route `/issues`→`/tasks`, param `issueId`→`taskId`, kinds
    issues-list→task-board / issue-detail→task-detail / issue-new→task-new,
    first-class app id `issues`→`tasks`.
  - **Collisions handled by hand:** `CreateIssueInput` (hook) vs existing
    `CreateTaskInput` (api) — keep distinct; getSubIssues/createSubIssue/reopenIssue
    keep wire keys `parent_issue_id` etc. but rename the TS function symbols.
- [~] **Phase 1 — Collapse lifecycle to 5 stages** (Backlog / In progress / Needs
    human input / Done / Archive) per D2. **IN PROGRESS — design locked, building.**
  - **Approach (confirmed): Stage = display/grouping LAYER over the existing 12
    `LifecycleState` values, NOT an enum collapse.** The 12 states carry load-bearing
    control-loop semantics (dispatch gate keys Running/Approved; reviewer auto-resolve
    on Review; unblock cascade on Rejected/Approved; decision-packet flush). Collapsing
    the enum would break the loop + dozens of `status=="blocked"` readers. So the
    substrate stays; we add a derived 5-value `Stage` that the board + pill render.
    Add `Archived` as a REAL new state (it's an action target, not just a bucket).
  - **7 stages now (user added Scheduled + Blocked, 2026-06-02). Board order:**
    Scheduled → Backlog → In progress → Blocked → Needs human input → Done → Archive.
  - **State→Stage mapping (the product call):**
    | Stage | LifecycleStates / source |
    |---|---|
    | `scheduled` ("Scheduled Tasks") | NOT a LifecycleState — populated from routines/scheduler data |
    | `backlog` | drafting, intake, ready, unknown |
    | `in_progress` | running, review, changes_requested |
    | `blocked` | blocked_on_pr_merge, queued_behind_owner (blocked on another thing first, NO human review needed) |
    | `needs_human` | decision (+ Phase-3: open blocking human request overrides any state) |
    | `done` | approved (Status already = "done"/ship) |
    | `archive` | **archived (NEW)**, rejected |
    - **Blocked vs Needs human input:** Blocked = waiting on a dependency/upstream
      (agent/system resolves it); Needs human input = waiting on the human specifically.
    - **Scheduled** = routines, relabeled "Scheduled Tasks". Routines are scheduler
      entities, not lifecycle tasks → the Scheduled column reads the routines list, not
      `lifecycleStageFor()`. Full task↔routine unification (create-as-routine) is Phase 3.
    - Spec-draft + approval gate sits at the backlog→in_progress boundary (Drafting =
      backlog; approving to start → Running = in_progress). Matches D2.
    - "Agent actively requests human input mid-run → needs_human" is wired in Phase 3
      (needs the per-task channel + request flow); Phase 1 maps needs_human←decision.
  - **Build:** Go — add `LifecycleStage` type + `lifecycleStageFor()`; add
    `LifecycleStateArchived` (enum + CanonicalLifecycleStates + derivedFields +
    migration map + isTerminal); add `archive`/`unarchive` status actions; expose a
    derived `stage` field on the `/tasks` wire payload (additive, back-compat). Web —
    add `LifecycleStage` TS type + labels/tokens; read `stage` off the wire (TS
    fallback map for safety); render the board as 5 stage columns; pill shows stage.
  - Gate: lifecycle dispatch tests still pass (gate unchanged); board renders 7
    columns; archive action round-trips; `go build/vet`, tsc, web+go tests green.
  - **Built 2026-06-02** (two parallel agents, Go substrate + Web 7-stage board).
    Reviewed: Go `lifecycleStageFor` and TS `stageForState` mappings are IDENTICAL
    (verified). Fixed one consistency gap by hand: added `queued_behind_owner` to the
    TS `LifecycleState` union + pill token + `stageForState`→blocked + TaskActivityStream
    switch (it was Go-only before). Web derives `stage` from `lifecycle_state` (no wire
    churn). Scheduled column = `getScheduler()` filtered by `isCadenceSchedulerJob`,
    cards deep-link `/routines/$routineSlug`; board fetches `includeDone:true`.
  - **Gates ALL green (independently re-run):** `go build ./...` ✅, `go vet ./...` ✅,
    `bunx tsc --noEmit` ✅, `bash scripts/test-web.sh` ✅ (179 files / 1731 / 40 skip),
    `test-go.sh ./internal/team` ✅ (agent run). Diff: 15 files, +496/−107.
  - **Live build+boot ✅** — `dev-mvp.sh --reset` rebuilt web+Go and booted the
    Phase-1 broker on :7891 (pid varies) with no panic. **Browser screenshot BLOCKED**:
    browser-harness↔Chrome CDP went stale (`ws://127.0.0.1:9222` dead; daemon restart
    didn't recover; only fix is killing the user's Chrome, which has live tabs → won't
    do unprompted). The 7-column board structure + stage grouping is covered by the 2
    new `TasksList.test.tsx` tests; onboarding gates the visual on a fresh workspace
    anyway. Broker left running on :7891 for the user to eyeball if desired.
  - **NOT committed yet** — awaiting user nod (commit + Phase 2, or hold to eyeball).
- [~] **Phase 2 — Channel-per-task + kill default channel** (pure task-scoped, D8).
    **DESIGN (2026-06-02).**
  - **Recon:** today tasks REFERENCE a channel slug (`normalizeChannelSlug(body.Channel)`,
    →"general"); they do NOT get a dedicated channel. Channel-create primitive exists:
    `createChannelLocked(channelCreateInput)` (broker_office_channels.go:917). Default-
    channel machinery to remove (from Phase-0 map): `normalizeChannelSlug("")→"general"`,
    auto-add-to-general (broker.go:909), general-undeletable (broker_office_channels.go:810),
    `defaultTeamChannels`/`ensureDefaultChannelsLocked` (broker_defaults.go:53,145),
    onboarding seeds general (broker_onboarding.go:220), all `||"general"` in
    broker_messages.go + web. Removal surfaces for DMs/subspaces: dmRoute/DMView,
    AgentSubspaceRoute, router/routeRegistry/useCurrentRoute, slashCommands, objectRoutes,
    Sidebar Agents/Channels sections.
  - **🚨 CRITICAL DISCOVERY (2026-06-02): "general" is load-bearing plumbing, not
    just a UI surface.** 141 non-test `"general"` literals across the backend —
    decision packets (`broker_decision_packet.go:57` `decisionPacketChannel="general"`),
    intake (`broker_intake.go:722`), human-share/human (`broker_human*.go`), requests/
    interviews, skills (`broker_skills.go` x4), scheduler (`broker_scheduler.go` x4),
    studio, auto-notebook, reviewer-routing — all use #general as the SYSTEM FALLBACK
    BUS for non-task messages. Onboarding seeds #general as the sole channel
    (`broker_onboarding_phase2.go:324`). Naive deletion breaks ~141 paths + onboarding.
  - **✅ RESOLVED — D11 "Backup & Migration" task (user, 2026-06-02):** absorb the
    default channel into a special **"Backup & Migration" Task** that OWNS the channel
    (keep slug `"general"`). Named for what it is: the holding container for migrated
    legacy #general history + the system catch-all. This means:
    - The ~141 backend `"general"` fallbacks + `normalizeChannelSlug("")→"general"`
      stay UNCHANGED — they now post to the Backup & Migration task's channel. ZERO 141-callsite
      churn, onboarding doesn't break. This is the big de-risk.
    - The UI becomes pure task-scoped: NO free-standing channel surface / channel
      list / #general landing. #general's role is served by the **Backup & Migration task** on
      the board (it absorbs system + uncategorized messages + legacy #general history).
    - Placement: Backup & Migration task defaults to the **Archive** stage (per "archive them
      under a general task") — parked out of the active flow but always present +
      accessible. (Assumption — correct me if you meant pinned/always-visible.)
    - It's a permanent, non-deletable system task.
  - **Plan (proposed sub-steps, commit each) — per D11:**
    - **2a-i (backend) ✅ DONE + committed.** Backup & Migration system task
      (`broker_system_tasks.go`, ID `task-general`, owns #general, archived, idempotent
      seed at all 3 paths). `teamTask.System` + `teamChannel.TaskID` fields.
      AllTasks/ChannelTasks exclude System tasks; archived tasks skip scheduler. Gates
      green (build/vet/test-go ./internal/team/boot). The `""→"general"` fallback +
      141 refs INTENTIONALLY KEPT (they now feed the Backup & Migration task).
    - **2a-ii (backend) — channel-per-task: DEFERRED from 2a, NEXT.** Root causes the
      agent surfaced: `findReusableTaskLocked` dedups tasks by CHANNEL (must become
      channel-agnostic), prompt builders hardcode `#general`, `canAccessChannelLocked`
      ordering (channel must exist before access check), ~15 tests assume tasks live in
      "general". Plan: new non-system tasks get a dedicated `task-<id>` channel
      (createChannelLocked, members owner+ceo, reverse-linked via teamChannel.TaskID);
      make task reuse keyed on title/intent not channel; migrate prompts off `#general`.
  - **2a-ii design (traced 2026-06-02):** Today `preferredTaskChannelLocked`
    (broker_tasks_worktrees.go:250) does the OPPOSITE of channel-per-task — for a
    business-objective task it GROUPS it into a recent (<20min) shared execution channel
    by the same creator (channels hold many tasks). `findReusableTaskLocked`
    (broker_tasks_lifecycle.go:570) hard-filters reuse by channel (line 578). Flip:
    (1) business-objective tasks (gate: `taskLooksLikeLiveBusinessObjective`) MINT their
    own `task-<id>` channel (members owner+ceo, reverse-link teamChannel.TaskID); remove
    the group-into-shared-channel behavior. (2) Non-business/system tasks stay in
    `general` (Backup & Migration) — keeps system plumbing quiet. (3) `findReusableTaskLocked`
    drops the channel hard-filter → reuse by title+owner+thread+scoped-identity
    (channel-agnostic). (4) create path mints the channel BEFORE the access check.
    (5) prompt_builder.go:268 "keep #general for top-level decisions" → task-scoped wording.
  - **2a-ii ✅ DONE + committed.** Implemented as designed. Removed now-dead
    `taskChannelCandidateOwnerAllowed` + gofmt'd 2 Phase-1/2a files → golangci-lint 0
    issues. Gates: build/vet ./..., golangci-lint(0), test-go ./internal/team (111s), boot
    clean. 7 tests updated + 3 added. **Refinement flagged (not blocking):** sub-issues
    (ParentIssueID set) currently fall back to #general instead of their parent task's
    channel — better to inherit the parent's channel; revisit in Phase 2 polish / Phase 5.
    - **2b (frontend) ✅ DONE + committed.** Implemented as designed:
      - **Sidebar** (`Sidebar.tsx`): dropped Agents + Channels nav sections; new
        `SidebarTaskNav.tsx` = tasks grouped by the 7 stages (active stages open by
        default), `+ New` → /tasks/new, `All tasks` → board. Tools section keeps AppList
        with **Agents** added as a first-class tool.
      - **Landing** (`router.ts` indexRoute + `RootRoute.tsx` onboarding redirect):
        `/agents/ceo` → `/tasks` (interim home; composer is Phase 3).
      - **Agents tool** (`AgentsTool.tsx`, new): `/agents` roster grid of cards +
        `/agents/$agentSlug` detail (reuses `AgentProfilePanel`). `+ New agent` → wizard.
      - **DM + subspace removed from the navigable product:** deleted `dmRoute`,
        `agentSubspaceRoute`/`agentSubspaceTabRoute`, route kinds `dm`/`agent-subspace`
        (→ `agents`/`agent-detail`); deleted `AgentSubspaceRoute.tsx`. Rewired all
        consumers (StatusBar, ChannelHeader, Shell, AgentPanel, AgentList, breadcrumbs,
        objectRoutes `#/dm/`→`#/agents/`, slashCommands) + tests.
      - **Task detail = tabbed** (`TaskDetailTabs.tsx`): Channel (channel discussion) ·
        Spec (the task body) · Activity (feed) + Sub-tasks when present. Spec-first
        default while drafting, else Channel.
      - **Gates:** `tsc` clean · biome clean · full web vitest **1732 passed / 40 skipped
        / 0 failed**.
      - **PRESERVED as internal-only (onboarding still uses them):** `DMView` +
        `directChannelSlug` (OnboardingChat / InterviewBar CEO-echo / useBrokerEvents).
        NOT navigable; full source deletion deferred to Phase 6 cleanup.
      - **DEFERRED to Phase 6 cleanup (dormant, not dead-causing):** store fields
        `sidebarAgentsOpen`/`sidebarChannelsOpen` + `activeAgentSlug` (kept to avoid
        touching the persistence layer now); `ChannelList`/`AgentList` components still
        exist (used by `CollapsedSidebar` popovers — revisit). Skipped `TaskDocument.test`
        blocks still reference old "Comments" tab label — update when the FIXME hang is
        fixed.
  - **Sequencing decision (TAKEN, not asking):** Tasks board is the INTERIM home in 2b;
    the rich new-task composer is Phase 3. Keeps the app working throughout (never a
    broken no-landing state).
  - **FORK RESOLVED (2026-06-02): agent management → a dedicated "Agents" tool**
    (standalone surface under Tools: list roster, create agents, configure
    provider/role/persona). Agents stay first-class, just not chat surfaces.
  - **LAYOUT FORKS LOCKED (2026-06-03):**
    1. **Task detail = TABBED** — one header (title + stage pill) over
       `Channel | Spec | Activity` tabs. Channel tab is the per-task chat;
       Spec tab renders the wiki spec + approval gate (Phase 5); Activity tab
       is the existing TaskActivity feed. Default to Channel tab.
    2. **Agents tool = ROSTER GRID of cards** (CEO, Librarian, specialists) +
       "+ New agent"; click a card → configure skills/role. Reuses the
       existing card-grid pattern (AgentList/AgentPanel/AgentProfilePanel).
    3. **Composer = centered chatbox + chips** (see Phase 3 above).
  - **external-app naming: BANNED everywhere** (PR/wiki/docs/branch/code) —
    user hard rule 2026-06-02. My tracker scrubbed (commit 3f46f328). Pre-existing
    competitive-analysis docs (desktop-platform.md, tutorials/*) left as-is per user
    (out of this PR's scope). the 🗄 emoji's Unicode name is an unrelated false positive.
  - Gate: new task → its own channel works; no path depends on a default channel;
    app boots to a working tasks-home with no DM/subspace surfaces.
- [x] **Phase 3 — new-task home composer. ✅ DONE (3a+3b+3c).** Home = new-task composer with
    provider / effort / owner-agent selectors + Start now / Backlog / Routine
    actions. Add `effort` field to task model + run wiring (D7). Wire Routine→
    existing scheduler; Backlog→create-without-dispatch; Start now→spec interview→
    approval→In progress. Seed each task channel with owner + CEO + Librarian (D5).
  - **Layout LOCKED (2026-06-03): centered chatbox + chips.** Single focal
    input ("What do you want to get done?"); provider / effort / owner as inline
    chips; Start-now / Backlog / Routine below. Mirror the reference homepage
    composer's components + interaction that the user pointed to in chat.
    **Model + effort are coupled and model-specific (clarified 2026-06-03):**
    the effort options are NOT a fixed global Low/Med/High — they derive from
    the selected model's own capabilities. Selecting a model populates that
    model's effort/reasoning set; changing the model updates the effort
    choices; both are selectable and changeable (in the composer and on the
    task later). Needs a model→effort-options registry/capability map.
  - **NAMING GUARD (hard rule):** match the reference design, but the external
    app's name must NOT appear in any artifact (code, comments, docs, this
    tracker, PR, branch). Use our own task vocabulary throughout.
  - **BUILD MAP (explored 2026-06-03, for a fresh window):**
    - Existing creation surface = `web/src/components/lifecycle/TaskNewForm.tsx`
      (title/details/channel/assignee → `createTasks()` → `POST /task-plan`
      `{channel,created_by,tasks:[{title,assignee,details,task_type:"issue"}]}`).
      The composer REPLACES this as the landing (index → composer instead of the
      2b interim `/tasks` board); keep `/tasks/new` as a fallback.
    - Model catalog = `web/src/lib/modelCatalog.ts` (`modelOptionsForKind(kind)`
      → per-provider model lists; providers are `LLMRuntimeKind`:
      claude-code / codex / opencode / mlx-lm / ollama / exo).
    - 3-step build:
      - **3a (backend) ✅ `5e43ceb3`:** `Effort string` on `teamTask` + `teamTaskWire`
        (wire key `effort`, stable). Threaded into dispatch: claude `--effort <level>`,
        codex `-c model_reasoning_effort=<level>`, re-validated per runtime in
        `internal/team/headless_effort.go`. Also plumbed through `TaskPlanInput` + both
        create paths. Effort CLI mechanisms confirmed (claude `--effort`
        low/medium/high/xhigh/max; codex `model_reasoning_effort`
        minimal/low/medium/high/xhigh). Live-verified: round-trips to `broker-state.json`.
      - **3b (UI) ✅ `d5b10eb8`:** `web/src/components/tasks/TaskComposer.tsx` (centered
        chatbox + owner/provider/model/effort chips + Start/Backlog/Routine). Effort map =
        `web/src/lib/effortCatalog.ts` (mirrors the Go guardrails). Mounted as landing
        (`/` → `{kind:"home"}`). Provider/model edits persist to the owner agent's binding;
        effort is per-task. Shared `web/src/lib/providerBinding.ts`.
      - **3c (wiring) ← NEXT.** See "3c PICKUP" below.
    - **3c PICKUP (start here):**
      - **Backlog = create-without-dispatch.** PROBLEM: `refreshPlannedTaskBlockStateLocked`
        sets `status="in_progress"` whenever an owner is set, which dispatches the owner
        immediately. For Backlog we want the task parked, no owner turn. Investigate: create
        with empty assignee (no owner → stays "open", no dispatch) vs. a backlog lifecycle
        state. The composer currently sends `assignee=owner` for both Start and Backlog and
        routes Backlog to `/tasks`; change Backlog to NOT trigger the owner run. Check
        `lifecycleStageFor`/`stageForState` for the backlog stage + how the board's Backlog
        column is populated, and whether `POST /task-plan` can create a parked task.
      - **Routine = prefill `/routines/new`.** 3b just navigates there. Check
        `web/src/api/scheduler.ts` + the routine composer route for a prompt/title prefill
        (search param or store) and pass the composer's prompt through.
      - **Start now = spec→approval→running.** Confirm the current create path already runs
        the owner through the spec interview + approval gate before In-progress (it should —
        that is the existing lifecycle); add nothing if so, document if it does.
      - Channel members: per-task channel already seeds owner + actor (2a). Librarian seed
        is Phase 4.
  - Gate: all 3 create-modes work end-to-end from the composer. ✅ MET — live-verified
    via the broker API (Backlog parks unassigned, Start now dispatches, effort persists to
    disk, Routine prefills /routines/new). Composer is the landing (`/`); board at /tasks.
    Note: visual browser click-through still blocked by the Chrome CDP zombie (data path
    + build + full test suite are the verification surface used instead).
- [ ] **Phase 4 — Librarian agent (Pam → Librarian).** Promote Pam to first-class
    `librarian` agent, default member of every task. Move wiki write/format/
    organize + promotion + review authority CEO→Librarian (prompts + tool gating).
    Owning agents still write notebooks (D6).
  - Gate: Librarian present in tasks; promotion/review flows route to Librarian.
- [ ] **Phase 5 — Spec-first into wiki `Specs/`** (D3). On approval, Librarian
    materializes approved spec to `team/specs/<task>.md`, linked from the task.
    Keep interview/questions + approval gate; render spec from wiki + link.
  - Gate: approve a task → spec appears in wiki Specs, linked both ways.
- [ ] **Phase 6 — Persisted-state migration + cleanup + E2E.** One-shot
    `broker-state.json` migration (lifecycle remap, fold legacy channels/DMs into
    archived Tasks, rename keys, incident rename). 3 ICP tutorial E2E scenarios +
    screenshots.
  - Gate: load a pre-change workspace → comes up clean as Tasks; tutorials pass.

**Status of plan:** awaiting user go-ahead to start **Phase 0**.
