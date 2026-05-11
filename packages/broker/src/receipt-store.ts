// In-process receipt storage interface.
//
// Branch 5 shipped an in-memory implementation only; branch 6
// (`feat/event-log-projections`) adds a durable, SQLite event-log-backed
// `SqliteReceiptStore` behind this same interface. The `list` method
// evolved in branch 6 to return a paginated `ListPage` with an opaque
// cursor — see `docs/event-log-projections-design.md` for the contract
// both implementations satisfy. Idempotency-key semantics (byte-identical
// retry returns 200 no-op) are still deferred to a later branch; the
// current `{ existed: boolean }` return is unchanged.
//
// Idempotency note: `put` is "insert if absent" — the same id with a
// different payload returns `existed:true` and the stored value is NOT
// replaced. This is a deliberate choice so a misbehaving client (or a
// retry-after-network-flake) cannot silently overwrite a previously
// stored receipt.
//
// Mutability contract: Implementations MAY store the caller-supplied
// `ReceiptSnapshot` by reference. Callers MUST NOT mutate a receipt after
// passing it to `put` and MUST NOT mutate values returned by `get`/`list`.
// The HTTP path (`packages/broker/src/receipts.ts`) is safe by construction
// because `receiptFromJson` produces a fresh frozen-args object on every
// parse; only direct programmatic callers (tests, future host code) need
// to honor this rule. `SqliteReceiptStore` sidesteps this by storing
// canonical bytes and re-parsing on read.

import type { ReceiptId, ReceiptSnapshot, ThreadId } from "@wuphf/protocol";

/**
 * Filter + pagination arguments for `ReceiptStore.list`.
 */
export interface ListFilter {
  /** Restrict to V2 receipts whose `threadId` matches. */
  readonly threadId?: ThreadId;
  /** Opaque continuation token from a prior list call's `nextCursor`. */
  readonly cursor?: string;
  /**
   * Max items in the returned page. Defaults to `DEFAULT_LIST_LIMIT`.
   * Values above `MAX_LIST_LIMIT` are silently clamped down; values
   * ≤ 0 or non-integer throw `InvalidListLimitError`.
   */
  readonly limit?: number;
}

/**
 * One page of receipts from `ReceiptStore.list`.
 */
export interface ListPage {
  readonly items: readonly ReceiptSnapshot[];
  /** `null` when no more pages. Otherwise an opaque token to pass back as `cursor`. */
  readonly nextCursor: string | null;
}

export const DEFAULT_LIST_LIMIT = 100;
export const MAX_LIST_LIMIT = 1_000;

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
   * await between); `SqliteReceiptStore` uses a `BEGIN IMMEDIATE`
   * transaction with the projection's PK as the unique constraint.
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
   * List receipts in LSN-ascending order, paginated. With `filter.threadId`,
   * returns only V2 receipts whose `threadId` matches. Cursors are opaque
   * — callers MUST NOT parse them; pass `nextCursor` from a prior page
   * back as `cursor` to fetch the next page. The wire-shape of the cursor
   * is identical across implementations so a test can mix-and-match, but
   * production code MUST treat it as opaque.
   *
   * Throws `InvalidListCursorError` for malformed cursors and
   * `InvalidListLimitError` for `limit <= 0` or non-integer limits.
   * Limits above `MAX_LIST_LIMIT` are silently clamped.
   */
  list(filter?: ListFilter): Promise<ListPage>;
  /**
   * Total count. Used by tests + the /api/health diagnostic surface.
   */
  size(): number;
}

/**
 * Error thrown by `InMemoryReceiptStore.put` when the process-wide receipt
 * cap is exceeded. Routes catch this and respond with `507 Insufficient
 * Storage`. `SqliteReceiptStore` replaces this with disk-quota-aware
 * persistence and does NOT raise `ReceiptStoreFullError`.
 */
export class ReceiptStoreFullError extends Error {
  override readonly name = "ReceiptStoreFullError";
}

/**
 * Error thrown by `list()` when `filter.cursor` is structurally invalid
 * (malformed base64 or doesn't decode to the expected `lsn:<n>` shape).
 * The HTTP route catches and responds 400.
 */
export class InvalidListCursorError extends Error {
  override readonly name = "InvalidListCursorError";
  constructor(cursor: string) {
    super(`Invalid list cursor: ${cursor}`);
  }
}

/**
 * Error thrown by `list()` when `filter.limit` is ≤ 0 or not an integer.
 * Limits above `MAX_LIST_LIMIT` are silently clamped and do NOT throw.
 */
