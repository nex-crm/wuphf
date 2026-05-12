// Public surface of `@wuphf/broker/cost-ledger` — everything a host
// needs to build, drive, and audit the cost ledger from outside the
// broker package. Split off the broker root export (#820) so the root
// stays scoped to `createBroker` per `packages/broker/AGENTS.md:3`.

// Event log primitives. Hosts constructing a cost ledger need these to
// open the database, apply migrations, and instantiate the event log
// the ledger writes through. Re-exported from the cost-ledger subpath
// so the broker root doesn't surface storage internals.
export type {
  AppendArgs,
  EventLog,
  EventLogRecord,
  EventType,
  OpenDatabaseArgs,
} from "../event-log/index.ts";
export {
  CURRENT_SCHEMA_VERSION,
  createEventLog,
  openDatabase,
  runMigrations,
} from "../event-log/index.ts";

// Idempotency parsing primitives. The atomic-append paths
// (`CostLedger.appendCostEventIdempotent` etc.) take a `ParsedIdempotencyKey`
// and do the lookup/store inside the same SQLite transaction.
export type { CostCommand, ParsedIdempotencyKey } from "./idempotency.ts";
export { COST_COMMAND_VALUES, parseIdempotencyKey } from "./idempotency.ts";
// Projection store + ledger writer.
export type {
  AgentSpendRow,
  BudgetRow,
  BudgetSetAppendResult,
  CostEventAppendResult,
  CostLedger,
  IdempotentAppendResult,
  IdempotentBudgetSetArgs,
  IdempotentCostEventArgs,
  TaskSpendRow,
  ThresholdCrossedAppendResult,
  ThresholdCrossingRow,
} from "./projections.ts";
export { createCostLedger } from "./projections.ts";
// Replay-check, used by the cost-ledger health route + nightly drift
// checks. The report shape is the authoritative discrepancy surface.
export type { ReplayCheckReport, ReplayDiscrepancy } from "./replay-check.ts";
export { runReplayCheck } from "./replay-check.ts";
