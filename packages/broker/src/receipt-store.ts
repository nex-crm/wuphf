// In-process receipt storage interface.
//
// Branch 5 ships an in-memory implementation only — the wire path is the
// load-bearing concern here. Branch 6 (`feat/event-log-projections`) will
// add a durable event-log impl. The store interface and the route's
// handling of conflicts will both evolve in that branch — the current
// `{ existed: boolean }` return cannot express the "byte-identical retry
// returns 200 no-op vs. different payload returns 409" semantics branch 6
// needs. Treat the interface here as branch-5-stable, NOT as the final
// shape for branch-6 idempotency-key semantics.
//
// Idempotency note (branch 5): `put` is "insert if absent" — the same id
// with a different payload returns `existed:true` and the stored value is
// NOT replaced. This is a deliberate choice so a misbehaving client (or a
// retry-after-network-flake) cannot silently overwrite a previously
// stored receipt.
//
// Mutability contract: Implementations MAY store the caller-supplied
// `ReceiptSnapshot` by reference. Callers MUST NOT mutate a receipt after
// passing it to `put` and MUST NOT mutate values returned by `get`/`list`.
// The HTTP path (`packages/broker/src/receipts.ts`) is safe by construction
// because `receiptFromJson` produces a fresh frozen-args object on every
// parse; only direct programmatic callers (tests, future host code) need
// to honor this rule. Durable backends in branch 6 will sidestep this by
// storing canonical bytes and re-parsing on read.

import type { ReceiptId, ReceiptSnapshot, ThreadId } from "@wuphf/protocol";

export interface ReceiptStore {
  /**
   * Insert a receipt. Returns `existed: true` if a receipt with this id
   * is already present; the existing value is NOT overwritten.
   *
   * Atomicity: implementations MUST be atomic with respect to the `id`.
   * Under concurrent calls with the same `id`, exactly one returns
   * `{ existed: false }` and any subsequent caller observes
   * `{ existed: true }`. `InMemoryReceiptStore` satisfies this via Node's
   * single-threaded event loop (the `has`/`set` pair runs without an
   * await between); durable backends MUST use a unique-constraint
   * INSERT, `ON CONFLICT DO NOTHING` + affected-rows check, or an
   * equivalent serializable primitive.
   *
   * Read-your-write: once `put` resolves with `{ existed: false }`, an
   * immediate `get(receipt.id)` MUST return the inserted receipt, and an
   * immediate `list({ threadId: receipt.threadId })` (when `schemaVersion
   * === 2` and a threadId is set) MUST include it. Eventually-consistent
   * projection backends are NOT acceptable behind this interface; the
   * HTTP `201 Location:` contract promises an immediately fetchable
   * receipt, and clients race the 201 response against follow-up reads.
   */
  put(receipt: ReceiptSnapshot): Promise<{ readonly existed: boolean }>;
  /**
   * Read by id. Returns null when not found.
   */
  get(id: ReceiptId): Promise<ReceiptSnapshot | null>;
  /**
   * List receipts. With `filter.threadId`, returns only V2 receipts whose
   * `threadId` matches. Without a filter, returns every receipt in
   * insertion order. Branch 6 will replace ordering with event-log order;
   * callers MUST NOT rely on cross-restart stability of order today.
   */
  list(filter?: { readonly threadId?: ThreadId }): Promise<readonly ReceiptSnapshot[]>;
  /**
   * Total count. Used by tests + the /api/health diagnostic surface.
   */
  size(): number;
}

/**
 * Error thrown by `InMemoryReceiptStore.put` when the process-wide receipt
 * cap is exceeded. Routes catch this and respond with `507 Insufficient
 * Storage`. Branch 6's durable backend replaces this with quota-aware
 * persistence and removes the in-process ceiling.
 */
export class ReceiptStoreFullError extends Error {
  override readonly name = "ReceiptStoreFullError";
}

// Default ceiling for the in-memory store. Sized to comfortably exceed a
// session's normal receipt volume (a typical agent-run produces O(1)
// receipts per task) while preventing a bearer-holding adversary from
// exhausting process RAM with bottomless POST traffic. Hosts can override
// via `createBroker({ receiptStore: new InMemoryReceiptStore({ maxReceipts }) })`.
const DEFAULT_MAX_RECEIPTS = 10_000;

export interface InMemoryReceiptStoreConfig {
  readonly maxReceipts?: number;
}

export class InMemoryReceiptStore implements ReceiptStore {
  // Map preserves insertion order — the `list()` contract documents that
  // ordering is non-durable but consistent within a single process lifetime.
  private readonly byId = new Map<ReceiptId, ReceiptSnapshot>();
  // Secondary index for `list({ threadId })`. Only V2 receipts (which carry
  // a `threadId`) are inserted; V1 receipts are absent here and therefore
  // never returned by a thread-scoped list. That's the intended semantic —
  // V1 predates threads.
  private readonly byThread = new Map<ThreadId, Set<ReceiptId>>();
  private readonly maxReceipts: number;

  constructor(config: InMemoryReceiptStoreConfig = {}) {
    const requested = config.maxReceipts ?? DEFAULT_MAX_RECEIPTS;
    if (!Number.isInteger(requested) || requested <= 0) {
      throw new Error(
        `InMemoryReceiptStore: maxReceipts must be a positive integer, got ${requested}`,
      );
    }
    this.maxReceipts = requested;
  }

  async put(receipt: ReceiptSnapshot): Promise<{ readonly existed: boolean }> {
    if (this.byId.has(receipt.id)) {
      return { existed: true };
    }
    // Cap check runs AFTER the `has` check so a duplicate POST against a
    // store at capacity still returns 409 (the correct semantic) rather
    // than 507 (which would falsely imply the receipt couldn't be stored).
    if (this.byId.size >= this.maxReceipts) {
      throw new ReceiptStoreFullError(`InMemoryReceiptStore at capacity (${this.maxReceipts})`);
    }
    this.byId.set(receipt.id, receipt);
    if (receipt.schemaVersion === 2 && receipt.threadId !== undefined) {
      const existing = this.byThread.get(receipt.threadId);
      if (existing === undefined) {
        this.byThread.set(receipt.threadId, new Set([receipt.id]));
      } else {
        existing.add(receipt.id);
      }
    }
    return { existed: false };
  }

  async get(id: ReceiptId): Promise<ReceiptSnapshot | null> {
    return this.byId.get(id) ?? null;
  }

  async list(filter?: { readonly threadId?: ThreadId }): Promise<readonly ReceiptSnapshot[]> {
    if (filter?.threadId !== undefined) {
      const ids = this.byThread.get(filter.threadId);
      if (ids === undefined) return [];
      const out: ReceiptSnapshot[] = [];
      for (const id of ids) {
        const r = this.byId.get(id);
        // The secondary index is populated in lockstep with `byId`, so a
        // missing primary entry would be a structural bug. Defensive guard.
        if (r !== undefined) out.push(r);
      }
      return out;
    }
    return Array.from(this.byId.values());
  }

  size(): number {
    return this.byId.size;
  }
}
