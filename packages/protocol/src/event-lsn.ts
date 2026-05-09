// Opaque, monotonically-orderable position in the events log.
//
// v1 wire format: "v1:<decimal-non-negative-integer>" where the integer fits
// in JS Number.MAX_SAFE_INTEGER (2^53 - 1). The wire form is unbounded
// decimal, but THIS implementation only round-trips through `parseLsn` if the
// value fits in a JS safe integer — `parseLsn` rejects anything larger and
// `lsnFromV1Number` enforces the same bound at construction time so a token
// minted by one helper cannot fail the other. Cross-language verifiers MUST
// either use the same safe-integer bound or coordinate a v2 wire format
// (likely string-arithmetic / bigint) before exceeding it.
//
// Future v2 will add instance scoping: "v2:<instanceId>:<decimal-uint>".
// Callers MUST go through the helpers in this module to compare or step LSNs;
// reaching into the string locks us out of the multi-instance extension
// without a hash-chain break (the audit chain canonicalizes seqNo into the
// hashed bytes — changing representation later breaks every existing chain).
//
// Multi-instance proper (vector clocks, CRDTs, federated writers) is out of
// scope for the rewrite. WUPHF is single-user-per-instance by product
// decision; multi-user is Nex Cloud. The pre-bake exists only so a v2 can
// arrive without rewriting persisted history.

import type { Brand } from "./brand.ts";

export type EventLsn = Brand<string, "EventLsn">;

const V1_PREFIX = "v1:";

/**
 * The LSN of the first appended event. Genesis sentinel for the audit chain
 * and the starting point a verifier compares against record[0].seqNo.
 */
export const GENESIS_LSN: EventLsn = `${V1_PREFIX}0` as EventLsn;

/**
 * Construct an EventLsn from a v1 local sequence number. Throws on negative
 * or non-integer input. Use only at the appender (where local sequence is
 * authoritative) or in tests / migration tooling.
 *
 * Bounded to `Number.isSafeInteger` so any LSN minted here can be parsed
 * back by `parseLsn` without loss — otherwise the appender could emit a
 * token (e.g. via `nextLsn(MAX_SAFE_INTEGER)`) that the verifier rejects.
 * Hitting this bound on a real install would take 285,000 years at one
 * event per millisecond; if it ever happens, migrate to a v2 wire format
 * with bigint storage rather than relaxing the guard.
 */
export function lsnFromV1Number(n: number): EventLsn {
  if (!Number.isSafeInteger(n) || n < 0) {
    throw new Error(`lsnFromV1Number: expected non-negative safe integer, got ${n}`);
  }
  return `${V1_PREFIX}${n}` as EventLsn;
}

interface ParsedLsnV1 {
  readonly format: "v1";
  readonly localLsn: number;
}

export type ParsedLsn = ParsedLsnV1;

/**
 * Parse an EventLsn into its structured form. v2 will return a discriminated
 * union including instanceId; callers should switch on `format`.
 */
export function parseLsn(lsn: EventLsn): ParsedLsn {
  const s = lsn as string;
  if (s.startsWith(V1_PREFIX)) {
    const tail = s.slice(V1_PREFIX.length);
    if (tail.length === 0) {
      throw new Error(`parseLsn: empty v1 LSN: ${s}`);
    }
    // Reject leading zeros, signs, and non-digits to keep the wire form canonical.
    if (!/^(0|[1-9]\d*)$/.test(tail)) {
      throw new Error(`parseLsn: malformed v1 LSN: ${s}`);
    }
    const n = Number.parseInt(tail, 10);
    if (!Number.isSafeInteger(n) || n < 0) {
      throw new Error(`parseLsn: v1 LSN out of safe-integer range: ${s}`);
    }
    return { format: "v1", localLsn: n };
  }
  throw new Error(`parseLsn: unrecognized LSN format: ${s}`);
}

/**
 * Total order on EventLsn. v1 has no instance scoping so ordering is by
 * localLsn. v2 will define cross-instance ordering (e.g., per-instance
 * sequence + tie-break on instanceId).
 */
export function compareLsn(a: EventLsn, b: EventLsn): -1 | 0 | 1 {
  const pa = parseLsn(a);
  const pb = parseLsn(b);
  if (pa.localLsn < pb.localLsn) return -1;
  if (pa.localLsn > pb.localLsn) return 1;
  return 0;
}

export function isAfter(a: EventLsn, b: EventLsn): boolean {
  return compareLsn(a, b) === 1;
}

export function isBefore(a: EventLsn, b: EventLsn): boolean {
  return compareLsn(a, b) === -1;
}

/**
 * Equality on the wire form. For canonical v1 LSNs this reduces to string
 * equality; the helper exists so callers don't pattern-match the string.
 */
export function isEqualLsn(a: EventLsn, b: EventLsn): boolean {
  return (a as string) === (b as string);
}

/**
 * The next LSN in v1 sequence. Used by the appender to assign monotonic
 * positions and by chain verifiers to predict the next expected LSN.
 *
 * v2 will require an instance context to disambiguate; this helper will
 * change shape, which is why callers should not synthesize LSNs by hand.
 */
export function nextLsn(lsn: EventLsn): EventLsn {
  const p = parseLsn(lsn);
  // lsnFromV1Number's safe-integer guard catches the MAX_SAFE_INTEGER + 1
  // overflow with a clear message rather than emitting a token that
  // parseLsn would later reject.
  return lsnFromV1Number(p.localLsn + 1);
}
