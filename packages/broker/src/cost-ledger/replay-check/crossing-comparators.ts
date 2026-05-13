// Comparators for the `threshold_crossing_*` discrepancy variants.
//
// Three categories live in this module:
//
//   1. Projection-vs-log comparators that surface drift between the
//      reactor's `cost_threshold_crossings` projection and the
//      `cost.budget.threshold.crossed` audit rows in event_log
//      (`compareCrossings`).
//   2. Log-internal integrity detectors that catch reactor invariants
//      violated INSIDE the audit log itself: duplicate emission,
//      causal-order violations, dangling references, and the
//      delayed-emission gap (`detectDuplicateLoggedCrossings`,
//      `detectCausalOrderViolations`, `validateBudgetSetReferences`,
//      `detectDelayedEmissions`, `validateLoggedCrossingReferences`).
//   3. Oracle-vs-log comparators that surface drift between the
//      independent threshold-oracle's expectations and the logged
//      audit (`compareExpectedAndLoggedCrossings`).
//
// All of these emit into a shared `out: ReplayDiscrepancy[]` so the
// orchestrator can merge them with the other comparator outputs in
// one final report.
import { type BudgetId, lsnFromV1Number, type MicroUsd } from "@wuphf/protocol";
import type { ReplayDiscrepancy, ReplayedCrossing } from "./discrepancy.ts";
import { crossingKey, type ThresholdCrossingDbRow } from "./internal.ts";
import type { ExpectedCrossing } from "./threshold-oracle.ts";

export function compareCrossings(
  replayed: ReadonlyMap<string, readonly ReplayedCrossing[]>,
  stored: ReadonlyMap<string, ThresholdCrossingDbRow>,
  out: ReplayDiscrepancy[],
): void {
  for (const [key, entries] of replayed.entries()) {
    // The projection PK collapses duplicate audit events into one row,
    // so log-vs-projection comparison uses the first event seen. The
    // duplicate case is surfaced separately by
    // `detectDuplicateLoggedCrossings`.
    const expected = entries[0];
    if (expected === undefined) continue; // unreachable: map only holds non-empty arrays
    const actual = stored.get(key);
    if (actual === undefined) {
      out.push({
        kind: "threshold_crossing_missing",
        budgetId: expected.budgetId as BudgetId,
        budgetSetLsn: lsnFromV1Number(expected.budgetSetLsn),
        thresholdBps: expected.thresholdBps,
      });
      continue;
    }
    if (actual.crossedAtLsn !== expected.crossedAtLsn) {
      out.push({
        kind: "threshold_crossing_field_mismatch",
        budgetId: expected.budgetId as BudgetId,
        budgetSetLsn: lsnFromV1Number(expected.budgetSetLsn),
        thresholdBps: expected.thresholdBps,
        field: "crossedAtLsn",
        replayed: expected.crossedAtLsn,
        stored: actual.crossedAtLsn,
      });
    }
    // Stored side is a decimal string from `CAST(...)`; widen the
    // protocol-bounded replayed side via `String(...)` for byte-exact
    // comparison. Equal-on-bytes implies equal-as-integers.
    const expectedObservedString = String(expected.observedMicroUsd);
    if (actual.observedMicroUsd !== expectedObservedString) {
      out.push({
        kind: "threshold_crossing_observed_mismatch",
        budgetId: expected.budgetId as BudgetId,
        budgetSetLsn: lsnFromV1Number(expected.budgetSetLsn),
        thresholdBps: expected.thresholdBps,
        replayedMicroUsdString: expectedObservedString,
        storedMicroUsdString: actual.observedMicroUsd,
      });
    }
    if (actual.limitMicroUsd !== expected.limitMicroUsd) {
      out.push({
        kind: "threshold_crossing_field_mismatch",
        budgetId: expected.budgetId as BudgetId,
        budgetSetLsn: lsnFromV1Number(expected.budgetSetLsn),
        thresholdBps: expected.thresholdBps,
        field: "limitMicroUsd",
        replayed: expected.limitMicroUsd,
        stored: actual.limitMicroUsd,
      });
    }
  }
  for (const [key, actual] of stored.entries()) {
    if (!replayed.has(key)) {
      out.push({
        kind: "threshold_crossing_ghost",
        budgetId: actual.budgetId as BudgetId,
        budgetSetLsn: lsnFromV1Number(actual.budgetSetLsn),
        thresholdBps: actual.thresholdBps,
      });
    }
  }
}

export function detectDuplicateLoggedCrossings(
  replayed: ReadonlyMap<string, readonly ReplayedCrossing[]>,
  out: ReplayDiscrepancy[],
): void {
  for (const entries of replayed.values()) {
    if (entries.length <= 1) continue;
    const first = entries[0];
    if (first === undefined) continue; // unreachable
    out.push({
      kind: "threshold_crossing_duplicate_event",
      budgetId: first.budgetId as BudgetId,
      budgetSetLsn: lsnFromV1Number(first.budgetSetLsn),
      thresholdBps: first.thresholdBps,
      eventLsns: entries.map((e) => lsnFromV1Number(e.eventLsn)),
    });
  }
}

