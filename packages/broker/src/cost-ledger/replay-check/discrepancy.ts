// Wire and in-memory shapes for the replay-check oracle.
//
// `ReplayCheckReport` is the public output of `runReplayCheck` and is
// serialized verbatim by the cost-ledger HTTP route. `ReplayDiscrepancy`
// enumerates every failure mode the oracle can surface — adding a new
// variant is a wire-shape change, and renaming an existing field is a
// breaking change for consumers (dashboards, Go/Rust SDKs).
//
// `ReplayedBudget` and `ReplayedCrossing` are the in-memory shapes the
// scan loop builds before the comparators run. They live here so the
// types travel with the discrepancy union they feed.
//
// Decimal-string fields (`*MicroUsdString`) carry values that can
// legitimately exceed `MAX_BUDGET_LIMIT_MICRO_USD` (the `MicroUsd`
// brand ceiling) or `Number.MAX_SAFE_INTEGER`. Casting them back to
// `MicroUsd`-typed numbers would forge the brand and / or silently
// round, so the wire shape is a base-10 integer string. Pair with
// `unsafe_lifetime_accumulator` for the boundary signal.
import type { BudgetId, EventLsn, MicroUsd } from "@wuphf/protocol";

export interface ReplayCheckReport {
  readonly ok: boolean;
  readonly highestLsn: EventLsn;
  readonly eventsScanned: number;
  readonly discrepancies: readonly ReplayDiscrepancy[];
}

