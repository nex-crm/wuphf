// Public broker types. No I/O here — implementations live in sibling modules.

import type { ApiToken, BrokerPort } from "@wuphf/protocol";

import type { ReceiptStore } from "./receipt-store.ts";

export interface BrokerLogger {
  info(event: string, payload?: Readonly<Record<string, unknown>>): void;
  warn(event: string, payload?: Readonly<Record<string, unknown>>): void;
  error(event: string, payload?: Readonly<Record<string, unknown>>): void;
}

/**
 * Renderer-bundle source. Pass `null` to disable static serving — `/`,
 * `/index.html`, and `/assets/*` will return 404. The desktop shell uses
 * this in dev mode (where electron-vite serves the renderer) and in the
 * headless `wuphf serve` mode (where there is no renderer at all).
 *
 * `dir` MUST be an absolute path. The static handler resolves requested
 * paths relative to it and rejects any traversal outside the dir.
 */
export interface RendererBundleSource {
  readonly dir: string;
}

export interface BrokerConfig {
  /**
   * Loopback port to bind. `0` (default) asks the OS for an ephemeral port;
   * `BrokerHandle.port` reports the assigned value.
   */
  readonly port?: number;
  /**
   * Bearer token to use. Pass an explicit value to make tests deterministic;
   * otherwise the broker generates one with `crypto.randomBytes(32)`.
   */
  readonly token?: ApiToken;
  /**
   * Source for `/`, `/index.html`, and `/assets/*`. `null` disables static
   * serving (returns 404). The desktop shell sets this to the packaged
   * renderer bundle path; the headless broker leaves it null.
   */
  readonly renderer?: RendererBundleSource | null;
  /**
   * Extra origins the `/api-token` browser-hardening gate accepts in
   * addition to the broker's own bound origin. The desktop dev-mode flow
   * uses this: in dev, electron-vite serves the renderer from
   * `http://localhost:5173` and the renderer fetches `/api-token` from the
   * broker on `http://127.0.0.1:<eph>` — a legitimate cross-origin probe
   * the strict gate would otherwise reject. In packaged mode this stays
   * empty so the gate enforces same-origin only.
   *
   * Values MUST be bare origins (`http://localhost:5173`, no path/query/
   * fragment). Loopback hosts only (127.0.0.1, localhost, ::1) — the gate
   * rejects entries that don't parse as loopback at config time.
   */
  readonly trustedOrigins?: readonly string[];
  readonly logger?: BrokerLogger;
  /**
   * Receipt persistence backend. When absent, `createBroker` constructs
   * an in-memory store (`InMemoryReceiptStore`) — process-local, lost
   * across restarts. Durable hosts pass a `SqliteReceiptStore` from
   * `@wuphf/broker/sqlite`. The interface is intentionally minimal:
   * idempotency-key semantics (byte-identical retry returns 200 no-op)
   * are deferred to a future widening of `put`'s return shape.
   */
  readonly receiptStore?: ReceiptStore;
}

export interface BrokerHandle {
  readonly url: string;
  readonly port: BrokerPort;
  readonly token: ApiToken;
  /**
   * Stop the listener. Idempotent: calling more than once resolves on the
   * same closure. Active SSE streams and WebSocket connections receive a
   * server-initiated close.
   */
  stop(): Promise<void>;
}

export const NOOP_LOGGER: BrokerLogger = Object.freeze({
  info: () => undefined,
  warn: () => undefined,
  error: () => undefined,
});