export function detectCausalOrderViolations(
  replayed: ReadonlyMap<string, readonly ReplayedCrossing[]>,
  out: ReplayDiscrepancy[],
): void {
  for (const entries of replayed.values()) {
    for (const entry of entries) {
      // The reactor appends `cost.budget.threshold.crossed` AFTER its
      // triggering `cost.event` in the same SQLite transaction, so the
      // threshold event's own LSN must strictly exceed the LSN it
      // names as the trigger.
      if (entry.eventLsn <= entry.crossedAtLsn) {
        out.push({
          kind: "threshold_crossing_causal_order_violation",
          budgetId: entry.budgetId as BudgetId,
          budgetSetLsn: lsnFromV1Number(entry.budgetSetLsn),
          thresholdBps: entry.thresholdBps,
          eventLsn: lsnFromV1Number(entry.eventLsn),
          crossedAtLsn: lsnFromV1Number(entry.crossedAtLsn),
        });
      }
    }
  }
}

export function validateBudgetSetReferences(
  replayed: ReadonlyMap<string, readonly ReplayedCrossing[]>,
  budgetSetLsnToBudgetId: ReadonlyMap<number, string>,
  out: ReplayDiscrepancy[],
): void {
  for (const entries of replayed.values()) {
    for (const entry of entries) {
      const actualBudgetId = budgetSetLsnToBudgetId.get(entry.budgetSetLsn);
      if (actualBudgetId === undefined) {
        out.push({
          kind: "threshold_crossing_invalid_budget_set_reference",
          budgetId: entry.budgetId as BudgetId,
          budgetSetLsn: lsnFromV1Number(entry.budgetSetLsn),
          thresholdBps: entry.thresholdBps,
          eventLsn: lsnFromV1Number(entry.eventLsn),
          reason: "lsn_not_a_budget_set",
        });
        continue;
      }
      if (actualBudgetId !== entry.budgetId) {
        out.push({
          kind: "threshold_crossing_invalid_budget_set_reference",
          budgetId: entry.budgetId as BudgetId,
          budgetSetLsn: lsnFromV1Number(entry.budgetSetLsn),
          thresholdBps: entry.thresholdBps,
          eventLsn: lsnFromV1Number(entry.eventLsn),
          reason: "budget_id_mismatch",
          actualBudgetId: actualBudgetId as BudgetId,
        });
      }
    }
  }
}

export function detectDelayedEmissions(
  replayed: ReadonlyMap<string, readonly ReplayedCrossing[]>,
  expected: ReadonlyMap<string, ExpectedCrossing>,
  out: ReplayDiscrepancy[],
): void {
  // Count expected crossings per triggering cost-event LSN. That count
  // IS the size of the contiguous emission window the reactor would
  // produce in the same SQLite transaction as the cost.event.
  const expectedCountByCostEventLsn = new Map<number, number>();
  for (const exp of expected.values()) {
    expectedCountByCostEventLsn.set(
      exp.crossedAtLsn,
      (expectedCountByCostEventLsn.get(exp.crossedAtLsn) ?? 0) + 1,
    );
  }
  for (const entries of replayed.values()) {
    for (const entry of entries) {
      // Spurious entries (no expected crossing for this key) are
      // surfaced by `compareExpectedAndLoggedCrossings`; skip them
      // here to avoid double-noise.
      const key = crossingKey(entry.budgetId, entry.budgetSetLsn, entry.thresholdBps);
      if (!expected.has(key)) continue;
      const expectedCount = expectedCountByCostEventLsn.get(entry.crossedAtLsn) ?? 0;
      if (expectedCount === 0) continue; // unreachable: expected.has(key) implies the count is ≥1
      const minLsn = entry.crossedAtLsn + 1;
      const maxLsn = entry.crossedAtLsn + expectedCount;
      if (entry.eventLsn < minLsn || entry.eventLsn > maxLsn) {
        out.push({
          kind: "threshold_crossing_delayed_emission",
          budgetId: entry.budgetId as BudgetId,
          budgetSetLsn: lsnFromV1Number(entry.budgetSetLsn),
          thresholdBps: entry.thresholdBps,
          eventLsn: lsnFromV1Number(entry.eventLsn),
          crossedAtLsn: lsnFromV1Number(entry.crossedAtLsn),
          expectedWindowMinLsn: lsnFromV1Number(minLsn),
          expectedWindowMaxLsn: lsnFromV1Number(maxLsn),
        });
      }
    }
  }
}

