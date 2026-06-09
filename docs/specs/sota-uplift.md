# Spec — SOTA Uplift: Compounding, Coordination, Outcomes

**Status:** ACTIVE — master tracker for the SOTA uplift lane
**Author:** Najmuzzaman + Claude, 2026-06-09
**Source analysis:** 30-agent gap analysis vs SOTA (Anthropic multi-agent research system, Cognition/Devin, LangGraph/OpenAI Agents SDK/Magentic-One, Letta/Mem0/ACE/Voyager, Codex/Cursor/Factory). 14 claims adversarially verified against `main@b9d0c878`.
**Supersedes:** the sequencing of the 2026-06-09 multi-agent overhaul plan. Its Phase 1 (chat-only tasks, PR #1052) lands independently; its Phases 2–4 are absorbed below (U3.4, U5.1).

▶ **RESUME HERE (2026-06-10, overnight run):** U0, U1, U2, U3.2/U3.3, U4.1 SHIPPED on branch `feat/sota-uplift-phase0` (worktree `.worktrees/sota-uplift`, PR #1062). Eval harness: **13/13 checks green, 0 known gaps** (`go run ./cmd/office-eval`). The moat loop is closed and regression-guarded: verified done → auto-distilled trusted learning → injected into the next similar task's packet, zero human steps.

Shipped: U0.1 eval harness (`internal/team/office_eval.go` + `cmd/office-eval`, runs in CI via TestOfficeEvals; known-gap markers auto-demand promotion to regression guards) · U0.2 packet/prompt economics flip · U1.1 verification-gated done (`task_verification.go`; gate runs outside b.mu; failures re-enter the owner's next packet) · U1.2 web evidence surfaces (VerificationBadge + proof panel + DoD rail) · U2.1/U2.2 task-scoped knowledge injection (`context_assembler.go`, IDF token-overlap, dense-rerank seam behind `relevantLearnings`) · U2.3/U3.3 per-task turn journal (`task_ledger.go`, broker-observed facts, packet-rendered) · U3.2 dependency edges carry upstream outcomes · U4.1 auto-distillation of verified outcomes (`task_distill.go`, `AppendVerified` trust path).

Next unstarted: U3.1 rest (objective/inputs/expected_artifact on team_task — reconcile with PR #1057's surface after it merges) · U3.4 (covered by open PR #1057) · U3.5 prompt-rule demolition · U4.2 one knowledge view · U4.3 skills-from-verified-runs · U4.4 human-facing knowledge-used UI (agent-side citation already in packets) · U5.1 (covered by open PR #1060) · U5.2/U5.3. NOTE: open PRs #1052/#1053/#1057/#1060 overlap prompt_builder/broker/queue — merge main promptly after any of them lands.

---

## The decision

We stop optimizing for token cost and start optimizing for outcome quality. The fresh-session/keyhole-packet/cache-stable-prefix economics were the right call for a cheap demo and are the wrong call for the product. SOTA systems spend 4–15× chat-level tokens and that spend is what buys reliability (Anthropic's multi-agent research numbers). Token budgeting is no longer a design constraint anywhere in this plan.

## North-star metrics (built in U0, gate every phase)

1. **Verified-done rate** — % of eval jobs reaching a machine-verified done state.
2. **Compounding delta (THE MOAT METRIC)** — eval suite run cold (fresh office) vs warm (office with accumulated learnings/skills/wiki): success-rate and turn-count delta. If warm isn't measurably better, the moat doesn't exist yet.
3. **Coordination multiplier** — multi-agent task success vs the same job given to one agent in one window. The "10x over single-window chat" claim, measured.
4. **Trust cost** — human interventions (approvals, corrections, re-asks) per verified-done task.

## Root causes being fixed (verified, file:line)

1. **Cost-first harness.** Fresh `claude --print` per turn, no resume/compaction (`internal/team/headless_claude.go`); specialists wake with 4 chat messages (`notification_context.go:504`), 512-char task details (`:494,:565`), 1000-char trigger (`:608`); prompt actively discourages context gathering ("Every tool call pays full turn cost", `prompt_builder.go:188,330`; "team_poll: LAST RESORT").
2. **Prose substrate.** Coordination via chat + ~71 numbered prompt patch-rules (~977-line `prompt_builder.go`); delegation parsed by regex (`internal/orchestration/delegator.go`); dependency edges carry scheduling only — upstream outcomes never injected into dependents (`notification_context.go` BuildTaskExecutionPacket).
3. **No ground truth.** `complete/approve` are unchecked string flips (`broker_tasks_mutation_service.go` markTaskDone); learning injection is a global top-8 with no query/task scoping (`prompts.go:68-82`); embeddings exist but unwired to search (`wiki_query_retrieve.go`); only token costs are benchmarked, never outcomes.

**Keep (good bones):** typed LifecycleState machine + dependency cascade, persistent scheduler jobs, signals/decisions audit ledger, per-agent worktrees, the deterministic-integrations gate, `rejectTheaterTaskForLiveBusiness` enforcement instinct.

---

## Phases

Sequencing logic: measurement first (U0), then ground truth (U1) because unverified claims poison every store downstream, then context (U2) because every other behavior is capped by what agents can see, then coordination (U3), then the compounding loop (U4) which now compounds *verified* knowledge through *rich* context, then agent definition (U5).

### U0 — Foundations flip (economics inversion + measurement)

- **U0.1 Outcome eval harness** (`evals/office/`). 6–8 canonical office jobs derived from `docs/specs/canonical-agent-workflow.md` beats (draft replies + capture contacts; research-and-publish wiki; build-and-verify small feature; multi-agent launch package; etc.). Runner boots a scratch office, submits the job, machine-scores the outcome (artifact exists, check passes, external record present). Two modes: `--cold` (fresh workspace) and `--warm` (workspace pre-loaded with accumulated knowledge) → emits the compounding delta. Wire into CI as non-blocking report first.
- **U0.2 Context starvation quick-flip** (shippable immediately, no new architecture):
  - `notification_context.go`: specialist `contextLimit` 4 → 20 (parity with lead); task details truncation 512 → 4096; trigger content 1000 → 4000; active-task details 96 → 512.
  - `prompt_builder.go`: delete "Every tool call pays full turn cost" and the team_poll "LAST RESORT" framing — replace with "gather the context you need; prefer pushed context but pull freely when it's missing."
  - `ARCHITECTURE.md`: rewrite the "three load-bearing choices" section — fresh sessions stay (for now), but the cost rationale is replaced by the outcome rationale; the 9× benchmark is demoted from flagship to footnote.
- **U0.3 Done:** evals run green in CI report mode; baseline numbers recorded in this doc.

### U1 — Ground truth (verification-gated done)

- **U1.1 Verification spec on tasks.** Extend `teamTask` with `verification {kind: command|artifact|url|external_record|none, spec, required}`. Intake/CEO sets it at creation (intake Spec already has `acceptanceCriteria` — map it). Broker executes the check inside the owner's worktree on `complete`/`approve`; failure blocks the transition and re-queues the task with the failure output injected into the next execution packet (ground truth re-enters context). `none` allowed but rendered as UNVERIFIED.
- **U1.2 Evidence surfaces.** Done card shows the proof (test output tail, artifact link, PR URL, external record id). Verified/unverified badges on completion claims in chat + task UI. Specialist completion claims without attached evidence get bounced by the broker, not by prompt rule 11b — then delete rule 11b.
- **U1.3 Done:** verified-done rate measurable on the U0 evals; no eval job can pass via self-declaration.

### U2 — Context spine (task-scoped, ranked, budgeted-by-relevance)

- **U2.1 Hybrid retrieval.** Wire `internal/embedding` into wiki + learning search; fuse with bleve BM25 via RRF (recorded house lesson). One retrieval API over wiki/notebook/learnings — physical store unification deferred to U4.2.
- **U2.2 Context assembler.** Replace fixed-count packets in `BuildMessageWorkPacket`/`BuildTaskExecutionPacket` with an assembler that ranks candidates against the trigger + task spec: full task thread tail, full spec, top-k learnings (query = task title+details — kills the global top-8 in `prompts.go:68`), top-k wiki hits, upstream dependency artifacts, the agent's task ledger (U2.3). Generous packet budget (~40k tokens), relevance-ordered.
- **U2.3 Per-(agent,task) state ledger.** At turn end, distill what was tried/decided/failed into a structured ledger record; inject verbatim on next wake for that task. Substrate for U3 handoffs and U5 legibility. (Provider `--resume` sessions stay a later optimization — the ledger is provider-agnostic and doubles as the audit artifact.)
- **U2.4 Done:** cold eval turn-counts drop (agents stop re-discovering); compounding delta becomes nonzero for the first time.

### U3 — Real coordination (typed handoffs, data-carrying edges)

- **U3.1 Typed delegation.** Extend `team_task` create with `objective`, `inputs` (artifact refs), `expected_artifact`, `verification` (from U1). Retire the regex delegator path (`internal/orchestration/delegator.go`) for office dispatch.
- **U3.2 Dependency edges carry data.** On approve/unblock, attach the upstream task's outcome (result summary, artifact body/link, verification proof) to every dependent's execution packet. Cheapest, highest-leverage collaboration fix.
- **U3.3 Living task brief.** Per-task compressed running brief (decisions, current state, open questions) updated each turn by the acting agent's distillation step (reuses U2.3); every participant — agent or human — wakes with the full brief. This is the shared-context contract Cognition's principles demand.
- **U3.4 CEO decomposition upgrade.** Parallel sub-task creation with joins, re-planning on verification failure, effort-scaling guidance (Anthropic's rubric: simple = 1 agent, complex = parallel lanes). Absorbs overhaul Phase 3.
- **U3.5 Prompt-rule demolition pass.** Every invariant that moved into code (U1.2, U3.1, U3.2) gets its prompt rule deleted; personality/banter mandates (`officeVibeBlock`, `teamVoiceForSlug`) confined to human-facing surfaces, stripped from agent-to-agent turns. Target: prompt_builder.go under ~400 lines.
- **U3.6 Done:** coordination multiplier measured on a multi-agent eval job; transcript review shows zero re-asked questions across a 3-agent task.

### U4 — Compounding loop (the moat)

- **U4.1 Auto-distillation.** Post-task broker step (async, off the hot path — avoids the old auto-writer deadlock) runs a distiller over the task's event stream, gated on the verification result: verified outcomes write structured learning records automatically; failures write Reflexion-style lessons. Restores the `autoNotebookWriter` seam with the salience filter it always needed.
- **U4.2 One knowledge view.** Single human-facing knowledge surface with provenance (which task produced it, which tasks used it) and confirm/edit/delete. Wiki becomes the curated projection; notebook/learning stores feed it. Librarian curates ranking, not gatekeeping writes.
- **U4.3 Skills from verified runs.** A verified-done task with a reusable shape auto-proposes a skill (artifact + outcome attached); skills track success/rework rate, not just invocation count; low-risk skills auto-enable, human review reserved for skills granting external authority (Voyager admission rule).
- **U4.4 Attribution surface.** Each Issue shows "knowledge used: learnings X,Y · skill Z · wiki A" and each knowledge item shows its impact stats. Compounding becomes *witnessed*, not just real.
- **U4.5 Done:** warm-vs-cold delta is positive and visible in-product; a returning user can point at the screen and say "it got better."

### U5 — Agent definition & working spaces

- **U5.1 Agent file framework.** SOUL / IDENTITY / OPERATIONS / TOOLS + USER files per agent (absorbs overhaul Phase 4, NO heartbeat). Agent definitions become legible, editable artifacts instead of Go-string prompt blocks.
- **U5.2 Legible workspaces.** Each agent subspace surfaces its ledger, notebook, skills, active work, and a structured decision timeline rendered from the headless event stream (tool call → result → state change) — work becomes inspectable data, not narrated chat.
- **U5.3 Risk-tiered gates.** Collapse the ~9 approval ceremonies: read-only/reversible actions auto-run under standing policy, Issue-approval and first-action approval merge, inbox ceremony reserved for irreversible/external actions (extends the deterministic-integrations grant model).
- **U5.4 Done:** trust-cost metric drops while verified-done rate holds.

---

## Decision log

| # | Decision | Rationale |
|---|---|---|
| D1 | Ledger-based continuity now; provider `--resume` sessions later | Provider-agnostic (codex too), survives restarts via existing resume.go story, doubles as handoff + legibility artifact |
| D2 | Verification runs in the owner's worktree via the broker | Reuses isolation we already trust; no new sandbox layer |
| D3 | One retrieval spine over 4 stores before physically merging them | Migration risk deferred; the read path is what's broken |
| D4 | Extend `team_task`, don't invent a new delegation primitive | EXTEND-don't-duplicate; lifecycle machine is good bones |
| D5 | Eval harness lands first and gates every phase | House lesson: benchmark infra in parallel with features; without it the 10x claim stays a vibe |
| D6 | Prompt cache hit-rate is explicitly allowed to regress | The cache constraint caused the global-context disease (byte-stable prefixes ⇒ task-blind context) |

## PR rhythm

One slice = one draft PR with tests + screenshots (web) + eval delta noted in the description. Full Go + web suites per slice (house rule from the structural-changes lane). Multi-round review rhythm for U1.1 (security boundary: broker executes commands) and U3.1 (wire shape).
