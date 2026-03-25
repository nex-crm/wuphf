/**
 * Worker script — runs claude -p in its own event loop.
 * Receives prompt + args via parentPort, posts result back.
 * This avoids Bun/Ink's event loop stalling on child processes.
 */

import { parentPort } from "node:worker_threads";
import { execFileSync } from "node:child_process";

if (!parentPort) {
  console.error("claude-worker must be run as a Worker thread");
  process.exit(1);
}

parentPort.on("message", (msg: { prompt: string; args: string[] }) => {
  try {
    const stdout = execFileSync("claude", msg.args, {
      timeout: 120_000,
      maxBuffer: 10 * 1024 * 1024,
      stdio: ["ignore", "pipe", "ignore"],
    });
    parentPort!.postMessage({ ok: true, stdout: stdout.toString() });
  } catch (err) {
    parentPort!.postMessage({
      ok: false,
      error: err instanceof Error ? err.message : String(err),
    });
  }
});
