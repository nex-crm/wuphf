# TODOs

Tracking work that is deliberately deferred from the current branch. Each item names the trigger that would unblock revisiting it.

## Open

### 1. Validate customer signal frequency before it becomes load-bearing in marketing

**What:** Add explicit instrumentation to count how often users actually shred their existing office and rebuild from a different blueprint. The current observation ("users shredding to test different business ideas") is anecdotal.

**Why:** The multi-workspace v1 design (2026-04-28) ships with founder velocity as the primary signal and customer behavior as a secondary hypothesis. If multi-workspace ships and the rebuild-after-shred pattern is actually 1-2 anecdotes rather than a recurring behavior, marketing copy and product positioning that leans on the customer-facing argument will be off-tone.

**Pros:** Decouples product claims from unverified user behavior. Keeps the founder-velocity argument honest and load-bearing without overstating.

**Cons:** Adds analytics surface area. Requires deciding what "shred + rebuild" looks like instrumentally (timestamp delta on shred → next blueprint create within X days?).

**Context:** The 2026-04-28 office-hours design doc (`~/.gstack/projects/nex-crm-wuphf/najmuzzaman-feat-multi-workspace-design-20260428-125124.md`) flagged this in "Demand Evidence" and "Open follow-ups." The signal is currently founder-reported, frequency unspecified. Multi-workspace ships regardless — this TODO is about marketing accuracy, not product blocking.

**Depends on / blocked by:** Multi-workspace v1 must ship before we can measure the signal in the new UI surface. Then ~30 days of telemetry before drawing conclusions.

**Trigger to revisit:** First marketing copy pass that mentions multi-workspace, OR a v1.1 product decision that depends on customer-facing rationale.

---

### 2. Decide whether to rip broker auth entirely (separate design pass)

**What:** Evaluate whether bearer-token authentication on the local WUPHF broker is justified given the threat model (single-user single-machine). Today every endpoint except `/web-token` requires `Authorization: Bearer <token>`.

