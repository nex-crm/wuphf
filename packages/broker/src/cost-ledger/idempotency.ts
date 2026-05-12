// Command idempotency.
//
// Per §15.A:1095, every state-changing cost command (`POST /api/v1/cost/events`,
// `POST /api/v1/cost/budgets`, `DELETE /api/v1/cost/budgets/:id`) requires an
// `Idempotency-Key` header in the form `cmd_<command>_<26-char-ULID>`. On
// duplicate POST with the same key, the broker returns the originally-stored
// response byte for byte — same status code, same payload. This lets clients
// safely retry after a lost 2xx without risk of double-applying the command.
//
// The key shape is structural, not magic:
//   cmd_<command>_<ULID>
//
// where:
//   - `<command>` is one of the registered command names below and must match
//     the route handling the request (so a key minted for cost.event cannot
//     replay a cost.budget.set response, or vice versa)
//   - `<ULID>` is a 26-char Crockford-base32 ULID
//
// Persistence: `command_idempotency` table (see 002_cost_ledger.sql). The
// status code and response payload are stored; we record `created_at_lsn`
// when the command produced an event so the operator can correlate retries
// to ledger writes.

import type Database from "better-sqlite3";

import type { EventLog } from "../event-log/index.ts";

export type CostCommand = "cost.event" | "cost.budget.set" | "cost.budget.tombstone";

export const COST_COMMAND_VALUES: readonly CostCommand[] = [
  "cost.event",
  "cost.budget.set",
  "cost.budget.tombstone",
];

const COST_COMMAND_SET: ReadonlySet<string> = new Set<string>(COST_COMMAND_VALUES);

/**
 * Match `cmd_<command>_<ULID>` exactly. `<command>` allows `.`-separated
 * lowercase segments to match the cost-command surface; `<ULID>` is the
 * standard 26-char Crockford-base32 form (uppercase). The whole key is at
 * most 1+3+26+3+26 = 59 chars — well inside any sensible HTTP header cap.
 */
const KEY_RE = /^cmd_([a-z][a-z0-9.]*[a-z0-9])_([0-9A-HJKMNP-TV-Z]{26})$/;

export interface ParsedIdempotencyKey {
  readonly raw: string;
  readonly command: CostCommand;
  readonly ulid: string;
}

export type IdempotencyParseError =
  | { readonly code: "missing" }
  | { readonly code: "malformed"; readonly reason: string }
  | { readonly code: "unknown_command"; readonly command: string }
  | { readonly code: "command_mismatch"; readonly expected: CostCommand; readonly actual: string };

export type IdempotencyParseResult =
  | { readonly ok: true; readonly key: ParsedIdempotencyKey }
  | { readonly ok: false; readonly error: IdempotencyParseError };

/**
 * Parse and validate an `Idempotency-Key` header. The expected command is
 * pinned by the route so a key minted for one command cannot replay another.
 */
export function parseIdempotencyKey(
  raw: string | undefined,
  expectedCommand: CostCommand,
): IdempotencyParseResult {
  if (raw === undefined || raw.length === 0) {
    return { ok: false, error: { code: "missing" } };
  }
  const match = KEY_RE.exec(raw);
  if (match === null) {
    return {
      ok: false,
      error: { code: "malformed", reason: "must match cmd_<command>_<26-char-ULID>" },
    };
  }
  const command = match[1] ?? "";
  const ulid = match[2] ?? "";
  if (!COST_COMMAND_SET.has(command)) {
    return { ok: false, error: { code: "unknown_command", command } };
  }
  if (command !== expectedCommand) {
    return {
      ok: false,
      error: { code: "command_mismatch", expected: expectedCommand, actual: command },
    };
  }
  return { ok: true, key: { raw, command: command as CostCommand, ulid } };
}

export interface StoredResponse {
  readonly statusCode: number;
  readonly payload: Buffer;
  readonly createdAtLsn: number | null;
  readonly createdAtMs: number;
}

