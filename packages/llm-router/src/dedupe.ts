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

import type { GatewayCompletionResult, ProviderRequest } from "./types.ts";

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
   */
  lookup(req: ProviderRequest): GatewayCompletionResult | null {
    const key = hashRequest(req);
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
  store(req: ProviderRequest, result: GatewayCompletionResult): void {
    const key = hashRequest(req);
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
 * SHA-256 of the request's canonical-JSON projection. `canonicalJSON`
 * is RFC 8785 — key order is deterministic. The same logical request
 * with different key insertion orders hashes identically.
 */
export function hashRequest(req: ProviderRequest): Sha256Hex {
  const canonical = canonicalJSON({
    model: req.model,
    prompt: req.prompt,
    maxOutputTokens: req.maxOutputTokens,
  });
  return sha256Hex(canonical);
}
