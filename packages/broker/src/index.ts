// Public surface of @wuphf/broker. The broker exposes a loopback
// HTTP+SSE+WebSocket listener with a DNS-rebinding guard and bearer-token
// auth. Hosts (Electron utility process, future `wuphf serve --headless`)
// import `createBroker` and ignore the rest of the module graph.

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
