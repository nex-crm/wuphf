// Cap enforcement: per-office daily cap (from the ledger), per-agent wake
// cap (in-memory sliding window), per-agent circuit breaker (in-memory),
// process-wide idle mode (in-memory).
//
// Daily cap reads `cost_by_agent` because the projection is the source of
// truth and survives broker restarts. Wake cap, breaker, and idle mode are
// process-local on purpose — they reset on restart (a crash that loses one
// minute of wake-cap state is harmless; reading from disk every call would
// be costly).
//
// Idle mode: any call goes through `preflightIdle`. Human activity calls
// `noteHumanActivity` to reset the clock. Agent-initiated activity does NOT
// reset the clock; otherwise an agent loop would keep itself awake.

import type { CostLedger } from "@wuphf/broker/cost-ledger";

import { CapExceededError, CircuitBreakerOpenError, IdleModeError } from "./errors.ts";
import type { AgentInspection, BreakerState, GatewayInspection } from "./types.ts";

export interface CapsConfig {
  /** Per-office daily cap in micro-USD. RFC §8 default: 5_000_000 ($5/day). */
  readonly dailyMicroUsd: number;
  /** Per-agent wake cap. RFC §8 default: 12/hr. */
  readonly wakeCapPerHour: number;
  /** Sliding-window size for wake cap. Default 60 * 60 * 1000 (1h). */
  readonly wakeWindowMs: number;
  /** Errors-in-window threshold for breaker. RFC §8 default: 2. */
  readonly breakerErrorThreshold: number;
  /** Breaker error window. RFC §8 default: 10 * 60 * 1000 (10min). */
  readonly breakerWindowMs: number;
  /** Cool-down once breaker opens. Default 5 * 60 * 1000 (5min). */
  readonly breakerCooldownMs: number;
  /** Idle threshold. RFC §8 default: 5 * 60 * 1000 (5min). */
  readonly idleThresholdMs: number;
}

export const DEFAULT_CAPS_CONFIG: CapsConfig = Object.freeze({
  dailyMicroUsd: 5_000_000,
  wakeCapPerHour: 12,
  wakeWindowMs: 60 * 60 * 1000,
  breakerErrorThreshold: 2,
  breakerWindowMs: 10 * 60 * 1000,
  breakerCooldownMs: 5 * 60 * 1000,
  idleThresholdMs: 5 * 60 * 1000,
});

export interface CapsDeps {
  readonly ledger: CostLedger;
  readonly config: CapsConfig;
  readonly nowMs: () => number;
}

interface AgentRuntimeState {
  /** Wake timestamps (ms). Pruned on each cap check. */
  wakes: number[];
  /** Error timestamps (ms). Pruned on each cap check. */
  errorTimestamps: number[];
  /** Current breaker state. */
  breaker: BreakerState;
}

/**
 * In-flight reservation handle. Returned by `reserveAndPreflight`; must
 * be either committed (success) or released (failure) so the reservation
 * does not leak. Reservations count toward subsequent preflight checks
 * until cleared so concurrent calls can't all pass a stale cap check and
 * then all bill the provider.
 */
export interface Reservation {
  readonly id: number;
  readonly agentSlug: string;
  readonly estimatedMicroUsd: number;
}

export class Caps {
  private readonly ledger: CostLedger;
  private readonly config: CapsConfig;
  private readonly nowMs: () => number;
  private readonly agents = new Map<string, AgentRuntimeState>();
  /**
   * Active reservations, keyed by reservation id. Counted toward both the
   * daily-cap and wake-cap preflight checks so two concurrent calls
   * can't both pass a stale snapshot of `cost_by_agent`/`wakes[]` and
   * then both bill the provider.
   */
  private readonly reservations = new Map<number, Reservation>();
  private nextReservationId = 1;
  /**
   * Last "activity" timestamp for the idle gate. Initialised to construction
   * time so the gateway starts active; `noteHumanActivity` resets it on
   * every inbound human-driven event.
   */
  private lastActivityMs: number;

  constructor(deps: CapsDeps) {
    this.ledger = deps.ledger;
    this.config = deps.config;
    this.nowMs = deps.nowMs;
    this.lastActivityMs = deps.nowMs();
  }

  noteHumanActivity(): void {
    this.lastActivityMs = this.nowMs();
  }

