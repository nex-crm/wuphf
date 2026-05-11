# Event log + projections — design contract (branch 6)

**Branch**: `feat/event-log-projections`
**Status**: implementation contract — both parallel workers MUST agree on the shapes here.
**Last updated**: 2026-05-11

## Goal

Replace the in-memory `ReceiptStore` with a durable, event-log-backed implementation while preserving the branch-5 wire and interface contracts. Add cursor pagination to the thread-list endpoint so the 1000-item truncation can go away.

## Non-goals (deferred)

- Idempotency keys (same id + byte-identical payload → 200 no-op). Branch 6 still returns 409 on id collision regardless of payload identity.
- Hash-chained audit (per-install signed Merkle root) — separate downstream branch.
- Multi-process writers. SQLite handle is owned by the broker process; renderer never opens the DB.

## Files (final layout — both workers respect this)

```
packages/broker/
├── package.json                              # +better-sqlite3 dep (Worker A)
├── src/
│   ├── event-log/                            # Worker A
│   │   ├── index.ts                          # re-exports
│   │   ├── event-log.ts                      # append, readFromLsn, openDatabase
│   │   ├── migrations.ts                     # forward-only migration runner
│   │   └── 001_initial.sql                   # schema
│   ├── sqlite-receipt-store.ts               # Worker A — implements ReceiptStore
│   ├── receipt-store.ts                      # Worker B extends interface (list signature change)
│   ├── receipts.ts                           # Worker B — cursor handling on /api/threads/:tid/receipts
│   ├── listener.ts                           # untouched (route shape unchanged)
│   ├── index.ts                              # Worker A adds SqliteReceiptStore export
│   └── ...
├── tests/
│   ├── event-log.spec.ts                     # Worker A
│   ├── sqlite-receipt-store.spec.ts          # Worker A
│   ├── receipt-store-parity.spec.ts          # Worker A — runs the same suite against both stores
│   └── receipts.spec.ts                      # Worker B — adds cursor pagination tests
├── docs/
│   └── event-log-projections-design.md       # this file
└── README.md                                 # Worker B updates route table
```

**Hard rule**: Worker A and Worker B do NOT overlap on file changes except for:
- `packages/broker/src/receipt-store.ts` — Worker B owns the interface change (new `list` signature). Worker A consumes it.
- `packages/broker/src/index.ts` — Worker A adds the `SqliteReceiptStore` export. Worker B does not touch.

If a file is in both columns, escalate to the integration step — do not race the edit.

## Event log schema (SQLite, `STRICT` mode, WAL)

```sql
-- One forward-only migration: 001_initial.sql

CREATE TABLE event_log (
  lsn        INTEGER PRIMARY KEY AUTOINCREMENT,    -- ordered append-only sequence
  ts_ms      INTEGER NOT NULL,                     -- ms since epoch at append time
  type       TEXT NOT NULL,                        -- 'receipt.put' for branch 6
  payload    BLOB NOT NULL                         -- canonical JSON bytes (UTF-8)
) STRICT;

CREATE TABLE receipts_projection (
  receipt_id      TEXT PRIMARY KEY,                -- ReceiptId branded string
  thread_id       TEXT,                            -- ThreadId branded string, NULL for V1 receipts
  schema_version  INTEGER NOT NULL,                -- 1 or 2
  lsn             INTEGER NOT NULL UNIQUE,         -- pointer into event_log
  payload         BLOB NOT NULL,                   -- duplicate of event_log.payload for fast reads
  FOREIGN KEY (lsn) REFERENCES event_log(lsn) ON DELETE RESTRICT
) STRICT;

CREATE INDEX receipts_projection_thread_lsn
  ON receipts_projection(thread_id, lsn)
  WHERE thread_id IS NOT NULL;

-- Schema version pin (used by migration runner)
PRAGMA user_version = 1;
```

### PRAGMAs at open time

```
PRAGMA journal_mode = WAL;            -- concurrent reader during writes
PRAGMA synchronous = NORMAL;          -- WAL-safe durability without fsync-per-commit
PRAGMA foreign_keys = ON;             -- enforce projection→event_log integrity
PRAGMA busy_timeout = 5000;           -- 5s wait on SQLITE_BUSY
```

Bench guard: the broker is the only writer. If multi-writer ever shows up, switch synchronous to FULL and re-bench.

## Public TypeScript API

