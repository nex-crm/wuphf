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

## Open questions / parking lot

- _(none yet)_

---

## Change log

## Change 1 — Tasks as the primary primitive (Issues → Tasks), channel-per-task, citation-style home

**Status:** PLANNING (exploring codebase; no code yet)

### Requirement (from user, 2026-06-02)

1. **Rename "Issues" → "Tasks" everywhere.** Tasks become first-class and the
   primary primitive. (NB: this reverses an earlier Tasks→Issues consolidation —
   see memory `project_mvp_session_2026_05_28`. Confirm intent / migration story.)
2. **Every channel is tied to a Task. There is no default channel.** (Today there
   is a "general"/default channel — must be removed.)
3. **Default screen = a chatbox like the the reference tool homepage**
   (https://github.com/hilash/reference tool) that creates a new Task. On that composer
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
6. **Task creation options (like the reference tool UI):** start **right away**, put in
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
- **D4 — Home = the reference tool composer:** post-onboarding landing becomes the new-task
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
  - **NOT committed yet** — awaiting user nod to checkpoint-commit Phase 0.
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
- [ ] **Phase 1 — Collapse lifecycle to 5 stages** (Backlog / In progress / Needs
    human input / Done / Archive) per D2. Redefine enum + derived-field map +
    transitions + dispatch gate + state pills + board columns. In-memory
    old→new remap (full persisted migration deferred to Phase 6).
  - Gate: lifecycle dispatch tests pass; board renders 5 columns.
- [ ] **Phase 2 — Channel-per-task + kill default channel** (pure task-scoped, D8).
    1:1 task↔channel link; spin a channel per task at creation; rip out every
    `""→"general"` fallback + office-channel/default machinery; remove agent DMs +
    subspaces; sidebar lists Tasks grouped by stage instead of channels/agents.
  - Gate: new task → its channel works; no path depends on a default channel.
- [ ] **Phase 3 — citation-style home composer.** Home = new-task composer with
    provider / effort / owner-agent selectors + Start now / Backlog / Routine
    actions. Add `effort` field to task model + run wiring (D7). Wire Routine→
    existing scheduler; Backlog→create-without-dispatch; Start now→spec interview→
    approval→In progress. Seed each task channel with owner + CEO + Librarian (D5).
  - Gate: all 3 create-modes work end-to-end from the composer.
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
