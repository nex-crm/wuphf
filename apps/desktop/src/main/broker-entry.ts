// THIS FILE RUNS IN THE UTILITY PROCESS, NOT THE MAIN PROCESS.
// No Electron main API is available here.
//
// Branch-4 entry: spawns @wuphf/broker on a loopback ephemeral port and
// reports `{ ready, brokerUrl }` back to the supervisor. The renderer
// bundle path is delivered through `WUPHF_RENDERER_DIST`; when absent
// (development electron-vite path), the broker still starts but `/`,
// `/index.html`, and `/assets/*` return 404 — the dev server owns those
// surfaces in dev.

import { chmodSync, existsSync, mkdirSync } from "node:fs";
import { dirname } from "node:path";

import { type BrokerHandle, type BrokerLogger, createBroker } from "@wuphf/broker";
// `SqliteReceiptStore` and its native `better-sqlite3` binding are loaded
// lazily via the `@wuphf/broker/sqlite` subpath so the utility process
// doesn't evaluate the native addon when the durable store isn't wired
// (e.g. headless smoke tests, or future hosts that bring their own
// ReceiptStore). Type-only import keeps the field type narrow without
// triggering runtime evaluation.
import type { SqliteReceiptStore } from "@wuphf/broker/sqlite";

const parentPort = process.parentPort;
if (!parentPort) {
  // The parent-port channel is missing, so we have nowhere to send a
  // structured event yet. stderr is the last resort for the supervisor's
  // `child_process_gone` log.
  console.error("broker-entry must run as a utility process");
  process.exit(1);
}

const ALIVE_INTERVAL_MS = 1_000;

const RENDERER_DIST_ENV = "WUPHF_RENDERER_DIST";
const DEV_RENDERER_ORIGIN_ENV = "WUPHF_DEV_RENDERER_ORIGIN";
const RECEIPT_STORE_PATH_ENV = "WUPHF_RECEIPT_STORE_PATH";

let aliveInterval: NodeJS.Timeout | null = null;
let broker: BrokerHandle | null = null;
// Module-scoped so `shutdown()` can close the SQLite handle after
// `broker.stop()`. Relying on process teardown to release the DB
// skips an explicit WAL checkpoint and leaves recovery work for the
// next launch.
let receiptStore: SqliteReceiptStore | null = null;
let shuttingDown = false;

function sendAlive(): void {
  parentPort.postMessage({ alive: true });
}

function sendReady(brokerUrl: string): void {
  parentPort.postMessage({ ready: true, brokerUrl });
}

const logger: BrokerLogger = {
  info: (event, payload) => parentPort.postMessage({ broker_log: "info", event, payload }),
  warn: (event, payload) => parentPort.postMessage({ broker_log: "warn", event, payload }),
  error: (event, payload) => parentPort.postMessage({ broker_log: "error", event, payload }),
};

async function shutdown(): Promise<void> {
  if (shuttingDown) return;
  shuttingDown = true;
  if (aliveInterval !== null) {
    clearInterval(aliveInterval);
    aliveInterval = null;
  }
  if (broker !== null) {
    try {
      await broker.stop();
    } catch {
      // Broker shutdown failures must not block process exit; the supervisor
      // will SIGKILL after the force grace if needed.
    }
  }
  if (receiptStore !== null) {
    try {
      // Triggers SQLite's WAL checkpoint + file handle release. Idempotent;
      // safe to call even when the broker stop above failed.
      receiptStore.close();
    } catch {
      // Same swallow-on-shutdown policy as broker.stop(): a failing close
      // must not deadlock process exit.
    }
  }
  process.exit(0);
}

parentPort.on("message", (event) => {
  if (isShutdownMessage(event.data)) {
    void shutdown();
  }
});

process.on("SIGTERM", () => void shutdown());
process.on("SIGINT", () => void shutdown());