**Why:** Surfaced during multi-workspace review (2026-04-29) when the user asked "why do we have auth now if this is just a locally running cli?" Auth exists today to defend against rogue local browser tabs (CORS isn't airtight on localhost). But the cost in code surface (per-handler checks, peer-token map for cross-broker, token file lifecycle) is non-trivial.

**Pros:** Removing auth would simplify ~200 LOC across broker, web client, peer-token map, and the new multi-workspace design. Single-user CLI tools rarely run web auth.

**Cons:** Real attack surface today: any local web app on `localhost:*` could fetch broker endpoints and read team data, send messages, dispatch agents. CORS reduces but doesn't eliminate. Removing auth is a one-way door if WUPHF ever ships a hosted/multi-user variant.

**Context:** Multi-workspace v1 inherits the existing auth scheme via a new `withAuth` middleware. The middleware refactor reduces blast radius of auth changes. Once that lands, ripping auth is a clean follow-up — change the middleware to be a noop and audit the `/web-token` endpoint.

**Depends on / blocked by:** Multi-workspace v1's `withAuth` middleware must land first. Then a fresh design pass on the threat model, ideally with a security review.

**Trigger to revisit:** When the broker code surface around auth feels disproportionate to the threat, OR when a future feature needs to add another auth-required route and the per-handler check feels gratuitous.

---

### 3. Post-MVP: shared API keys via `~/.wuphf-spaces/keys.json` symlink

**What:** Today, multi-workspace forks API keys at create time (each workspace's `config.json` gets a copy). Future: a global `~/.wuphf-spaces/keys.json` that each workspace's `config.json` symlinks to (or reads as a fallback) so updating an API key in one place propagates everywhere.

**Why:** The fork-at-create pattern means rotating an API key requires re-pasting it into every workspace. For founders running 3-4 workspaces, that's friction at every key rotation (typically every few months for security).

**Pros:** Single point of truth for API keys. Rotations are one-touch. Matches the LLM-CLI auth model (`~/.codex`, `~/.claude` already global).

**Cons:** Couples workspaces by introducing shared mutable state. A malformed update breaks every workspace. Loses per-workspace key isolation (e.g., different LLM provider quotas per workspace).

**Context:** Multi-workspace v1 (2026-04-28 design) explicitly defers this and documents the fork semantics. The design notes "out of scope" but worth tracking because it directly affects founder velocity, which is multi-workspace's primary justification.

**Depends on / blocked by:** Multi-workspace v1 must ship. Then ~30 days of usage to see how often users actually rotate keys and whether the fork friction is real.

**Trigger to revisit:** First user complaint about "I changed my Anthropic key in workspace A but workspace B still has the old one," OR a security incident requiring a forced rotation.

---

### 4. Two-step create + onboard: route the rich onboarding fields through /onboarding/* per workspace

**What:** Today the CreateWorkspaceModal collects `company_description`, `company_priority`, `llm_provider`, `team_lead_slug` etc. and previously sent them on the create payload. The broker's `CreateRequest` only accepts `{name, blueprint, inherit_from, company_name, from_scratch}`, so those richer fields used to 400 every request. Fix in this PR (CodeRabbit #3164366659): drop them from the wire payload. Followup: after `/workspaces/create` returns, navigate to the new workspace and run a scoped `/onboarding/*` flow there to apply the rich fields. The Wizard already exists; what is missing is the per-workspace runtime context for that wizard call.

**Why:** "Inherit from current" and "Start from scratch" both currently assume the active broker's onboarding endpoints. For from-scratch the wizard runs against the active broker (CodeRabbit #3164366660), which mutates the wrong workspace.

**Trigger to revisit:** First user feedback that the modal's company-description/llm fields don't actually take effect on the new workspace, OR Lane B/C exposing per-workspace `/onboarding/*` endpoints.

---

### 5. Surface known orchestrator errors as 4xx instead of blanket 500

**What:** `internal/team/broker_workspaces.go` currently returns 500 for every orchestrator error (invalid name, not-found, conflict, etc.). CodeRabbit #3164366603 is correct that this leaks expected client errors into the server-error bucket and degrades the API contract.

**Why:** Out of scope for this CodeRabbit cleanup — needs a typed error sentinel pass on Lane B's `internal/workspaces` package (ErrSlugInvalid, ErrSlugReserved, ErrWorkspaceNotFound, ErrWorkspaceConflict, ErrPortExhausted) and a centralized `errorToStatus` mapper in broker_workspaces.go. Defer until the orchestrator settles.

**Trigger to revisit:** Lane B's typed-error sweep, or first user-visible bug where a 400-class error renders as a generic "Internal server error".

---

### 6. Replace handleWorkspacesPause hand-rolled HTTP proxy with orchestrator.Pause

**What:** CodeRabbit #3164656935 — the broker's `/workspaces/pause` handler currently opens its own HTTP request to the target broker's `/admin/pause`, bypassing `workspaceOrchestrator.Pause`. That means registry/state transitions, timeouts, and cleanup that Lane B put behind `workspaces.Pause` are skipped on this code path.

**Why:** Out of scope for this PR — a clean fix needs to either route the orchestrator's `Pause` itself via the cross-broker proxy (so registry transitions can wrap the proxy call) or split the handler into "active workspace" vs "remote workspace" paths.

**Trigger to revisit:** First bug report where a paused workspace shows up running in the registry, OR Lane B refactoring `Pause` to accept a target URL.

---

### 7. orchestrator.Pause should fail closed on shutdown errors

**What:** CodeRabbit #3164366631 — `orchestrator.Pause` currently ignores `readTokenFile`/`postAdminPause` errors and falls through to `StatePaused` even if the broker shutdown timed out. That can leave a live broker while the registry says paused.

**Why:** Touching the pause state machine has cascade risk — pause is the most-tested workspace lifecycle path (six tests in `cmd/wuphf/workspace_test.go` alone) and the SIGTERM/SIGKILL escalation already covers the worst case (broker did not exit). Defer to a focused PR with a dedicated test plan.

**Trigger to revisit:** First port-conflict bug after a pause+resume cycle, OR a fresh design pass on pause failure modes.

---

### 8. snapshotDir leak fingerprints should hash file content, not just size

**What:** CodeRabbit #3164366617 — the Phase-0 leak gate's `snapshotDir` keys files by size only. A mutation that preserves byte length escapes detection.

**Why:** Out of scope — adding SHA-256 per file is the right fix but it's a non-trivial test refactor (deduce when to skip binaries, manage error tolerance, surface diff context). The current size-based check still catches the dominant leak modes (new file appears, file grows).

**Trigger to revisit:** Any leak-test false-negative report.

---

### 9. ClearRuntime/Shred should consolidate onto ResetAt/ShredAt

**What:** CodeRabbit #3164366608 — the doc comments on `internal/workspace/workspace.go::ClearRuntime` and `Shred` claim they delegate to `ResetAt`/`ShredAt`, but the actual implementations duplicate the wipe logic.

**Why:** Out of scope for the multi-workspace branch — `internal/workspace` is the singular-workspace package, mostly unchanged in this PR. Consolidating wants its own focused commit + tests so the wipe set stays in lockstep.

**Trigger to revisit:** Next time anyone touches the wipe path (adding/removing a stripped subdir).

---

### 10. Trash listing endpoint missing

**What:** CodeRabbit #3164366654 — `useWorkspaceTrash` queries `/workspaces/list?include=trash` but the broker does not implement either an `include=trash` query parameter or a dedicated endpoint. The hook always returns empty entries today.

**Why:** Lane B's `internal/workspaces` package owns the trash listing; the broker handler is a passthrough. Pending Lane B exposing `Orchestrator.Trash()` and a route registration.

**Trigger to revisit:** First UI surface that needs to render trash entries (RestoreToast already lives off the mutation, not a query, so it's not blocked yet).

---

### 11. StatusPill: render loading placeholder instead of "0 tokens today"

**What:** CodeRabbit #3164366665 — the pill renders `0` while the usage query is still in-flight, which is briefly misleading.

**Why:** Trivial fix but touches the most visible UI surface in the rail; defer to the next StatusPill polish pass to avoid a one-liner change in this comment-cleanup batch.

**Trigger to revisit:** Next StatusPill change, or a user report that the "0 tokens" state looks wrong.

---

### 12. orchestrator.go uses RuntimeHomeDir for cross-workspace token + symlink paths

**What:** CodeRabbit #3164366633 + #3164366635 — two sites in `internal/workspaces/orchestrator.go` (~lines 350 and 530) resolve token-file and symlink paths via `config.RuntimeHomeDir()`. These are cross-workspace artifacts that semantically must live at the user's REAL home (matching `spacesDir` and `migration.go`).

**Why:** The doctor symlink site mirrors what `doctor_fix.go::symlinkPaths` already does (now fixed in this PR). The token-file path inside `orchestrator.go::Pause` is the harder one — it shells out to `tokenFilePath(home, name)` which is package-private. Touching it without rewiring the token reader is risky.

**Trigger to revisit:** First reproducible test where a `--workspace=foo` override loses track of pause/resume tokens, OR any change to `tokenFilePath`.

---

### 13. From-scratch flow should run `/workspaces/create` first, then scope Wizard to the new workspace

**What:** CodeRabbit #3164366660 — when the inherit toggle is OFF, `CreateWorkspaceModal` skips `useCreateWorkspace` and renders the Wizard directly against the active broker. That mutates the current workspace instead of creating a new one.

**Why:** Out of scope for this PR — a clean fix needs Lane C/D to expose per-workspace `/onboarding/*` endpoints (or a way for the Wizard to talk to a freshly-spawned broker before the user sees it). The same prerequisite blocks TODO #4 above. Tracking separately because the user-visible bug is "from-scratch silently overwrites my current workspace" which is data-loss territory.

**Trigger to revisit:** Lane C/D exposing per-workspace onboarding endpoints, OR a security review flagging the data-overwrite path.

---

### 14. TestList_ErrorIsSurfaced is inert

**What:** CodeRabbit #3164366592 — `cmd/wuphf/workspace_test.go::TestList_ErrorIsSurfaced` only asserts a local sentinel; it never calls `runWorkspaceList`/`runWorkspace`, so it can't catch regressions in the error path.

**Why:** Real fix needs a `withFakeOrchestrator` + `printError` capture seam, which doesn't exist yet in cmd/wuphf. Adding one is a separate test-infra change.

**Trigger to revisit:** Next time the workspace test suite gets a fixture refactor.

---

### 15. SystemSchedulesPanel.test.tsx — vi.mock top-level variable bug

**What:** `web/src/components/apps/SystemSchedulesPanel.test.tsx` crashes vitest with `ReferenceError: Cannot access 'MOCK_SPECS' before initialization` because `vi.mock` hoisting runs before the top-level `MOCK_SPECS` const is initialized.

**Why:** Pre-existing bug in the file as merged. The fix is to move `MOCK_SPECS` inside the factory callback or use `vi.hoisted()`. Out of scope for the wiki read-tracking feature.

**Trigger to revisit:** Next pass on the SystemSchedulesPanel test suite or when vitest compatibility is reviewed.

---

### 16. wiki_reads.go: in-memory ReadStats cache to replace per-article file scan

**What:** `AllStats()` does a full linear scan of `reads.jsonl` on every `BuildArticle` call. As the log grows over months of agent + human reads, this becomes O(n) on every wiki article open. Fix: maintain an in-memory `map[string]ReadStats` updated on each `Append` (already holding the mutex), and serve `Stats`/`AllStats` from the map rather than the file. The file stays as a durable audit log.

**Why:** At v1 corpus scale (≤500 articles, ≤10k read events) the scan is fast enough. After 6+ months with active agent reads it will noticeably slow article page loads.

**Trigger to revisit:** When `reads.jsonl` exceeds ~10k lines, or if article load latency exceeds 200ms in production.

---

### 18. Wiki worker queue depth monitoring + alert thresholds (post-PR 1 notebook auto-writer)

**What:** Instrument `wiki_worker.go` to emit queue depth and time-in-queue metrics, plus a warn log at 80% of the 64-buffer capacity. Track the dropped-event counter from `auto_notebook_writer.go` alongside.

**Why:** PR 1 of the notebook-wiki-promise effort adds a new write source (every agent message + every task transition) that competes with real wiki, artifact, fact, lint, playbook, and learning writes through the same shared queue. Codex flagged this as the most likely operational failure mode. Without observability we discover starvation only when a real wiki write fails or times out.

**Pros:** Confirms PR 1 didn't quietly DoS real wiki writes. Gives a concrete signal for tuning the auto-writer's enqueue capacity or for triggering the OV7B event-log migration (TODO #20).

**Cons:** Adds a small metrics surface; needs to land somewhere in the existing analytics path.

**Context:** PR 1 design doc: `~/.gstack/projects/nex-crm-wuphf/najmuzzaman-main-design-20260505-131620-notebook-wiki-promise.md`. Eng review: same dir, dated 2026-05-05. Decision OV7A locked the per-event commit model with bounded enqueue; finding #8 from the Codex outside voice is the underlying concern.

**Depends on / blocked by:** PR 1 must ship first (introduces the new write source).

**Trigger to revisit:** First week of real PR 1 traffic, OR if any real wiki write ever times out.

---

### 19. Premise #3 closure design pass: memory workflow gate auto-satisfaction or removal

**What:** Fresh `/office-hours` design pass on closing the memory workflow gate semantically. Today the gate requires `lookup + capture + promote` all satisfied to mark a task complete (`memory_workflow.go:398`). The eng review had originally planned to satisfy it inline from the auto-writer, but Codex flagged two killers: (a) the gate needs all three steps, not just capture; (b) calling `RecordTaskMemoryCapture` inline holds `b.mu` and would deadlock with the broker API path.

**Why:** Premise #3 of the notebook-wiki-promise design says "kill the narrow `process_research` filter so the gate applies to all tasks, auto-satisfied by the writer." With OV3A we deferred this from PR 1 because the implementation path was wrong. Two possible exit ramps: (1) auto-satisfy all three steps deterministically with proper locking (raw helper holding existing lock + queueing the file write async); (2) kill the gate entirely — once writes are deterministic the gate is dead weight, but we lose its override-tracking and partial-error surfaces.

**Pros:** Closes premise #3 honestly. Either direction simplifies the broker.

**Cons:** A real design pass, not a one-line filter removal. Potentially touches `memory_workflow.go`, `broker_tasks_memory_workflow.go`, and `memory_workflow_reconciler.go`.

**Context:** Notebook-wiki-promise design doc PR 8 row was originally "5-line filter removal at memory_workflow.go:212-227." Codex outside voice (2026-05-05) showed why that's wrong. Locked decisions OV3A, OV3B (rejected), OV3D (deadlock).

**Depends on / blocked by:** PR 1 + PR 2 should ship first so we can observe the writer's real behavior before deciding which exit ramp is right.

**Trigger to revisit:** After PRs 1, 2, 3 ship and the auto-writer behavior is observable in real WUPHF traffic.

---

### 20. Evaluate event-log architecture migration after notebook-wiki-promise PR 1 screenshot test

**What:** After PR 1 ships and one full WUPHF session populates shelves, evaluate whether the per-event direct-commit model (OV7A) creates the predicted problems: wiki worker queue saturation, NotebookSignalScanner garbage on noisy files, PR 3 clustering quality. If yes, plan migration to the alternative architecture (OV7B): append-only event log per agent (`agents/{slug}/notebook/.events.jsonl`) plus a renderer pass that materializes markdown files on cadence or on-demand.

**Why:** Codex outside voice (2026-05-05, finding #11) argued the event-log architecture is fundamentally simpler than per-event commits and avoids problems PRs 3-6 will have to unwind. We chose OV7A for PR 1 because it gives instant visible shelf fill (the screenshot is the demand test). If the prediction holds, the migration option needs to be alive, not re-derived from scratch.

**Pros:** Far fewer git commits (1/min batched vs 1/event). Lighter long-term cost as agent activity scales. Cleaner separation between durable raw stream and rendered surface.

**Cons:** Bigger refactor than designing it right once. Migration must preserve existing markdown files or accept that historical writes stay in the original layout.

**Context:** Notebook-wiki-promise design 2026-05-05. OV7A vs OV7B in the eng review. Codex flagged OV7B as the simpler architecture; we picked OV7A for the demo path with this TODO as the safety net.

**Depends on / blocked by:** PR 1 must ship and one full session must populate shelves. TODO #18's queue-depth metric is a leading signal.

**Trigger to revisit:** After the screenshot post in founder channel + one week of PR 1 production traffic, OR if TODO #18 alerts fire repeatedly, OR if PR 3 ranking quality is poor.

---

## Deferred

Items with known fixes but out of scope for this branch. Each names the trigger that would unblock revisiting.

### 17. Staleness badges need catalog card rendering (article view has a catch-22)

**What:** `StalenessIndicator` is rendered in `WikiArticle.tsx`, which means it only shows in the article view. But `fetchArticle` always sends `&reader=web`, which records a human read and resets `days_unread=0` before the component renders. Result: a human can never see the "unread 30d+" or "agents only" badge in the browser — their own page view clears the state.

**Fix:** Add badge rendering to the wiki catalog cards (sidebar article list) where browsing does not trigger a read. Alternatively, add a `?stats_only=1` param to `BuildArticle` that returns stored stats without appending a new read event.

**Trigger to revisit:** Next pass on the wiki catalog UI, or when a user reports "I never see the staleness badges."

---

## Closed

