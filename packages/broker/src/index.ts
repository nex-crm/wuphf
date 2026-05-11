// Public surface of @wuphf/broker. The broker exposes a loopback
// HTTP+SSE+WebSocket listener with a DNS-rebinding guard and bearer-token
// auth. Hosts (Electron utility process, future `wuphf serve --headless`)
// import `createBroker` and ignore the rest of the module graph.

export { createBroker } from "./listener.ts";
export type { ReceiptStore } from "./receipt-store.ts";
export { InMemoryReceiptStore } from "./receipt-store.ts";
export type { SqliteReceiptStoreConfig } from "./sqlite-receipt-store.ts";
export { SqliteReceiptStore } from "./sqlite-receipt-store.ts";
export { generateApiToken } from "./token.ts";
export type {
  BrokerConfig,
  BrokerHandle,
  BrokerLogger,
  RendererBundleSource,
} from "./types.ts";
export { NOOP_LOGGER } from "./types.ts";
