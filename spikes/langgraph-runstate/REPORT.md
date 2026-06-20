# Spike report: P4 run-state migration (LangGraph checkpointer ↔ WUPHF lifecycle)

> Throwaway spike. Branch `worktree-deepagents-harness-eval`. 2026-06-19.
> Question: can WUPHF's real 13-state task lifecycle round-trip through a LangGraph
> checkpointer without losing the lifecycle position — the highest-risk part of the
> orchestration move (TODOS.md #0: "task moves to unknown and refuses to start")?

## Verdict

**Run-state seam validated, and it picks a winner.** WUPHF's real lifecycle (ported
faithfully from `broker_lifecycle_transition.go`: 13 states, the 4-tuple forward map, the
migration map, the fail-loud `unknown`) round-trips through LangGraph **in both ownership
variants, for all 13 states.** The evidence favors the **re-hydrate** variant. Two concrete
design rules fall out, below.

## Setup

- Faithful port: `lifecycle_model.py` mirrors the Go state set + `lifecycleDerivedFields` +
  `lifecycleMigrationMap` + `deriveLifecycleStateFromLegacy` + the bare-status fallback.
- LangGraph `0.x` + `langgraph-checkpoint-sqlite 3.1.0`. No broker, no API key — pure
  orchestration-state mechanics.
- Fixtures synthesized from the canonical map + adversarial tuples (no real `~/.wuphf` state
  read — exactly the anonymized-tuple approach TODOS.md #0 prescribes).
- Repro: `../deepagents-seam/.venv/bin/python runstate_probe.py`

## Results

| Layer | What it proves | Result |
|---|---|---|
| **A — lossiness** | Is the legacy 4-tuple enough to recover the state? | **10/13 lossless; 3 collapse** if `lifecycle_state` is dropped: `intake→ready`, `decision→review`, `changes_requested→running` |
| **B — pure** | LangGraph SqliteSaver as run-state store: migrate → restart → resume | ✅ **13/13** restored + resumed to the correct next state |
| **C — re-hydrate** | Go record authoritative; ephemeral saver rebuilt on restart | ✅ **13/13** rehydrated + resumed correctly |
| **D — adversarial** | Contradictory/garbage tuples must fail loud | ✅ all 3 contradictions → `unknown` (operator triage); legacy alias *names* (`merged`, `blocked_on_pr_merge`) normalize losslessly |

`results.json` holds the raw per-state output.

## Two design rules this nails down

1. **Carry `lifecycle_state` directly — never re-derive from the 4-tuple.** The 4-tuple is
   lossy for 3 states (A). Modern `broker-state.json` already persists `lifecycle_state`, so
   the migration is lossless for all 13 by just carrying that field. 4-tuple derivation is a
   *legacy-only fallback* (pre-Lane-A snapshots with no state field), where the 3 collapses
   are the documented, accepted loss.

2. **Prefer the re-hydrate variant (C) over pure-checkpoint (B).** Both pass 13/13, but
   re-hydrate keeps the **Go broker record as the single source of durable truth** and treats
   the LangGraph checkpoint as a rebuildable cache. That:
   - removes the scary physical migration (no moving durable run-state into a new SQLite store);
   - eliminates dual-source-of-truth drift (the checkpoint is derived, not authoritative);
   - makes a lost/corrupt checkpoint *recoverable* (rebuild from the Go record) instead of data loss.
   It still satisfies D4 ("LangGraph owns orchestration") — LangGraph decides what runs; Go just
   holds the durable record it rehydrates from. The only thing pure-checkpoint buys (arbitrary
   sub-step time-travel) isn't needed: lifecycle-granularity rebuild is enough.

## What this de-risks

The TODOS.md #0 failure mode — "task lands in `unknown` and refuses to start" — is now a
**safe, surfaced** failure, not silent corruption: contradictory tuples resolve to `unknown`
for operator triage (D), and modern tasks never hit derivation at all (rule 1). The migration's
worst case is a flagged task, not a wrong one.

## NOT covered here (carry into P4 build)

- Real production `broker-state.json` fixtures (still TODOS.md #0 — synthesized tuples here).
- The full task record beyond lifecycle position (owner, deps, messages, packet) — the
  orchestrator needs those too; this spike isolated the lifecycle state.
- Concurrency/lane semantics (separate §7 risk in the plan).