### `event-log/event-log.ts`

```ts
import type Database from "better-sqlite3";

export type EventType = "receipt.put";   // branch-6 only event; expand later

export interface EventLogRecord {
  readonly lsn: number;
  readonly tsMs: number;
  readonly type: EventType;
  readonly payload: Buffer;              // canonical JSON bytes
}

export interface AppendArgs {
  readonly type: EventType;
  readonly payload: Buffer;
}

export interface EventLog {
  /**
   * Append-only. Returns the assigned LSN. Synchronous — better-sqlite3
   * does not expose an async API and the broker is the sole writer.
   *
   * MUST be invoked inside a containing transaction when the caller is
   * also writing to a projection table (see SqliteReceiptStore.put).
   */
  append(args: AppendArgs): number;

  /**
   * Read events with lsn > `fromLsn`, in LSN order, up to `limit` rows.
   * Used by replay-from-LSN bootstrap.
   */
  readFromLsn(fromLsn: number, limit: number): readonly EventLogRecord[];

  /**
   * Highest assigned LSN, or 0 if the log is empty. Used by tests + diagnostics.
   */
  highestLsn(): number;
}

export interface OpenDatabaseArgs {
  /** Absolute path. ":memory:" is accepted for tests. */
  readonly path: string;
}

export function openDatabase(args: OpenDatabaseArgs): Database.Database;
export function createEventLog(db: Database.Database): EventLog;
```

### `event-log/migrations.ts`

```ts
import type Database from "better-sqlite3";

/**
 * Apply all forward-only migrations whose number > current `PRAGMA user_version`.
 * Runs each migration in its own transaction; sets `user_version` on success.
 * Throws on first failure — caller MUST treat the DB as uninitialized.
 */
export function runMigrations(db: Database.Database): void;

export const CURRENT_SCHEMA_VERSION: number;  // = 1
```

### `sqlite-receipt-store.ts`

```ts
import type Database from "better-sqlite3";
import type { ReceiptSnapshot } from "@wuphf/protocol";
import type { ReceiptStore } from "./receipt-store.ts";

export interface SqliteReceiptStoreConfig {
  /** Absolute path or ":memory:". */
  readonly path: string;
}

export class SqliteReceiptStore implements ReceiptStore {
  static open(config: SqliteReceiptStoreConfig): SqliteReceiptStore;
  // (constructor takes a db; static `open` runs migrations + wires the event log)
  put(receipt: ReceiptSnapshot): Promise<{ readonly existed: boolean }>;
  get(id: ReceiptId): Promise<ReceiptSnapshot | null>;
  list(filter?: ListFilter): Promise<ListPage>;     // see below — interface evolves
  size(): number;
  close(): void;                                    // releases the SQLite handle
}
```

### Interface evolution: `ReceiptStore.list` (owned by Worker B)

The branch-5 signature is:

```ts
list(filter?: { readonly threadId?: ThreadId }): Promise<readonly ReceiptSnapshot[]>;
```

Branch-6 signature:

```ts
export interface ListFilter {
  readonly threadId?: ThreadId;
  /** Opaque continuation token returned from a prior list call. */
  readonly cursor?: string;
  /** Default 100, max 1000. Implementations MUST clamp out-of-range values. */
  readonly limit?: number;
}

export interface ListPage {
  readonly items: readonly ReceiptSnapshot[];
  /** `null` when no more pages; opaque otherwise. */
  readonly nextCursor: string | null;
}

list(filter?: ListFilter): Promise<ListPage>;
```

**Cursor opacity**: cursors are base64-encoded `lsn:<n>` strings. The shape is an implementation detail — callers MUST treat them as opaque tokens. Both stores produce cursors with the same format so tests can mix-and-match, but no production code parses them.

**Ordering**: LSN ascending. For the in-memory store, LSN is replaced by insertion order (1-indexed). Same `${type}:${value}` shape.

## HTTP wire change — `GET /api/threads/:tid/receipts` (owned by Worker B)

### Query parameters

| Param | Type | Default | Notes |
|---|---|---|---|
| `cursor` | string | — | Opaque base64 token from prior response's `Link: rel="next"`. |
| `limit` | integer | 100 | Clamped to [1, 1000]. Invalid → 400. |

### Response

