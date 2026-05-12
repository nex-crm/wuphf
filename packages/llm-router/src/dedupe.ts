// Identical-payload dedupe (RFC §7.5). Within `windowMs`, two calls
// whose canonical request bytes hash to the same SHA-256 share one
// response: the second call returns the cached `GatewayCompletionResult`
// (with `dedupeReplay: true`) and does NOT consume wake/daily cap budget
// or call the provider again. The original cost_event is the one the
// ledger has on file.
//
// Why content-hash (not idempotency key): the gateway's caller surface is
// internal (agent runners), and we want dedupe to fire even if a runner
// re-issues a request with the same payload but didn't think to mint an
// idempotency key. Content-hashing the request bytes is the floor.

import { canonicalJSON, type Sha256Hex, sha256Hex } from "@wuphf/protocol";

import type { GatewayCompletionResult, ProviderRequest, SupervisorContext } from "./types.ts";

export interface DedupeConfig {
  /** Sliding window. RFC §7.5 default: 60_000 (60s). */
  readonly windowMs: number;
}

export const DEFAULT_DEDUPE_CONFIG: DedupeConfig = Object.freeze({
  windowMs: 60_000,
});

interface DedupeEntry {
  readonly result: GatewayCompletionResult;
  readonly expiresAtMs: number;
}

/**
 * In-memory dedupe cache. Keyed by SHA-256 of canonical JSON of the
 * request. The result is the original (non-replay) gateway result; we
 * flip `dedupeReplay: true` on every read so callers can log the replay.
 */
export class DedupeCache {
  private readonly entries = new Map<Sha256Hex, DedupeEntry>();
  private readonly nowMs: () => number;
  private readonly windowMs: number;

  constructor(deps: { readonly nowMs: () => number; readonly config?: DedupeConfig }) {
    this.nowMs = deps.nowMs;
    this.windowMs = deps.config?.windowMs ?? DEFAULT_DEDUPE_CONFIG.windowMs;
  }

  /**
   * Look up a cached response. Returns the cached `GatewayCompletionResult`
   * with `dedupeReplay: true`, or `null` if no live entry exists.
   * Side effect: prunes expired entries it encounters.
   *
   * The key includes `SupervisorContext` so two different agents (or two
   * tasks under one agent) with the same prompt do NOT share an LSN.
   * Without the context, replay leaks cost attribution and bypasses the
   * second caller's wake-cap accounting.
   */
  lookup(ctx: SupervisorContext, req: ProviderRequest): GatewayCompletionResult | null {
    const key = hashRequest(ctx, req);
    const entry = this.entries.get(key);
    if (entry === undefined) return null;
    const now = this.nowMs();
    if (entry.expiresAtMs <= now) {
      this.entries.delete(key);
      return null;
    }
    return { ...entry.result, dedupeReplay: true };
  }

  /**
   * Store a fresh result. Call after the gateway produces a real result
   * (with `dedupeReplay: false`). The cache stores the result as-is and
   * flips the flag on lookup.
   */
  store(ctx: SupervisorContext, req: ProviderRequest, result: GatewayCompletionResult): void {
    const key = hashRequest(ctx, req);
    this.entries.set(key, {
      result,
      expiresAtMs: this.nowMs() + this.windowMs,
    });
  }

  /**
   * Prune entries whose deadline has passed. Called by the gateway on a
   * timer or before inspection; the lookup path also self-prunes on miss.
   */
  pruneExpired(): void {
    const now = this.nowMs();
    for (const [key, entry] of this.entries) {
      if (entry.expiresAtMs <= now) {
        this.entries.delete(key);
      }
    }
  }

  size(): number {
    return this.entries.size;
  }
}

/**
 * SHA-256 of the (context, request) canonical-JSON projection.
 * `canonicalJSON` is RFC 8785 — key order is deterministic.
 *
 * The context fields (`agentSlug`, `taskId`, `receiptId`) are part of
 * the key so dedupe stays scoped to one caller. Without them the same
 * prompt from a different agent would replay the original caller's
 * `costEventLsn` — leaking cost attribution AND bypassing wake-cap
 * accounting for the second caller. See triangulation finding B3.
 *
 * `null` is used for absent optional fields so the canonical JSON shape
 * stays stable across callers; `omitUndefined` would shift key ordering
 * between presence/absence callers.
 */
export function hashRequest(ctx: SupervisorContext, req: ProviderRequest): Sha256Hex {
  const canonical = canonicalJSON({
    agentSlug: ctx.agentSlug as string,
    taskId: (ctx.taskId as string | undefined) ?? null,
    receiptId: (ctx.receiptId as string | undefined) ?? null,
    model: req.model,
    prompt: req.prompt,
    maxOutputTokens: req.maxOutputTokens,
  });
  return sha256Hex(canonical);
}
