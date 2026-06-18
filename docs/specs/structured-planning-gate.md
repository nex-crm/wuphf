# Structured planning gate — no duplicate / shallow tasks

Branch: `fix/structured-planning-no-dup-tasks` (worktree `.worktrees/structured-planning`, off `origin/main`).

## Problem (user report)
1. Duplicate tasks getting created.
2. Tasks should go through a **structured planning** process FIRST.
3. Agent should **ask the human relevant questions** before creating tasks / starting execution.
4. Must **not create redundant shallow subtasks**.

## Root causes (verified in code)
1. Dedup (`findReusableTaskLocked`, `broker_tasks_lifecycle.go`) matches only **exact title+owner**; the "dedupe first" rule is prompt-only → near-duplicates spawn.
2. Plan mode was **fully removed** in `3465c75f` (core-loop R3). New Issues land in `Running` and dispatch immediately. No questions, no plan, no approval.
3. CEO prompt actively says *"auto-create sub-issues… don't wait for the human"* with no sibling/parent dedup.

## Design (user-confirmed direction)
- Structured planning uses each **provider's native plan mode** (Claude `--permission-mode plan`, Codex `-s read-only`). Artifact is a **plan file** in the owner notebook — NOT a new UI gate.
- Agent asks clarifying **questions via `human_interview`**.
- Plan is **approved via `human_interview`** (reuse existing approval card). Approve → `Planning→Running`.
- All enforced by a **hard broker gate**, not prompts.

## Reference implementation
- `3465c75f^` = the commit BEFORE plan mode was removed. Port backend machinery from there:
  - `internal/team/plan_mode.go` (whole file).
  - `prompts.go`: `turnPosture`, `posturePlan`, `resolveTurnPosture`, `resolvePermissionFlags(ctx,slug)`.
  - `headless_claude.go`: ExitPlanMode harvest (`planning`, `planArtifact`, `extractClaudePlanArtifact`, `isExitPlanModeTool`) + emit plan event.
  - `headless_codex_runner.go`: `-s read-only` posture branch + `emitHeadlessPlan` from final message.
  - `broker_lifecycle_transition.go`: `LifecycleStatePlanning` enum, forward map, `isExecutableTeamTaskStatus`, `lifecycleStageFor`, reindex guard.
  - `broker_tasks_contracts.go` / `broker_types.go`: `PlanFirst` wire field (optional reuse).
- `5dc00c48` (#1058) is the original provider-native plan mode + harvest PR.

## Build order (check `go build ./...` after each)
- [x] L1. Re-add backend plan-mode machinery (ported from `3465c75f^`): `LifecycleStatePlanning` enum + forward/migration maps + `lifecycleStageFor` + reindex guard; `turnPosture`/`resolveTurnPosture`/`resolvePermissionFlags(ctx,slug)`; Claude `--permission-mode plan` + ExitPlanMode harvest; Codex `-s read-only` + plan emit; `plan_mode.go`; `HeadlessEventTypePlan`/`emitHeadlessPlan`; `planModeDirective` injected in `notifier_delivery.go`.
- [x] L2. **Default human goals to Planning** (`issueShouldPlanFirstLocked` in `broker_plan_approval.go`): new top-level owner-set Issue lands `Planning`; sub-issues + internal recovery actors exempt; broker field `disablePlanFirstDefault` (prod ON, eval fixture OFF).
- [x] L3. **Hard gate:** sub-issue create refused while parent in Planning; complete/submit refused from Planning (`broker_tasks_mutation_service.go`).
- [x] L4. **Approval via human_interview** (`broker_plan_approval.go`): planning turn → `RaisePlanApproval` (kind=approval, idempotent); approve → `startApprovedPlanTaskLocked` (Planning→Running + dispatch). Also direct task `approve` on Planning (human-only). Hooked into `applyRequestAnswerLocked`.
- [x] L5. **Dedup hardening** (`task_title_similarity.go`): normalize (lowercase, strip punctuation + stopwords, light stem) + token-set Jaccard ≥ 0.8 in `findReusableTaskLocked`. Sub-issue creates skip the global reuse-merge.
- [x] L6. **Shallow-subtask guard:** reject sub-issue whose title ≈ parent or an existing non-terminal sibling.
- [ ] L7. Prompts: Rule Zero + CEO sub-issue guidance updated for plan-first + dedupe. (DEFERRED — backend gates are the hard enforcement; prompt nudges are polish.)
- [x] L8. Frontend: `planning` re-added to `lifecycle.ts` (union, ALL_LIFECYCLE_STATES, stageForState, STATE_PILL_TOKENS, FILTER_TO_STATES), `lifecycleStageMap.json`, `TaskActivityStream` dot-kind. Plan surfaces via the channel post + existing human_interview approval card (no new UI gate). Dedicated `plan` HeadlessEvent card = optional polish (DEFERRED).
- [x] L9. Tests: new `broker_plan_approval_test.go` + `task_title_similarity_test.go`; updated lifecycle/migration/stage/service/verification/distill/completion/across-channel tests + web stagemap. `gofmt`/`go vet`/`go build`/`tsc` clean.

## Verification status
- Go build / vet / gofmt: clean.
- Targeted team tests (planning, dedup, shallow-subtask, verification, distill, lifecycle, migration, create/complete): PASS.
- Web: `tsc` clean; `lifecycle.test.ts` + `lifecycle.stagemap.test.ts` PASS; full web suite running.
- **Known pre-existing flake (NOT mine):** `TestOfficeEvals` fails intermittently on the `TempDir RemoveAll: directory not empty` teardown race (async distill/entity-regen goroutines vs fixture cleanup). Verified `origin/main` ALSO fails ~1/3. All eval CHECKS pass on my branch; my added create-path work raises the flake rate. Follow-up: drain async work before fixture cleanup.
- **Pre-existing failure (NOT mine):** `internal/workspaces` `TestResumeMarksErrorOnSpawnFailure` fails on clean `main` too.

## Remaining / follow-ups
- L7 prompt updates (Rule Zero + CEO sub-issue guidance) — optional, hard gates already enforce.
- Dedicated `plan` HeadlessEvent card in the stream view (polish).
- Wire planning as first-class coverage through the office-eval scenarios (instead of `disablePlanFirstDefault`).
- Fix the office-eval teardown race (drain async goroutines before cleanup).
- Screenshots for `web/` changes before PR ready.

## Risk flagged to user
Default-on planning partially reverses the recorded founder directive *"creating an Issue IS the authorization to work it"* — explicitly requested, treated as authorized. Planning stays skippable for trivial/internal/agent work so the office does not stall.