- **Body unchanged**: same bare JSON array of receipts, in LSN order.
- **`Link` header added** when more pages exist:

  ```
  Link: </api/threads/<tid>/receipts?cursor=<base64>&limit=<n>>; rel="next"
  ```

  No `Link` header on the last page. Clients that ignore the header degrade to "first page only" behavior — no breakage.

- **`MAX_THREAD_LIST_RECEIPTS` truncation goes away**. With pagination, there is no need for a hard cap on `list.length`. The clamped `limit` is the only ceiling per response.

### Status codes

- 200 + body — happy path (with or without next page).
- 400 — invalid `limit` (non-integer, ≤ 0) or invalid `cursor` (malformed base64, malformed `lsn:<n>` shape after decode, or LSN that doesn't belong to this thread).
- 404 — malformed thread id (unchanged from branch 5).
- 401 — missing bearer (unchanged).

### Why `Link` header instead of changing the body shape

- Backward-compatible: existing branch-5 clients (the renderer dev fetch path) keep working.
- Standardized: RFC 8288 is the boring-tech default; both `fetch` and any HTTP library can pluck the header.
- The bare-array body keeps the JSON-by-default ergonomics from branch 5.

## Storage location (Worker A)

- Default path: `<app.getPath("userData")>/event-log.sqlite`
- For `createBroker` callers that don't supply a `receiptStore`, the broker still defaults to `InMemoryReceiptStore` (unchanged). The durable store is opt-in via host wiring — the Electron main process is responsible for constructing it. Branch 6 does NOT change the default.
- Wiring to `apps/desktop/src/main/` is OUT OF SCOPE for the parallel workers and lands in the integration commit.

## Test plan

### Worker A — sqlite-receipt-store.spec.ts

1. `put` returns `{existed: false}`, then `get` round-trips the receipt.
2. Duplicate `put` returns `{existed: true}`; event_log row count stays at 1.
3. `list({ threadId })` returns receipts for that thread only, in LSN order.
4. `list({ threadId, limit: 5 })` returns ≤5 items and a `nextCursor` when more exist.
5. `list({ threadId, cursor: <from prior call> })` skips already-seen items.
6. `list({ limit: 9999 })` clamps to 1000.
7. `list({ limit: 0 })` rejects with a thrown error (the HTTP layer surfaces 400).
8. `list({ cursor: "not-base64-!@#" })` rejects (HTTP → 400).
9. `close()` is idempotent.
10. After restart (open + close + open the same file): receipts persist, LSN sequence continues from highest+1.
11. WAL rollback safety: a `put` that fails mid-transaction (force-thrown after event_log insert, before projection insert) MUST leave both tables empty. Use a transaction-instrumented mock.

### Worker A — event-log.spec.ts

1. `append` assigns monotonically increasing LSNs.
2. `readFromLsn(0, 10)` returns first 10 events; `readFromLsn(5, 10)` skips lsn ≤ 5.
3. `readFromLsn(huge, 10)` returns `[]`.
4. `highestLsn()` matches the last appended LSN.
5. Migrations run idempotently — open the same file twice; second open is a no-op.

### Worker A — receipt-store-parity.spec.ts

Reusable test suite that takes a `ReceiptStore` factory and runs the same 8–10 contract tests against both `InMemoryReceiptStore` and `SqliteReceiptStore`. Confirms interface parity.

### Worker B — receipts.spec.ts additions

1. First page: `GET /api/threads/<tid>/receipts?limit=2` with 5 receipts → 200, body has 2, `Link` header present.
2. Last page: follow the `Link` cursor twice → 200, body has 1, no `Link` header.
3. Default limit: with 150 receipts, no `limit` query → returns 100, has `Link`.
4. Invalid limit: `?limit=0` → 400 with `{ "error": "invalid_limit" }`.
5. Invalid cursor: `?cursor=not%21base64` → 400.
6. Empty thread: `?cursor=` (absent) → 200, empty array, no `Link`.

## Verification commands (both workers MUST run before commit)

```bash
cd packages/broker && bunx tsc --noEmit
cd packages/broker && bun run test
cd packages/broker && bunx biome check src/ tests/
bunx secretlint
```

## Disposition reporting

Each worker ends its run with:

```markdown
| # | Finding | Status | Notes |
|---|---------|--------|-------|
| 1 | <short> | FIXED   | commit <sha> |
| 2 | <short> | SKIPPED | <reason> |
| 3 | <short> | DEFERRED | <issue / next branch> |
```
