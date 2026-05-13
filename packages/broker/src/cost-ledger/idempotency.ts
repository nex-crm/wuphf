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
// to ledger writes. Rows are eligible for prune after the default TTL below;
// pruning only disables replay for expired keys and never deletes ledger rows.

export type CostCommand = "cost.event" | "cost.budget.set" | "cost.budget.tombstone";

export const DEFAULT_COMMAND_IDEMPOTENCY_TTL_MS = 24 * 60 * 60 * 1000;

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

// The idempotency-row storage and atomic replay are now owned by
// `CostLedger.appendCostEventIdempotent` and `appendBudgetSetIdempotent`
// (see `projections.ts`). The earlier `runIdempotent` / `CommandIdempotencyStore`
// pair was removed in the B1 fix because they ran lookup/append/store as
// three separate operations, opening a crash window between the ledger
// commit and the idempotency-row insert. This module now only exposes
// the key parser the routes use to validate the `Idempotency-Key` header.
