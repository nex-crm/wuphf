// THIS FILE RUNS IN THE UTILITY PROCESS, NOT THE MAIN PROCESS.
// No Electron main API is available here.
//
// Branch-4 entry: spawns @wuphf/broker on a loopback ephemeral port and
// reports `{ ready, brokerUrl }` back to the supervisor. The renderer
// bundle path is delivered through `WUPHF_RENDERER_DIST`; when absent
// (development electron-vite path), the broker still starts but `/`,
// `/index.html`, and `/assets/*` return 404 — the dev server owns those
// surfaces in dev.

import { existsSync } from "node:fs";

import { type BrokerHandle, type BrokerLogger, createBroker } from "@wuphf/broker";

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

let aliveInterval: NodeJS.Timeout | null = null;
let broker: BrokerHandle | null = null;
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
  broker = await createBroker({
    port: 0,
    renderer,
    logger,
    ...(trustedOrigins !== undefined ? { trustedOrigins } : {}),
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
