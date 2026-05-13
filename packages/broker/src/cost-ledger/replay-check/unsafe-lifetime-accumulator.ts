// Boundary-signal helpers for the replay-check threshold-oracle's
// cumulative lifetime accumulators. The accumulators run in `bigint`
// so the cumulative integer sum stays exact across the full range —
// but a `Number(bigint)` cast at any downstream emission boundary
// would silently round past `Number.MAX_SAFE_INTEGER`. Likewise a
// downstream consumer that re-validates a `MicroUsd`-branded payload
// would reject any value past `MAX_BUDGET_LIMIT_MICRO_USD`.
//
// `flagUnsafeAccumulator` emits `unsafe_lifetime_accumulator`
// discrepancies once per `(scope, subjectId, reason)` per run when an
// accumulator first crosses each representability boundary. Two
// boundaries fire independently:
//
//   - `exceeds_micro_usd_brand` (MAX_BUDGET_LIMIT_MICRO_USD, 1e12):
//     downstream `MicroUsd`-validating consumers will reject past
//     here; oracle discrepancies emit decimal strings instead of
//     forging the brand.
//   - `exceeds_safe_integer` (Number.MAX_SAFE_INTEGER, ~9e15): any
//     number-typed derivative loses precision past here.
import { lsnFromV1Number, MAX_BUDGET_LIMIT_MICRO_USD } from "@wuphf/protocol";
import type { ReplayDiscrepancy } from "./discrepancy.ts";

// `MicroUsd` brand ceiling as a bigint. Cumulative oracle accumulators
// past this no longer fit the `MicroUsd` contract; emit a decimal
// string form in any discrepancy that carries them. Derived from the
// protocol constant so a future change to the brand bound cannot
// silently drift the oracle.
export const MAX_BUDGET_LIMIT_MICRO_USD_BIG = BigInt(MAX_BUDGET_LIMIT_MICRO_USD);

// 2^53 - 1, the largest exact integer representable as a JS `number`.
// Past this point any `Number(bigint)` cast rounds. The internal math
// is bigint and cumulative-observed wire shape is decimal string, so
// this is purely an on-call signal that any number-typed derivative
// of this accumulator is now suspect.
export const MAX_SAFE_INTEGER_BIG = BigInt(Number.MAX_SAFE_INTEGER);

export function flagUnsafeAccumulator(
  scope: "global" | "agent" | "task",
  subjectId: string | null,
  post: bigint,
  costEventLsn: number,
  flagged: Set<string>,
  out: ReplayDiscrepancy[],
): void {
  if (post > MAX_BUDGET_LIMIT_MICRO_USD_BIG) {
    pushUnsafeIfNew("exceeds_micro_usd_brand", scope, subjectId, post, costEventLsn, flagged, out);
  }
  if (post > MAX_SAFE_INTEGER_BIG) {
    pushUnsafeIfNew("exceeds_safe_integer", scope, subjectId, post, costEventLsn, flagged, out);
  }
}

function pushUnsafeIfNew(
  reason: "exceeds_micro_usd_brand" | "exceeds_safe_integer",
  scope: "global" | "agent" | "task",
  subjectId: string | null,
  post: bigint,
  costEventLsn: number,
  flagged: Set<string>,
  out: ReplayDiscrepancy[],
): void {
  const key = `${scope}|${subjectId ?? ""}|${reason}`;
  if (flagged.has(key)) return;
  flagged.add(key);
  out.push({
    kind: "unsafe_lifetime_accumulator",
    reason,
    scope,
    subjectId,
    costEventLsn: lsnFromV1Number(costEventLsn),
    accumulatedMicroUsdString: post.toString(),
  });
}
