# Expert E — Date / wire-time hygiene audit

## Inventory of every `Date` / time-ish call site

No `Date.now()`, `Date.parse()`, or `.valueOf()` exists in `src/`, `tests/`, or `scripts/`.

| File:line | Form | Context (1 line) | Classification |
|---|---|---|---|
| src/audit-event.ts:125,134 | `Date` fields | `timestamp`, Merkle `signedAt` | marking time |
| src/audit-event.ts:201,261 | `.toISOString()` | emit `signedAt` / commit event `timestamp` into canonical bytes | marking time; not uniqueness: same-ms events still differ by `seqNo` and `prevHash` |
| src/audit-event.ts:598,691,696,700-701 | `Date`, `new Date`, `.getTime`, `.toISOString` | validate/parse ISO instants | marking time |
| src/receipt-types.ts:52,63,64,75,110,111,124,140,192,193 | `Date` fields | receipt/source/tool/approval/write timestamps | marking time |
| src/receipt.ts:273,274,307,320,321,334,377,378,395,410 | `dateToJson` calls | emit receipt timestamps | marking time |
| src/receipt.ts:423,439,489,516,517,541,617,618,646,657,677 | `required/optionalDateFromJson` | parse receipt timestamps | marking time |
| src/receipt.ts:903-939 | `Date`, `.toISOString`, `new Date`, `.getTime` | shared date codec | marking time |
| src/receipt-validator.ts:298,299,374,397,398,434,518,541,542,607,795-827 | `Date`, `instanceof`, `.getTime`, `.toISOString` | validate dates and interval errors | marking time / per-record interval sanity |
| src/receipt-validator.ts:354-362,403-411,556-564,697-705 | temporal comparison | finished/expiry/approved must be after start/issued | validity window only, not cross-event ordering |
| src/budgets.ts:181-182,337-348 | `Date`, `.getTime` | approval-token max lifetime | validity-window budget only |
| src/ipc.ts:130,259,414-425,452,529-530,544,552,580,592 | ISO fields, `Date`, `.getTime` | approval request shape and response/SSE time strings | marking time / token validity only |
| src/canonical-json.ts:21 | `Date` comment | notes `Date` is rejected as non-plain object | marking-time hygiene note |
| tests/audit-event.spec.ts:89,270,309,342,556,741-743 | `new Date`, `.toISOString`, `.getTime` | fixtures and fixture parser | test-only marking time |
| tests/receipt.spec.ts:410,437,453,471,810,811,825,826,838,839,852,854,882,897,952,953,995,1010,1038,1040,1053,1054,1066,1067 | `new Date` | fixtures for valid/invalid receipt intervals | test-only marking time / validity windows |
| tests/ipc.spec.ts:331,339,349,357,388,471,472 | `new Date`, `.getTime`, ISO string | approval-token lifetime/expiry fixtures | test-only validity windows |
| tests/budgets.spec.ts:123,128,135,304,305,318,319,345,346,379,405,428,429 | `new Date`, `.getTime` | budget and receipt fixtures | test-only marking time / validity windows |
| tests/frozen-args.spec.ts:204-205; tests/sanitized-string.spec.ts:292 | `new Date()` | assert `Date` rejected as non-plain input | test-only boundary case |
| scripts/demo.ts:254,461,531,532,548,549,561,562,573,574,589 | `new Date` | demo audit/receipt fixtures | demo-only marking time |
| No hits | all other files in src/tests/scripts | brand, lsn, sanitizer, sha, literals, utility, invariant script, README | no Date/time use |

## Forbidden uses found

None.

## Edge-case audit

- Approval token `notBefore < expiresAt` is used only as a per-token validity window and lifetime budget (`MAX_APPROVAL_TOKEN_LIFETIME_MS`), not to order approvals across tokens.
- `ReceiptId` is only a branded uppercase ULID regex here. This package does not generate ULIDs, sort them, or require timestamp-prefix uniqueness. Receipt ordering by ULID, if used elsewhere, is outside this package.
- Audit event creation/verification uses `seqNo` (`EventLsn`) plus `prevHash`/`eventHash` for ordering. `timestamp` is included in canonical bytes to commit when the event was marked, but same-ms events cannot collide solely on time because `seqNo` and `prevHash` also differ.
- `payload.body` is opaque bytes. `verifyChain` never inspects body timestamps; it verifies `seqNo`, `prevHash`, and `eventHash`.
- Receipt timestamps (`startedAt`, `finishedAt`, `approvedAt`, etc.) are emitted/validated for human-readable lifecycle consistency, not for sequence construction.

## Custom check design

```bash
# scripts/check-date-hygiene.sh
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

violators=$(
  grep -rnE --include='*.ts' \
    '(Date\.now\(|Date\.parse\(|performance\.now\(|\.valueOf\(\)|new Date\(\)|\.getTime\(\)|\.toISOString\(\))' \
    src/ tests/ scripts/ 2>/dev/null |
  grep -Ei '(id|key|hash|seq|lsn|sort|order|compare|dedup|unique|monotonic)' |
  grep -vFf scripts/check-date-hygiene.allowlist || true
)

if [ -n "$violators" ]; then
  printf 'FORBIDDEN date-derived uniqueness/ordering candidate:\n%s\n' "$violators" >&2
  exit 1
fi
```

Patterns: ban date APIs when same-line context mentions IDs, keys, hashes, sequence/order, sorting, dedup, uniqueness, or monotonicity. Also fail any raw `Date.now()` unless allowlisted.

Allowlist rationale: `src/audit-event.ts:201,261` commit marked times, not order; `src/receipt.ts:903-939` codec; `src/receipt-validator.ts:801-824`, `src/budgets.ts:344-358`, `src/ipc.ts:452` interval/TTL validation; tests/demo fixture lines above. Exit non-zero on candidates. Wire pre-commit through lefthook for changed `packages/protocol/**/*.ts`; wire CI by invoking it from `bash scripts/test-protocol.sh` before Vitest.

## AGENTS.md addition

### 14. Date APIs are for marking time only

`Date`, `Date.now()`, `new Date(...)`, `.getTime()`, `.toISOString()`, and related wall-clock APIs MUST NOT provide uniqueness, ordering, deduplication, hash uniqueness, or monotonic counters. Millisecond precision collides under rapid events. Use `EventLsn` for audit ordering, explicit sequence/counter fields for monotonic state, and random/ULID generators for IDs. Date values may record when something happened, serialize that marked time, or enforce a local validity window such as `issuedAt < expiresAt`; they must not decide cross-event order.

## Verdict

- Forbidden uses found: 0.
- Top severity findings: none.
- Package is currently safe for this policy. The real ordering primitive is `EventLsn`; date comparisons are local lifecycle/TTL checks.

| # | Finding | Status | Notes |
|---|---------|--------|-------|
| 1 | Date-derived uniqueness/ordering in protocol source | SKIPPED | no current forbidden use found |