export type ReplayDiscrepancy =
  | {
      // Aggregate accumulators (`cost_by_agent.total_micro_usd`,
      // `cost_by_task.total_micro_usd`) are cumulative across the
      // lifetime of the ledger. The `MicroUsd` brand is bounded at
      // `MAX_BUDGET_LIMIT_MICRO_USD` (1e12), but these aggregates can
      // legitimately exceed that bound (a busy agent can spend more
      // than $1M micro-USD = 1e12 µUSD over time), and a hostile
      // projection row can carry arbitrary integer bytes. Emit as
      // decimal strings so the brand contract is preserved on the
      // wire and the diagnostic preserves exact integer values even
      // past `Number.MAX_SAFE_INTEGER`. The threshold-oracle path
      // (`threshold_crossing_unemitted` / `_spurious` /
      // `_oracle_observed_mismatch` / `_observed_mismatch`) carries
      // the same shape for the same reason.
      readonly kind: "agent_day_total_mismatch";
      readonly agentSlug: string;
      readonly dayUtc: string;
      readonly replayedMicroUsdString: string;
      readonly storedMicroUsdString: string;
    }
  | {
      readonly kind: "task_total_mismatch";
      readonly taskId: string;
      readonly replayedMicroUsdString: string;
      readonly storedMicroUsdString: string;
    }
  | {
      readonly kind: "agent_day_row_missing";
      readonly agentSlug: string;
      readonly dayUtc: string;
      readonly replayedMicroUsdString: string;
    }
  | {
      readonly kind: "agent_day_row_ghost";
      readonly agentSlug: string;
      readonly dayUtc: string;
      readonly storedMicroUsdString: string;
    }
  | {
      readonly kind: "task_row_missing";
      readonly taskId: string;
      readonly replayedMicroUsdString: string;
    }
  | {
      readonly kind: "task_row_ghost";
      readonly taskId: string;
      readonly storedMicroUsdString: string;
    }
  | {
      readonly kind: "budget_state_mismatch";
      readonly budgetId: BudgetId;
      readonly field:
        | "scope"
        | "subjectId"
        | "limitMicroUsd"
        | "thresholdsBps"
        | "setAtLsn"
        | "tombstoned";
      readonly replayed: unknown;
      readonly stored: unknown;
    }
  | {
      readonly kind: "budget_row_missing";
      readonly budgetId: BudgetId;
    }
  | {
      readonly kind: "budget_row_ghost";
      readonly budgetId: BudgetId;
    }
  | {
      readonly kind: "threshold_crossing_missing";
      readonly budgetId: BudgetId;
      readonly budgetSetLsn: EventLsn;
      readonly thresholdBps: number;
    }
  | {
      readonly kind: "threshold_crossing_ghost";
      readonly budgetId: BudgetId;
      readonly budgetSetLsn: EventLsn;
      readonly thresholdBps: number;
    }
  | {
      // `observedMicroUsd` lives on the sibling
      // `threshold_crossing_observed_mismatch` variant below so it
      // can carry decimal-string values. Cumulative oracle-observed
      // spend in the projection row is unbounded (a hostile or
      // tampered row can exceed `Number.MAX_SAFE_INTEGER`), so this
      // variant only carries safe-integer-bounded fields.
      readonly kind: "threshold_crossing_field_mismatch";
      readonly budgetId: BudgetId;
      readonly budgetSetLsn: EventLsn;
      readonly thresholdBps: number;
      readonly field: "crossedAtLsn" | "limitMicroUsd";
      readonly replayed: number;
      readonly stored: number;
    }
  | {
      // Projection drift on the cumulative `observedMicroUsd` field.
      // The replayed side comes from the protocol-validated event_log
      // audit (bounded number); the stored side comes from
      // `cost_threshold_crossings.observed_micro_usd`, which is
      // hostile DB input and can exceed both the `MicroUsd` brand
      // ceiling and `Number.MAX_SAFE_INTEGER`. Both fields are
      // decimal-string form to preserve exact bytes — the SQL read
      // uses `CAST(observed_micro_usd AS TEXT)` so a tampered row
      // past 2^53 flows through without rounding.
      readonly kind: "threshold_crossing_observed_mismatch";
      readonly budgetId: BudgetId;
      readonly budgetSetLsn: EventLsn;
      readonly thresholdBps: number;
      readonly replayedMicroUsdString: string;
      readonly storedMicroUsdString: string;
    }
  | {
      // Oracle: the independent threshold replay says this crossing
      // should have fired, but no `cost.budget.threshold.crossed` event
      // exists for it. Catches reactor under-emission bugs (e.g. a
      // regression in `crossesThreshold`'s integer math) that the
      // event-log-as-oracle replay can't see, because the missing event
      // is missing in BOTH the log and the projection.
      readonly kind: "threshold_crossing_unemitted";
      readonly budgetId: BudgetId;
      readonly budgetSetLsn: EventLsn;
      readonly thresholdBps: number;
      // Decimal-string form of the oracle's bigint cumulative `observed`.
      // Cumulative lifetime spend is unbounded; `MicroUsd` is bounded at
      // `MAX_BUDGET_LIMIT_MICRO_USD` and would forge the brand here.
      // Pair with `unsafe_lifetime_accumulator` for boundary signals.
      readonly observedMicroUsdString: string;
      readonly limitMicroUsd: MicroUsd;
      readonly crossedAtLsn: EventLsn;
    }
  | {
      // Oracle: a `cost.budget.threshold.crossed` event exists, but the
      // independent threshold replay says it shouldn't fire. Distinct
      // from `threshold_crossing_ghost` — that variant is projection-row
      // drift from event log. This variant is event-log drift from the
      // oracle's view of cost events + budgets.
      //
      // `eventLsn` is the `event_log.lsn` of the offending threshold
      // audit row so on-call can locate and surgically inspect it.
      readonly kind: "threshold_crossing_spurious";
      readonly budgetId: BudgetId;
      readonly budgetSetLsn: EventLsn;
      readonly thresholdBps: number;
      // Decimal-string form of the logged audit's `observedMicroUsd`.
      // The protocol bounds the per-event value at
      // `MAX_BUDGET_LIMIT_MICRO_USD`, but emitting via a string keeps
      // the field shape consistent with `threshold_crossing_unemitted`
      // and avoids brand drift if an adversary forges a value past
      // brand range that the decoder accepts.
      readonly observedMicroUsdString: string;
      readonly limitMicroUsd: MicroUsd;
      readonly crossedAtLsn: EventLsn;
      readonly eventLsn: EventLsn;
    }
  | {
      // Oracle: same (budget, epoch, threshold) exists in both the event
      // log and the oracle, but one of the recorded fields disagrees.
      // Most likely cause: the reactor and oracle diverged on which
      // cost_event was the trigger or what cumulative sum was observed.
      //
      // `crossedAtMs` is the `crossedAt` timestamp converted to ms-since-
      // epoch so all four fields share a `number` shape; on-call who
      // wants the human-readable timestamp can `new Date(value)`.
      readonly kind: "threshold_crossing_oracle_field_mismatch";
      readonly budgetId: BudgetId;
      readonly budgetSetLsn: EventLsn;
      readonly thresholdBps: number;
      // `observedMicroUsd` is now handled by the sibling
      // `threshold_crossing_oracle_observed_mismatch` variant so it can
      // carry unbounded bigint values without forging the `MicroUsd`
      // brand. The fields kept here are bounded by their wire shape:
      // crossedAtLsn ≤ event_log.lsn (safe integer in practice),
      // limitMicroUsd ≤ `MAX_BUDGET_LIMIT_MICRO_USD`, crossedAtMs ≤ JS
      // `Date.now()` range.
      readonly field: "crossedAtLsn" | "limitMicroUsd" | "crossedAtMs";
      readonly expected: number;
      readonly logged: number;
      readonly eventLsn: EventLsn;
    }
  | {
      // Oracle: like `threshold_crossing_oracle_field_mismatch` but for
      // `observedMicroUsd`. Cumulative lifetime spend is unbounded, so
      // expected/logged are decimal-string form of the bigint values.
      // Split out so the bounded-field variant can keep `number` and
      // the brand contract on its emitted fields.
      readonly kind: "threshold_crossing_oracle_observed_mismatch";
      readonly budgetId: BudgetId;
      readonly budgetSetLsn: EventLsn;
      readonly thresholdBps: number;
      readonly expectedMicroUsdString: string;
      readonly loggedMicroUsdString: string;
      readonly eventLsn: EventLsn;
    }
  | {
      // Oracle: more than one `cost.budget.threshold.crossed` event in
      // event_log shares the same `(budgetId, budgetSetLsn, thresholdBps)`
      // key. The projection PK suppresses duplicate rows
      // (`cost_threshold_crossings`) and `replayedCrossings` keys would
      // collapse duplicates into one entry, so the prior comparator
      // couldn't see this. A reactor over-emission bug or a forged
      // duplicate audit event surfaces here.
      readonly kind: "threshold_crossing_duplicate_event";
      readonly budgetId: BudgetId;
      readonly budgetSetLsn: EventLsn;
      readonly thresholdBps: number;
      readonly eventLsns: readonly EventLsn[];
    }
  | {
      // Oracle: the reactor invariant is that a `cost.budget.threshold.crossed`
      // event is appended AFTER its triggering `cost.event` in the same
      // SQLite transaction, so `eventLsn > crossedAtLsn` must hold. A
      // payload claiming a later cost-event LSN than its own
      // event_log row LSN means either a forgery or a reactor regression
      // that violates the §15.A "every commit reproduces state at every
      // LSN" invariant.
      readonly kind: "threshold_crossing_causal_order_violation";
      readonly budgetId: BudgetId;
      readonly budgetSetLsn: EventLsn;
      readonly thresholdBps: number;
      readonly eventLsn: EventLsn;
      readonly crossedAtLsn: EventLsn;
    }
  | {
      // Oracle: a `cost.budget.threshold.crossed` event names a
      // `crossedAtLsn` that has no corresponding `cost.event` in
      // event_log. Catches forged references that name an unrelated
      // LSN (e.g. a `cost.budget.set` row) — the existing causal-order
      // check only validates the numeric ordering, not that the
      // referenced row is actually a cost event.
      readonly kind: "threshold_crossing_dangling_reference";
      readonly budgetId: BudgetId;
      readonly budgetSetLsn: EventLsn;
      readonly thresholdBps: number;
      readonly eventLsn: EventLsn;
      readonly crossedAtLsn: EventLsn;
    }
  | {
      // Oracle: the reactor's strongest invariant is that threshold
      // events are appended SYNCHRONOUSLY immediately after their
      // triggering `cost.event`, all inside the same SQLite
      // transaction. So for a cost.event at LSN X with K legitimate
      // crossings, the threshold-event LSNs MUST be {X+1, …, X+K}.
      // A forged or delayed event appended much later (after a
      // tombstone, budget reset, or arbitrary gap) would otherwise
      // pass causal-order + reference + field checks. This variant
      // catches the gap.
      readonly kind: "threshold_crossing_delayed_emission";
      readonly budgetId: BudgetId;
      readonly budgetSetLsn: EventLsn;
      readonly thresholdBps: number;
      readonly eventLsn: EventLsn;
      readonly crossedAtLsn: EventLsn;
      readonly expectedWindowMinLsn: EventLsn;
      readonly expectedWindowMaxLsn: EventLsn;
    }
  | {
      // Oracle: a `cost.budget.threshold.crossed` event names a
      // `budgetSetLsn` that has no corresponding `cost.budget.set` in
      // event_log. Sibling of `dangling_reference` for the budget
      // side. Catches forgeries that point at an unrelated row OR
      // at a budgetSet for a different budgetId.
      readonly kind: "threshold_crossing_invalid_budget_set_reference";
      readonly budgetId: BudgetId;
      readonly budgetSetLsn: EventLsn;
      readonly thresholdBps: number;
      readonly eventLsn: EventLsn;
      readonly reason: "lsn_not_a_budget_set" | "budget_id_mismatch";
      /** When `reason === "budget_id_mismatch"`, the actual budgetId at the named LSN. */
      readonly actualBudgetId?: BudgetId;
    }
  | {
      // Oracle: one of the lifetime accumulators (`oracleGlobalLifetime`,
      // `oracleAgentLifetime[slug]`, `oracleTaskLifetime[taskId]`) just
      // crossed a representability boundary. Fires once per
      // (scope, subjectId, reason) per replay-check run.
      //
      // Two boundaries fire independently:
      //
      // - `exceeds_micro_usd_brand`: post > `MAX_BUDGET_LIMIT_MICRO_USD`
      //   (1e12). The accumulator has surpassed the documented
      //   `MicroUsd` brand ceiling. Downstream consumers that re-validate
      //   `MicroUsd` payloads should not accept values past this point.
      //   Oracle discrepancies that carry cumulative observed totals
      //   (`threshold_crossing_unemitted`,
      //   `threshold_crossing_spurious`,
      //   `threshold_crossing_oracle_observed_mismatch`) emit decimal
      //   strings instead of branded `MicroUsd` to avoid forging the
      //   brand past this boundary.
      //
      // - `exceeds_safe_integer`: post > `Number.MAX_SAFE_INTEGER`
      //   (≈ 9e15). The accumulator has surpassed the JS-`number`
      //   precision boundary. The internal math is now bigint, but any
      //   `Number(observed)` conversion would lose precision past this
      //   point. The decimal-string emission keeps the wire shape
      //   exact, but on-call should still treat any number-typed
      //   derivative as suspect once this fires.
      readonly kind: "unsafe_lifetime_accumulator";
      readonly reason: "exceeds_micro_usd_brand" | "exceeds_safe_integer";
      readonly scope: "global" | "agent" | "task";
      /** `null` for global; agentSlug or taskId for the other two scopes. */
      readonly subjectId: string | null;
      /** Cost event whose append pushed the accumulator past the boundary. */
      readonly costEventLsn: EventLsn;
      /** Accumulated total as a decimal string. Bigint, JSON-safe. */
      readonly accumulatedMicroUsdString: string;
    }
  | {
      // Surface unparseable event-log rows distinctly so on-call sees the
      // failing LSN, event type, and parse reason instead of a bare
      // `internal_error` from the route.
      readonly kind: "event_payload_unparseable";
      readonly lsn: EventLsn;
      readonly type: string;
      readonly reason: string;
    };

// In-memory shape the scan loop builds for each `cost.budget.set`
// event. The replay loop indexes these by budget id and feeds them to
// the threshold-oracle's `computeExpectedCrossings`.
export interface ReplayedBudget {
  readonly scope: "global" | "agent" | "task";
  readonly subjectId: string | null;
  readonly limitMicroUsd: number;
  readonly thresholdsBps: readonly number[];
  readonly setAtLsn: number;
  readonly tombstoned: boolean;
}

// In-memory shape for a `cost.budget.threshold.crossed` event the
// scan loop has decoded from the event_log. Compared against the
// projection row (`ThresholdCrossingDbRow`) and the oracle's
// independent expectation (`ExpectedCrossing`) downstream.
export interface ReplayedCrossing {
  readonly budgetId: string;
  readonly budgetSetLsn: number;
  readonly thresholdBps: number;
  readonly crossedAtLsn: number;
  readonly observedMicroUsd: number;
  readonly limitMicroUsd: number;
  /** `crossedAt` from the audit payload, as ms-since-epoch. */
  readonly crossedAtMs: number;
  /** The `event_log.lsn` of this threshold-crossed audit row itself. */
  readonly eventLsn: number;
}