export function validateLoggedCrossingReferences(
  replayed: ReadonlyMap<string, readonly ReplayedCrossing[]>,
  costEventOccurredAtMs: ReadonlyMap<number, number>,
  out: ReplayDiscrepancy[],
): void {
  for (const entries of replayed.values()) {
    for (const entry of entries) {
      const referencedMs = costEventOccurredAtMs.get(entry.crossedAtLsn);
      if (referencedMs === undefined) {
        // The named trigger LSN doesn't correspond to any cost.event
        // we replayed. Either the threshold event names a non-cost
        // row (e.g. a `cost.budget.set` LSN) or an LSN that doesn't
        // exist in event_log.
        out.push({
          kind: "threshold_crossing_dangling_reference",
          budgetId: entry.budgetId as BudgetId,
          budgetSetLsn: lsnFromV1Number(entry.budgetSetLsn),
          thresholdBps: entry.thresholdBps,
          eventLsn: lsnFromV1Number(entry.eventLsn),
          crossedAtLsn: lsnFromV1Number(entry.crossedAtLsn),
        });
        continue;
      }
      if (referencedMs !== entry.crossedAtMs) {
        // The threshold event's payload `crossedAt` doesn't match the
        // cost.event it names as trigger. Catches forged timestamps
        // (key matches but timestamp drifts) AND per-duplicate
        // divergence the first-entry-only oracle comparator misses.
        out.push({
          kind: "threshold_crossing_oracle_field_mismatch",
          budgetId: entry.budgetId as BudgetId,
          budgetSetLsn: lsnFromV1Number(entry.budgetSetLsn),
          thresholdBps: entry.thresholdBps,
          field: "crossedAtMs",
          expected: referencedMs,
          logged: entry.crossedAtMs,
          eventLsn: lsnFromV1Number(entry.eventLsn),
        });
      }
    }
  }
}

export function compareExpectedAndLoggedCrossings(
  expected: ReadonlyMap<string, ExpectedCrossing>,
  logged: ReadonlyMap<string, readonly ReplayedCrossing[]>,
  out: ReplayDiscrepancy[],
): void {
  for (const [key, exp] of expected.entries()) {
    const entries = logged.get(key);
    // Compare against the first logged event per key. Duplicates are
    // surfaced by `detectDuplicateLoggedCrossings` so the
    // oracle-vs-log comparator focuses on shape drift.
    const log = entries?.[0];
    if (log === undefined) {
      out.push({
        kind: "threshold_crossing_unemitted",
        budgetId: exp.budgetId as BudgetId,
        budgetSetLsn: lsnFromV1Number(exp.budgetSetLsn),
        thresholdBps: exp.thresholdBps,
        observedMicroUsdString: exp.observedMicroUsd.toString(),
        limitMicroUsd: exp.limitMicroUsd as MicroUsd,
        crossedAtLsn: lsnFromV1Number(exp.crossedAtLsn),
      });
      continue;
    }
    if (log.crossedAtLsn !== exp.crossedAtLsn) {
      out.push({
        kind: "threshold_crossing_oracle_field_mismatch",
        budgetId: exp.budgetId as BudgetId,
        budgetSetLsn: lsnFromV1Number(exp.budgetSetLsn),
        thresholdBps: exp.thresholdBps,
        field: "crossedAtLsn",
        expected: exp.crossedAtLsn,
        logged: log.crossedAtLsn,
        eventLsn: lsnFromV1Number(log.eventLsn),
      });
    }
    // Compare cumulative observed in bigint to preserve precision past
    // the safe-integer boundary. Logged is the protocol-validated audit
    // value (number, ≤ MAX_BUDGET_LIMIT_MICRO_USD); widen to bigint for
    // an exact compare against the oracle's unbounded accumulator.
    const loggedObservedBig = BigInt(log.observedMicroUsd);
    if (loggedObservedBig !== exp.observedMicroUsd) {
      out.push({
        kind: "threshold_crossing_oracle_observed_mismatch",
        budgetId: exp.budgetId as BudgetId,
        budgetSetLsn: lsnFromV1Number(exp.budgetSetLsn),
        thresholdBps: exp.thresholdBps,
        expectedMicroUsdString: exp.observedMicroUsd.toString(),
        loggedMicroUsdString: loggedObservedBig.toString(),
        eventLsn: lsnFromV1Number(log.eventLsn),
      });
    }
    if (log.limitMicroUsd !== exp.limitMicroUsd) {
      out.push({
        kind: "threshold_crossing_oracle_field_mismatch",
        budgetId: exp.budgetId as BudgetId,
        budgetSetLsn: lsnFromV1Number(exp.budgetSetLsn),
        thresholdBps: exp.thresholdBps,
        field: "limitMicroUsd",
        expected: exp.limitMicroUsd,
        logged: log.limitMicroUsd,
        eventLsn: lsnFromV1Number(log.eventLsn),
      });
    }
    // `crossedAtMs` is validated per-entry against the cost-event LSN
    // map in `validateLoggedCrossingReferences`, which also covers
    // duplicates and spurious entries that this comparator skips.
  }
  for (const [key, entries] of logged.entries()) {
    if (expected.has(key)) continue;
    const log = entries[0];
    if (log === undefined) continue; // unreachable
    out.push({
      kind: "threshold_crossing_spurious",
      budgetId: log.budgetId as BudgetId,
      budgetSetLsn: lsnFromV1Number(log.budgetSetLsn),
      thresholdBps: log.thresholdBps,
      observedMicroUsdString: log.observedMicroUsd.toString(),
      limitMicroUsd: log.limitMicroUsd as MicroUsd,
      crossedAtLsn: lsnFromV1Number(log.crossedAtLsn),
      eventLsn: lsnFromV1Number(log.eventLsn),
    });
  }
}
