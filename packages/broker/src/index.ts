// Public surface of @wuphf/broker. The broker exposes a loopback
// HTTP+SSE+WebSocket listener with a DNS-rebinding guard and bearer-token
// auth. Hosts (Electron utility process, future `wuphf serve --headless`)
// import `createBroker` and ignore the rest of the module graph.

// Idempotency parsing primitives. The atomic-append paths
// (`CostLedger.appendCostEventIdempotent` etc.) take a `ParsedIdempotencyKey`
// and do the lookup/store inside the same SQLite transaction, so consumers
// no longer compose their own `CommandIdempotencyStore` — the parser is the
// only piece they need.
export type {
  CostCommand,
  ParsedIdempotencyKey,
} from "./cost-ledger/idempotency.ts";
export {
  COST_COMMAND_VALUES,
  parseIdempotencyKey,
} from "./cost-ledger/idempotency.ts";
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
} from "./cost-ledger/index.ts";
export { createCostLedger } from "./cost-ledger/index.ts";
export type { ReplayCheckReport, ReplayDiscrepancy } from "./cost-ledger/replay-check.ts";
export { runReplayCheck } from "./cost-ledger/replay-check.ts";
export { createBroker } from "./listener.ts";
export type {
  InMemoryReceiptStoreConfig,
  ListFilter,
  ListPage,
  ReceiptStore,
} from "./receipt-store.ts";
export {
  DEFAULT_LIST_LIMIT,
  InMemoryReceiptStore,
  InvalidListCursorError,
  InvalidListLimitError,
  MAX_LIST_LIMIT,
  ReceiptStoreBusyError,
  ReceiptStoreFullError,
  ReceiptStoreUnavailableError,
} from "./receipt-store.ts";
// `SqliteReceiptStore` is intentionally NOT re-exported from the root.
// It pulls in the native `better-sqlite3` binding via static import,
// which evaluates at module load. Hosts that want the durable store
// import it from the `@wuphf/broker/sqlite` subpath so consumers that
// only need the listener + in-memory store don't pay the native-load
// cost. See `docs/event-log-projections-design.md` § "Package surface".
export { generateApiToken } from "./token.ts";
export type {
  BrokerConfig,
  BrokerHandle,
  BrokerLogger,
  RendererBundleSource,
} from "./types.ts";
export { NOOP_LOGGER } from "./types.ts";
