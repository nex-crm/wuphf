/**
 * Agent event timer — pure pill-state derivation + a single 1Hz scheduler.
 *
 * Eng decision C2: one shared `setInterval(1000)` walks all snapshots; rows
 * subscribe to a derived slice via shallow-equality selectors. Per-agent
 * timers are not used.
 *
 * The 1Hz scheduler MUST clean up on unmount or it will keep ticking after
 * AgentList is gone (dev hot-reload, route changes, multi-tab) — flagged as
 * the critical regression risk in the eng review test plan.
 */

const HALO_DECAY_MS = 600;
const ROUTINE_HOLD_MS = 60_000;
const MILESTONE_HOLD_MS = 120_000;
// Duration of the dim phase that follows the hold window, NOT an absolute
// cutoff from the event. Routine: 60s hold + 60s dim = idle at 120s.
// Milestone: 120s hold + 60s dim = idle at 180s. Previously equal to
// MILESTONE_HOLD_MS, which made "dim" unreachable for milestone events.
const DIM_WINDOW_MS = 60_000;
const TICK_INTERVAL_MS = 1000;

export type PillState = "halo" | "holding" | "dim" | "idle" | "stuck";

export type EventKind = "routine" | "milestone" | "stuck" | undefined;

export interface PillStateInput {
  /** Wall-clock timestamp (ms) of the most recent event for this agent. */
  lastEventMs: number;
  /** Wall-clock now (ms), injected so tests stay deterministic. */
  nowMs: number;
  /** Kind of the most recent event. `stuck` overrides everything. */
  kind: EventKind;
  /**
   * Wall-clock cutoff (ms) for the halo glow decay. Typically ~600ms after
   * the event arrives. When undefined or in the past, no halo is rendered.
   */
  haloUntilMs?: number;
}

/**
 * Pure derivation: given a snapshot and "now", return the pill state.
 *
 * Rules (in priority order):
 *   1. `kind === "stuck"` -> "stuck" (overrides everything).
 *   2. `nowMs < haloUntilMs` -> "halo" (recent event glow).
 *   3. Within hold window -> "holding". Routine = 60s, milestone = 120s.
 *      `kind === undefined` (or any future unrecognised kind) is treated
 *      as "routine" so we never crash on a snapshot that pre-dates the
 *      classifier.
 *   4. Within dim phase that follows hold (next 60s) -> "dim".
 *   5. Otherwise -> "idle".
 */
export function computePillState(input: PillStateInput): PillState {
  const { lastEventMs, nowMs, kind, haloUntilMs } = input;

  if (kind === "stuck") {
    return "stuck";
  }

  if (typeof haloUntilMs === "number" && nowMs < haloUntilMs) {
    return "halo";
  }

  const sinceEvent = nowMs - lastEventMs;
  const holdMs = kind === "milestone" ? MILESTONE_HOLD_MS : ROUTINE_HOLD_MS;

  if (sinceEvent <= holdMs) {
    return "holding";
  }
  if (sinceEvent <= holdMs + DIM_WINDOW_MS) {
    return "dim";
  }
  return "idle";
}

export type SchedulerCallback = (nowMs: number) => void;

/**
 * Start the single shared 1Hz tick. Calls `callback(Date.now())` every
 * ~1000ms. Returns a cleanup function that MUST be called on unmount —
 * without it the interval leaks and keeps re-rendering unmounted trees.
 */
export function startEventTimer(callback: SchedulerCallback): () => void {
  const handle = setInterval(() => {
    callback(Date.now());
  }, TICK_INTERVAL_MS);

  return () => {
    clearInterval(handle);
  };
}

// Internal constants exported for tests / docs only.
export const __internal = {
  HALO_DECAY_MS,
  ROUTINE_HOLD_MS,
  MILESTONE_HOLD_MS,
  DIM_WINDOW_MS,
  TICK_INTERVAL_MS,
} as const;
