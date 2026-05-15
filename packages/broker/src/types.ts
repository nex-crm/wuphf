// Public broker types. No I/O here — implementations live in sibling modules.

import type { AgentId, ApiToken, BrokerPort } from "@wuphf/protocol";
import type Database from "better-sqlite3";
import type { CostLedger } from "./cost-ledger/index.ts";
import type { ReceiptStore } from "./receipt-store.ts";
import type { RunnerRouteConfig } from "./runners/route.ts";
import type { Clock, WebAuthnPolicyConfig, WebAuthnStore } from "./webauthn/types.ts";

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
   * Clock used by time-expiring broker control-plane state. Tests pass a fake
   * clock so expiry assertions are deterministic; production defaults to the
   * system wall clock.
   */
  readonly clock?: Clock;
  /**
   * Receipt persistence backend. When absent, `createBroker` constructs
   * an in-memory store (`InMemoryReceiptStore`) — process-local, lost
   * across restarts. Durable hosts pass a `SqliteReceiptStore` from
   * `@wuphf/broker/sqlite`. The interface is intentionally minimal:
   * idempotency-key semantics (byte-identical retry returns 200 no-op)
   * are deferred to a future widening of `put`'s return shape.
   *
   * Ownership: when a host supplies its own `receiptStore`, the host
   * owns its lifecycle. `broker.stop()` closes the HTTP/WebSocket
   * surface and the WS server but does NOT close the injected store —
   * call `store.close()` (or equivalent) after `broker.stop()` to
   * release any underlying handle.
   */
  readonly receiptStore?: ReceiptStore;
  /**
   * Root directory for runner working directories. The runner route resolves
   * every requested cwd under `<workspaceRoot>/<agentId>/` after realpath so
   * one agent bearer cannot point a subprocess at another agent's workspace.
   * When omitted, the route uses `WUPHF_WORKSPACE_ROOT` or
   * `~/.wuphf/workspaces`.
   */
  readonly workspaceRoot?: string | undefined;
  /**
   * Optional cost-ledger feature. When supplied, `/api/v1/cost/*` routes
   * are mounted; when absent, those paths return 404 like any other
   * unknown `/api/*` route. Hosts construct the deps via
   * `createCostLedger(db, eventLog)`.
   *
   * Idempotency is now atomic with ledger appends (see
   * `CostLedger.appendCostEventIdempotent`); the route layer doesn't
   * need a separate `CommandIdempotencyStore` injected.
   *
   * The broker does NOT own the database — closing the broker does not
   * close `db`. Host owns lifecycle.
   */
  readonly cost?: {
    readonly ledger: CostLedger;
    readonly db: Database.Database;
    /**
     * Additional bearer-like capability required for cost-ledger mutation
     * routes. Read routes continue to use the broker bearer only.
     */
    readonly operatorToken?: ApiToken;
  };
  /**
   * Optional agent runner routes. When supplied, POST /api/runners and
   * GET /api/runners/:id/events are mounted. The broker owns bearer-to-agent
   * authorization and injects a BrokerIdentity through this config; runners
   * never hold broker identity directly.
   */
  readonly runners?: Omit<RunnerRouteConfig, "receiptStore" | "workspaceRoot">;
  /**
   * Optional WebAuthn co-sign routes. When supplied,
   * `/api/webauthn/{registration,cosign}/*` are mounted. The store owns
   * credential/challenge/token persistence; the bearer-to-agent map binds
   * registration and token issuance to one agent.
   */
  readonly webauthn?: WebAuthnPolicyConfig & {
    readonly store: WebAuthnStore;
    readonly tokenAgentIds: ReadonlyMap<ApiToken, AgentId>;
  };
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
