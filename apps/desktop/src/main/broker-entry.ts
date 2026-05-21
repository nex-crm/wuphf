// THIS FILE RUNS IN THE UTILITY PROCESS, NOT THE MAIN PROCESS.
// No Electron main API is available here.
//
// Branch-4 entry: spawns @wuphf/broker on a loopback ephemeral port and
// reports `{ ready, brokerUrl }` back to the supervisor. The runtime helper
// opens durable SQLite-backed receipt + WebAuthn stores only when their env
// paths are plumbed by Electron main, keeping `node:sqlite` access lazy for
// headless tests.

import type { BrokerLogger } from "@wuphf/broker";

import {
  type DesktopBrokerRuntime,
  RENDERER_DIST_ENV,
  startDesktopBrokerFromEnv,
} from "./broker-entry-runtime.ts";
import { toDesktopBrowserBrokerUrl } from "./broker-url.ts";

const parentPort = process.parentPort;
if (!parentPort) {
  // The parent-port channel is missing, so we have nowhere to send a
  // structured event yet. stderr is the last resort for the supervisor's
  // `child_process_gone` log.
  console.error("broker-entry must run as a utility process");
  process.exit(1);
}

const ALIVE_INTERVAL_MS = 1_000;

let aliveInterval: NodeJS.Timeout | null = null;
let runtime: DesktopBrokerRuntime | null = null;
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
  if (runtime !== null) {
    try {
      await runtime.close();
    } catch {
      // Broker/store shutdown failures must not block process exit; the
      // supervisor will SIGKILL after the force grace if needed.
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
  runtime = await startDesktopBrokerFromEnv({ env: process.env, logger });
  const readyUrl =
    typeof process.env[RENDERER_DIST_ENV] === "string" && process.env[RENDERER_DIST_ENV].length > 0
      ? toDesktopBrowserBrokerUrl(runtime.broker.url)
      : runtime.broker.url;
  sendReady(readyUrl);
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
