// In-process receipt storage interface.
//
// Branch 5 ships an in-memory implementation only — the wire path is the
// load-bearing concern here. Branch 6 (`feat/event-log-projections`) will
// add a durable event-log impl behind the same interface and route
// `createBroker` to it instead, without touching the listener / routes /
// codec layers.
//
// Idempotency note: `put` is "insert if absent" — the same id with a
// different payload returns `existed:true` and the stored value is NOT
// replaced. This is a deliberate choice so a misbehaving client (or a
// retry-after-network-flake) cannot silently overwrite a previously
// stored receipt. Branch 6 will introduce idempotency-key semantics
// where the SAME byte-identical receipt re-posted at the same id is a
// 200 (no-op) and a DIFFERENT receipt at the same id is a 409. Both
// modes flow through this same interface; the impl decides.

import type { ReceiptId, ReceiptSnapshot, ThreadId } from "@wuphf/protocol";

export interface ReceiptStore {
  /**
   * Insert a receipt. Returns `existed: true` if a receipt with this id
   * is already present; the existing value is NOT overwritten.
   *
   * Implementations MUST be atomic with respect to the `id`: under
   * concurrent calls with the same `id`, exactly one returns
   * `{ existed: false }` and any subsequent caller observes
   * `{ existed: true }`. `InMemoryReceiptStore` satisfies this via
   * Node's single-threaded event loop (the `has`/`set` pair runs
   * without an await between); durable backends MUST use a unique-
   * constraint INSERT, `ON CONFLICT DO NOTHING` + affected-rows check,
   * or an equivalent serializable primitive.
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

export class InMemoryReceiptStore implements ReceiptStore {
  // Map preserves insertion order — the `list()` contract documents that
  // ordering is non-durable but consistent within a single process lifetime.
  private readonly byId = new Map<ReceiptId, ReceiptSnapshot>();
  // Secondary index for `list({ threadId })`. Only V2 receipts (which carry
  // a `threadId`) are inserted; V1 receipts are absent here and therefore
  // never returned by a thread-scoped list. That's the intended semantic —
  // V1 predates threads.
  private readonly byThread = new Map<ThreadId, Set<ReceiptId>>();

  async put(receipt: ReceiptSnapshot): Promise<{ readonly existed: boolean }> {
    if (this.byId.has(receipt.id)) {
      return { existed: true };
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