  /**
   * Throw `IdleModeError` if the broker has been idle past the threshold.
   * Idle does NOT account for agent-initiated activity by design.
   */
  preflightIdle(): void {
    const now = this.nowMs();
    if (now - this.lastActivityMs > this.config.idleThresholdMs) {
      throw new IdleModeError(this.lastActivityMs);
    }
  }

  /**
   * Reject if today's office spend already meets or exceeds the cap. We
   * do NOT pre-charge for the call we're about to make; the ±$0.05
   * tolerance in §10.4 covers one in-flight call going over.
   */
  preflightDailyCap(): void {
    const today = isoDateUtc(new Date(this.nowMs()));
    const total = this.officeSpendMicroUsd(today);
    if (total >= this.config.dailyMicroUsd) {
      const retryAfterMs = msUntilNextUtcMidnight(this.nowMs());
      throw new CapExceededError("daily", total, this.config.dailyMicroUsd, retryAfterMs);
    }
  }

  preflightWakeCap(agentSlug: string): void {
    const now = this.nowMs();
    const state = this.stateFor(agentSlug);
    pruneTimestamps(state.wakes, now - this.config.wakeWindowMs);
    if (state.wakes.length >= this.config.wakeCapPerHour) {
      const oldest = state.wakes[0] ?? now;
      const retryAfterMs = Math.max(0, oldest + this.config.wakeWindowMs - now);
      throw new CapExceededError(
        "wake",
        state.wakes.length,
        this.config.wakeCapPerHour,
        retryAfterMs,
      );
    }
  }

  /**
   * Record a wake. Call AFTER pre-flight succeeds so a rejected call
   * doesn't consume a slot in the window.
   */
  recordWake(agentSlug: string): void {
    this.stateFor(agentSlug).wakes.push(this.nowMs());
  }

  /**
   * Atomically check daily cap + wake cap and reserve a slot. The
   * `estimatedMicroUsd` is a pessimistic upper bound on what the call
   * might cost; once the ledger writes the actual cost, the caller
   * `commitReservation` (or `releaseReservation` on failure) to clear
   * the reservation.
   *
   * The reservation is counted toward subsequent preflight checks so a
   * burst of concurrent `Gateway.complete()` calls cannot all see the
   * same pre-call ledger snapshot and all pass. This closes the cost-
   * ceiling bypass surfaced by the PR #834 round-1 review (adversarial
   * BLOCK + security HIGH).
   */
  reserveAndPreflight(agentSlug: string, estimatedMicroUsd: number): Reservation {
    if (
      typeof estimatedMicroUsd !== "number" ||
      !Number.isSafeInteger(estimatedMicroUsd) ||
      estimatedMicroUsd < 0
    ) {
      throw new Error(
        `reserveAndPreflight: estimatedMicroUsd must be a non-negative safe integer, got ${String(estimatedMicroUsd)}`,
      );
    }
    const today = isoDateUtc(new Date(this.nowMs()));
    const ledgerTotal = this.officeSpendMicroUsd(today);
    const pendingTotal = this.totalPendingReservationsMicroUsd();
    if (ledgerTotal + pendingTotal + estimatedMicroUsd > this.config.dailyMicroUsd) {
      const retryAfterMs = msUntilNextUtcMidnight(this.nowMs());
      throw new CapExceededError(
        "daily",
        ledgerTotal + pendingTotal,
        this.config.dailyMicroUsd,
        retryAfterMs,
      );
    }
    const now = this.nowMs();
    const state = this.stateFor(agentSlug);
    pruneTimestamps(state.wakes, now - this.config.wakeWindowMs);
    const pendingWakesForAgent = this.pendingWakesFor(agentSlug);
    if (state.wakes.length + pendingWakesForAgent >= this.config.wakeCapPerHour) {
      const oldest = state.wakes[0] ?? now;
      const retryAfterMs = Math.max(0, oldest + this.config.wakeWindowMs - now);
      throw new CapExceededError(
        "wake",
        state.wakes.length + pendingWakesForAgent,
        this.config.wakeCapPerHour,
        retryAfterMs,
      );
    }
    const reservation: Reservation = {
      id: this.nextReservationId++,
      agentSlug,
      estimatedMicroUsd,
    };
    this.reservations.set(reservation.id, reservation);
    return reservation;
  }