async function main(): Promise<void> {
  const rendererDir = process.env[RENDERER_DIST_ENV];
  // Fail fast when the env var is set but the path doesn't exist. A silent
  // fallback to renderer:null would let the broker report ready and the
  // window load to a 404 for `/`, surfacing a packaging regression as a
  // blank window instead of a structured exit log the supervisor can
  // route into broker_entry_main_failed.
  if (typeof rendererDir === "string" && rendererDir.length > 0 && !existsSync(rendererDir)) {
    throw new Error(`renderer dist directory does not exist: ${rendererDir}`);
  }
  const renderer =
    typeof rendererDir === "string" && rendererDir.length > 0 ? { dir: rendererDir } : null;
  // Dev mode: main/index.ts plumbs the electron-vite dev server origin
  // through this env var so the broker's /api-token gate accepts the
  // legitimate cross-origin bootstrap fetch. Empty in packaged mode.
  const devOrigin = process.env[DEV_RENDERER_ORIGIN_ENV];
  const trustedOrigins =
    typeof devOrigin === "string" && devOrigin.length > 0 ? [devOrigin] : undefined;

  // Open the durable, SQLite event-log-backed ReceiptStore at the path
  // `main/index.ts` plumbed through. If the env var is absent we fall
  // through to `createBroker`'s default (the in-memory store) — useful
  // for tests and the headless smoke path. The `SqliteReceiptStore`
  // module is loaded via dynamic import here so the native
  // `better-sqlite3` binding is only evaluated when the durable store
  // is actually wired.
  const receiptStorePath = process.env[RECEIPT_STORE_PATH_ENV];
  if (typeof receiptStorePath === "string" && receiptStorePath.length > 0) {
    const storeDir = dirname(receiptStorePath);
    // Ensure the parent directory exists. `userData` is created by
    // Electron on first launch; this guards against the host having
    // deleted it (rare but recoverable).
    mkdirSync(storeDir, { recursive: true });
    // POSIX: lock down the receipt store directory and DB sidecar
    // permissions. Receipts can contain local metadata (worktree paths,
    // source reads, model details, error text); on shared systems the
    // default umask may leave the DB world-readable. Windows uses ACLs
    // and ignores `chmod`, so we no-op there.
    if (process.platform !== "win32") {
      tightenStorePermissions(storeDir, receiptStorePath);
    }
    const { SqliteReceiptStore } = await import("@wuphf/broker/sqlite");
    receiptStore = SqliteReceiptStore.open({ path: receiptStorePath });
    // Tighten again after open — better-sqlite3 creates the DB + WAL/SHM
    // sidecars with the process umask, so the post-open pass catches
    // them. Idempotent on the directory.
    if (process.platform !== "win32") {
      tightenStorePermissions(storeDir, receiptStorePath);
    }
  }

  broker = await createBroker({
    port: 0,
    renderer,
    logger,
    ...(trustedOrigins !== undefined ? { trustedOrigins } : {}),
    ...(receiptStore !== null ? { receiptStore } : {}),
  });
  sendReady(broker.url);
  sendAlive();
  aliveInterval = setInterval(sendAlive, ALIVE_INTERVAL_MS);
}

void main().catch((err: unknown) => {
  logger.error("broker_entry_main_failed", {
    error: err instanceof Error ? err.message : String(err),
  });
  process.exit(1);
});

function isShutdownMessage(message: unknown): message is { readonly type: "shutdown" } {
  return (
    typeof message === "object" &&
    message !== null &&
    Object.hasOwn(message, "type") &&
    (message as { readonly type?: unknown }).type === "shutdown"
  );
}

// POSIX-only: tighten the receipt store directory + DB sidecars to
// owner-only access. Best-effort — if a `chmod` fails we keep going
// rather than refuse to start (some sandboxed filesystems error on
// chmod). Windows uses ACLs and ignores `chmod`, so callers branch on
// `process.platform` before calling this.
function tightenStorePermissions(storeDir: string, dbPath: string): void {
  const tryChmod = (path: string, mode: number): void => {
    try {
      chmodSync(path, mode);
    } catch {
      // Filesystem may refuse (network mount, sandbox, etc). Permissions
      // are defense-in-depth on top of the userData isolation; not a
      // showstopper.
    }
  };
  tryChmod(storeDir, 0o700);
  for (const sidecar of [dbPath, `${dbPath}-wal`, `${dbPath}-shm`, `${dbPath}-journal`]) {
    if (existsSync(sidecar)) {
      tryChmod(sidecar, 0o600);
    }
  }
}