export interface CommandIdempotencyStore {
  /**
   * Replay the stored response if a key has been seen before; returns
   * `null` otherwise. Read-only — does not allocate or insert.
   */
  lookup(key: ParsedIdempotencyKey): StoredResponse | null;

  /**
   * Record a freshly-produced command response. Throws on duplicate; the
   * caller is expected to have called `lookup` first (the route helper
   * `runIdempotent` below enforces this). `createdAtLsn` may be null if
   * the command was rejected before appending to event_log (e.g. a 4xx
   * validation failure).
   */
  store(
    key: ParsedIdempotencyKey,
    response: { readonly statusCode: number; readonly payload: Buffer },
    createdAtLsn: number | null,
    createdAtMs: number,
  ): void;
}

interface StoredResponseDbRow {
  readonly statusCode: number;
  readonly responsePayload: Buffer;
  readonly createdAtLsn: number | null;
  readonly createdAtMs: number;
}

export function createCommandIdempotencyStore(db: Database.Database): CommandIdempotencyStore {
  const lookupStmt = db.prepare<[string, string], StoredResponseDbRow>(
    `SELECT status_code AS statusCode, response_payload AS responsePayload,
            created_at_lsn AS createdAtLsn, created_at_ms AS createdAtMs
     FROM command_idempotency
     WHERE idempotency_key = ? AND command = ?`,
  );
  const insertStmt = db.prepare<[string, string, number, Buffer, number | null, number]>(
    `INSERT INTO command_idempotency
       (idempotency_key, command, status_code, response_payload, created_at_lsn, created_at_ms)
     VALUES (?, ?, ?, ?, ?, ?)`,
  );

  return {
    lookup(key: ParsedIdempotencyKey): StoredResponse | null {
      const row = lookupStmt.get(key.raw, key.command);
      if (row === undefined) return null;
      return {
        statusCode: row.statusCode,
        payload: Buffer.from(row.responsePayload),
        createdAtLsn: row.createdAtLsn,
        createdAtMs: row.createdAtMs,
      };
    },
    store(
      key: ParsedIdempotencyKey,
      response: { readonly statusCode: number; readonly payload: Buffer },
      createdAtLsn: number | null,
      createdAtMs: number,
    ): void {
      insertStmt.run(
        key.raw,
        key.command,
        response.statusCode,
        response.payload,
        createdAtLsn,
        createdAtMs,
      );
    },
  };
}

export interface IdempotentResult {
  readonly replayed: boolean;
  readonly statusCode: number;
  readonly payload: Buffer;
}

/**
 * Wraps a command execution with idempotency. If the key has been seen
 * before, returns the stored response (replayed: true). Otherwise runs
 * `produce`, stores its result, and returns it (replayed: false).
 *
 * `produce` MUST be deterministic given the request body — re-running it
 * after a crash must produce the same response. The cost-event and
 * budget-set commands satisfy this because the event_log + projection
 * append are wrapped in one SQLite transaction; if the transaction commits
 * before we store the response, on the duplicate retry we'll see the
 * command already applied (PRIMARY KEY collisions or no-op upserts) and
 * the `produce` function should re-derive the same response from the
 * current projection state.
 */
export function runIdempotent(
  store: CommandIdempotencyStore,
  key: ParsedIdempotencyKey,
  nowMs: number,
  produce: () => {
    readonly statusCode: number;
    readonly payload: Buffer;
    readonly createdAtLsn: number | null;
  },
): IdempotentResult {
  const cached = store.lookup(key);
  if (cached !== null) {
    return { replayed: true, statusCode: cached.statusCode, payload: cached.payload };
  }
  const fresh = produce();
  store.store(
    key,
    { statusCode: fresh.statusCode, payload: fresh.payload },
    fresh.createdAtLsn,
    nowMs,
  );
  return { replayed: false, statusCode: fresh.statusCode, payload: fresh.payload };
}

// `EventLog` is unused inside this file but consumers depend on the
// import surface; re-export type only to keep the cost-ledger module's
// surface coherent.
export type { EventLog };
