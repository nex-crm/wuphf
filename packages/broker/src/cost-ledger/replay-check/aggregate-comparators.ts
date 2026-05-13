// Comparators for the agent-day and per-task aggregate totals.
//
// `cost_by_agent.total_micro_usd` and `cost_by_task.total_micro_usd`
// accumulate over the lifetime of the ledger and can exceed both the
// `MicroUsd` brand ceiling (`MAX_BUDGET_LIMIT_MICRO_USD`, 1e12) and
// `Number.MAX_SAFE_INTEGER` (~9e15). Both sides flow as `bigint` and
// the discrepancy wire shape is a decimal string, so the diagnostic
// preserves exact bytes regardless of magnitude.
import type { ReplayDiscrepancy } from "./discrepancy.ts";

// Inverse of the orchestrator's `agentDayKey` encoding. Only the
// aggregate comparators round-trip from key back to its components,
// so the helper lives here next to its single call site.
function splitAgentDayKey(key: string): {
  readonly agentSlug: string;
  readonly dayUtc: string;
} {
  const idx = key.indexOf("|");
  if (idx === -1) {
    throw new Error(`replay-check: malformed agent-day key ${key}`);
  }
  return { agentSlug: key.slice(0, idx), dayUtc: key.slice(idx + 1) };
}

export function compareAgentDays(
  replayed: ReadonlyMap<string, bigint>,
  stored: ReadonlyMap<string, bigint>,
  out: ReplayDiscrepancy[],
): void {
  for (const [key, replayedTotal] of replayed.entries()) {
    const storedTotal = stored.get(key);
    const { agentSlug, dayUtc } = splitAgentDayKey(key);
    if (storedTotal === undefined) {
      out.push({
        kind: "agent_day_row_missing",
        agentSlug,
        dayUtc,
        replayedMicroUsdString: replayedTotal.toString(),
      });
      continue;
    }
    if (storedTotal !== replayedTotal) {
      out.push({
        kind: "agent_day_total_mismatch",
        agentSlug,
        dayUtc,
        replayedMicroUsdString: replayedTotal.toString(),
        storedMicroUsdString: storedTotal.toString(),
      });
    }
  }
  for (const [key, storedTotal] of stored.entries()) {
    if (!replayed.has(key)) {
      const { agentSlug, dayUtc } = splitAgentDayKey(key);
      out.push({
        kind: "agent_day_row_ghost",
        agentSlug,
        dayUtc,
        storedMicroUsdString: storedTotal.toString(),
      });
    }
  }
}

export function compareTasks(
  replayed: ReadonlyMap<string, bigint>,
  stored: ReadonlyMap<string, bigint>,
  out: ReplayDiscrepancy[],
): void {
  for (const [taskId, replayedTotal] of replayed.entries()) {
    const storedTotal = stored.get(taskId);
    if (storedTotal === undefined) {
      out.push({
        kind: "task_row_missing",
        taskId,
        replayedMicroUsdString: replayedTotal.toString(),
      });
      continue;
    }
    if (storedTotal !== replayedTotal) {
      out.push({
        kind: "task_total_mismatch",
        taskId,
        replayedMicroUsdString: replayedTotal.toString(),
        storedMicroUsdString: storedTotal.toString(),
      });
    }
  }
  for (const [taskId, storedTotal] of stored.entries()) {
    if (!replayed.has(taskId)) {
      out.push({
        kind: "task_row_ghost",
        taskId,
        storedMicroUsdString: storedTotal.toString(),
      });
    }
  }
}
