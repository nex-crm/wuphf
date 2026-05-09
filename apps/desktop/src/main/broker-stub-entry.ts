// THIS FILE RUNS IN THE UTILITY PROCESS, NOT THE MAIN PROCESS.
// No Electron main API is available here.

const parentPort = process.parentPort;
if (!parentPort) {
  console.error("broker-stub must run as a utility process");
  process.exit(1);
}

const ALIVE_INTERVAL_MS = 1_000;

let aliveInterval: NodeJS.Timeout | null = null;

function sendAlive(): void {
  parentPort.postMessage({ alive: true });
}

function shutdown(): void {
  if (aliveInterval !== null) {
    clearInterval(aliveInterval);
    aliveInterval = null;
  }
  process.exit(0);
}

parentPort.on("message", (event) => {
  if (isShutdownMessage(event.data)) {
    shutdown();
  }
});

process.on("SIGTERM", shutdown);
process.on("SIGINT", shutdown);

sendAlive();
aliveInterval = setInterval(sendAlive, ALIVE_INTERVAL_MS);

function isShutdownMessage(message: unknown): message is { readonly type: "shutdown" } {
  return (
    typeof message === "object" &&
    message !== null &&
    Object.hasOwn(message, "type") &&
    (message as { readonly type?: unknown }).type === "shutdown"
  );
}