  /**
   * Success path: drop the reservation and record the wake. The actual
   * spend lives in the ledger; cost_by_agent will reflect it.
   */
  commitReservation(reservation: Reservation): void {
    if (this.reservations.delete(reservation.id)) {
      this.recordWake(reservation.agentSlug);
    }
  }

  /**
   * Failure path: drop the reservation without recording a wake. The
   * caller is expected to also call `recordError` for breaker-eligible
   * failures; bad-input failures release the reservation but skip the
   * breaker per existing policy.
   */
  releaseReservation(reservation: Reservation): void {
    this.reservations.delete(reservation.id);
  }

  preflightBreaker(agentSlug: string): void {
    const state = this.stateFor(agentSlug);
    if (state.breaker.status === "open") {
      const now = this.nowMs();
      if (now < state.breaker.cooldownEndsMs) {
        throw new CircuitBreakerOpenError(state.breaker.cooldownEndsMs);
      }
      // Cool-down elapsed — half-open: clear errors and let the next call
      // try. A successful call closes; another error re-opens.
      state.breaker = { status: "closed", recentErrors: 0 };
      state.errorTimestamps = [];
    }
  }

  recordSuccess(agentSlug: string): void {
    const state = this.stateFor(agentSlug);
    state.errorTimestamps = [];
    state.breaker = { status: "closed", recentErrors: 0 };
  }

  recordError(agentSlug: string): void {
    const now = this.nowMs();
    const state = this.stateFor(agentSlug);
    pruneTimestamps(state.errorTimestamps, now - this.config.breakerWindowMs);
    state.errorTimestamps.push(now);
    if (state.errorTimestamps.length >= this.config.breakerErrorThreshold) {
      state.breaker = {
        status: "open",
        openedAtMs: now,
        cooldownEndsMs: now + this.config.breakerCooldownMs,
      };
    } else {
      state.breaker = { status: "closed", recentErrors: state.errorTimestamps.length };
    }
  }

  inspect(): GatewayInspection {
    const now = this.nowMs();
    const perAgent = new Map<string, AgentInspection>();
    for (const [slug, state] of this.agents) {
      pruneTimestamps(state.wakes, now - this.config.wakeWindowMs);
      perAgent.set(slug, {
        recentWakeCount: state.wakes.length,
        breaker: state.breaker,
      });
    }
    const idleFor = now - this.lastActivityMs;
    return {
      idleSinceMs: this.lastActivityMs,
      idle: idleFor > this.config.idleThresholdMs,
      perAgent,
    };
  }

  /**
   * Sum `cost_by_agent.total_micro_usd` across every agent for the given
   * UTC day. The cap is per-office, not per-agent; this is the office.
   */
  private officeSpendMicroUsd(dayUtc: string): number {
    let total = 0;
    for (const row of this.ledger.listAgentSpend({ dayUtc })) {
      total += row.totalMicroUsd as number;
    }
    return total;
  }

  private totalPendingReservationsMicroUsd(): number {
    let total = 0;
    for (const r of this.reservations.values()) total += r.estimatedMicroUsd;
    return total;
  }

  private pendingWakesFor(agentSlug: string): number {
    let n = 0;
    for (const r of this.reservations.values()) {
      if (r.agentSlug === agentSlug) n += 1;
    }
    return n;
  }

  private stateFor(agentSlug: string): AgentRuntimeState {
    let state = this.agents.get(agentSlug);
    if (state === undefined) {
      state = {
        wakes: [],
        errorTimestamps: [],
        breaker: { status: "closed", recentErrors: 0 },
      };
      this.agents.set(agentSlug, state);
    }
    return state;
  }
}

function pruneTimestamps(buf: number[], cutoffMs: number): void {
  while (buf.length > 0 && (buf[0] ?? Number.POSITIVE_INFINITY) < cutoffMs) {
    buf.shift();
  }
}

function isoDateUtc(d: Date): string {
  return d.toISOString().slice(0, 10);
}

function msUntilNextUtcMidnight(nowMs: number): number {
  const d = new Date(nowMs);
  const next = Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), d.getUTCDate() + 1, 0, 0, 0, 0);
  return Math.max(0, next - nowMs);
}