export class InvalidListLimitError extends Error {
  override readonly name = "InvalidListLimitError";
  constructor(limit: number) {
    super(`Invalid list limit: ${limit} (must be a positive integer)`);
  }
}

/**
 * Internal helper — encode a numeric LSN into the opaque base64 cursor
 * wire shape. Exported for `SqliteReceiptStore` and the in-memory store
 * to share one implementation; callers outside the package MUST treat
 * cursors as opaque.
 *
 * @internal
 */
export function encodeListCursor(lsn: number): string {
  if (!Number.isInteger(lsn) || lsn < 0) {
    throw new Error(`encodeListCursor: lsn must be a non-negative integer, got ${lsn}`);
  }
  return Buffer.from(`lsn:${lsn}`, "utf8").toString("base64url");
}

/**
 * Internal helper — decode an opaque cursor to its LSN, or throw
 * `InvalidListCursorError` on malformed input.
 *
 * @internal
 */
export function decodeListCursor(cursor: string): number {
  let decoded: string;
  try {
    decoded = Buffer.from(cursor, "base64url").toString("utf8");
  } catch {
    throw new InvalidListCursorError(cursor);
  }
  const prefix = "lsn:";
  if (!decoded.startsWith(prefix)) {
    throw new InvalidListCursorError(cursor);
  }
  const tail = decoded.slice(prefix.length);
  // Reject empty, leading +, hex, scientific notation, etc.
  if (!/^[0-9]+$/.test(tail)) {
    throw new InvalidListCursorError(cursor);
  }
  const lsn = Number(tail);
  if (!Number.isInteger(lsn) || lsn < 0) {
    throw new InvalidListCursorError(cursor);
  }
  return lsn;
}

/**
 * Internal helper — validate + clamp `limit`. Throws on invalid values;
 * silently clamps values above `MAX_LIST_LIMIT`. Returns the resolved
 * effective limit. `undefined` resolves to `DEFAULT_LIST_LIMIT`.
 *
 * @internal
 */
export function resolveListLimit(limit: number | undefined): number {
  if (limit === undefined) {
    return DEFAULT_LIST_LIMIT;
  }
  if (!Number.isInteger(limit) || limit <= 0) {
    throw new InvalidListLimitError(limit);
  }
  return Math.min(limit, MAX_LIST_LIMIT);
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

interface InMemoryEntry {
  readonly receipt: ReceiptSnapshot;
  /**
   * Monotonic counter assigned at insertion. Mirrors the LSN that
   * `SqliteReceiptStore` derives from the event log, so cursors are
   * structurally identical across both implementations.
   */
  readonly lsn: number;
}

export class InMemoryReceiptStore implements ReceiptStore {
  // Map preserves insertion order — the `list()` contract documents that
  // ordering is non-durable but consistent within a single process lifetime.
  private readonly byId = new Map<ReceiptId, InMemoryEntry>();
  // Secondary index for `list({ threadId })`. Only V2 receipts (which carry
  // a `threadId`) are inserted; V1 receipts are absent here and therefore
  // never returned by a thread-scoped list. That's the intended semantic —
  // V1 predates threads.
  private readonly byThread = new Map<ThreadId, Set<ReceiptId>>();
  private readonly maxReceipts: number;
  private nextLsn = 1;

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
    const lsn = this.nextLsn++;
    this.byId.set(receipt.id, { receipt, lsn });
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
    return this.byId.get(id)?.receipt ?? null;
  }

  async list(filter?: ListFilter): Promise<ListPage> {
    const limit = resolveListLimit(filter?.limit);
    const afterLsn = filter?.cursor !== undefined ? decodeListCursor(filter.cursor) : 0;

    const candidateIds: readonly ReceiptId[] =
      filter?.threadId !== undefined
        ? Array.from(this.byThread.get(filter.threadId) ?? [])
        : Array.from(this.byId.keys());

    const out: ReceiptSnapshot[] = [];
    let lastLsn = 0;
    let hasMore = false;
    for (const id of candidateIds) {
      const entry = this.byId.get(id);
      // The secondary index is populated in lockstep with `byId`; a
      // missing primary entry would be a structural bug. Defensive guard.
      if (entry === undefined) continue;
      if (entry.lsn <= afterLsn) continue;
      if (out.length >= limit) {
        hasMore = true;
        break;
      }
      out.push(entry.receipt);
      lastLsn = entry.lsn;
    }

    return {
      items: out,
      nextCursor: hasMore ? encodeListCursor(lastLsn) : null,
    };
  }

  size(): number {
    return this.byId.size;
  }
}
