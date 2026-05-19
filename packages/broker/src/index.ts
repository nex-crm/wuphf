// Public surface of @wuphf/broker. The broker exposes a loopback
// HTTP+SSE+WebSocket listener with a DNS-rebinding guard and bearer-token
// auth. Hosts (Electron utility process, future `wuphf serve --headless`)
// import `createBroker` and ignore the rest of the module graph.
//
// Cost-ledger, thread, and event-log primitives live on subpaths so consumers
// that only need the listener don't pull in storage internals.
// `SqliteReceiptStore` is on `@wuphf/broker/sqlite` for the same reason
// (native binding load cost).

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
export type {
  AgentRunnerFactoryDeps,
  RunnerCostLedger,
  RunnerEventLog,
} from "./runners/factory.ts";
export { createAgentRunnerForBroker } from "./runners/factory.ts";
export type { RunnerRouteConfig, RunnerRouteState } from "./runners/route.ts";
export { generateApiToken } from "./token.ts";
export type {
  BrokerConfig,
  BrokerHandle,
  BrokerLogger,
  RendererBundleSource,
} from "./types.ts";
export { NOOP_LOGGER } from "./types.ts";
