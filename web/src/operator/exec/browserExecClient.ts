// browserExecClient.ts — the LIVE EXECUTE path: POST a goal to the broker's
// /execute/browser SSE endpoint, which spawns the cua runner (OpenAI plans,
// cua-driver drives the real browser) and streams one JSON event per step. We
// parse the SSE frames and hand each event to the caller. On a 503 (no key or
// no runner on the host) we throw EXEC_UNAVAILABLE so the modal falls back to
// the scripted mock. See docs/specs/operator-cua-migration.md.

import { postStream } from "../../api/client";

// One event from the runner — mirrors runner/cua_exec.py's emit() shapes.
export interface RunnerEvent {
  type: "status" | "action" | "done" | "error";
  // status: running | thinking
  status?: string;
  detail?: string;
  // action:
  label?: string;
  reasoning?: string;
  tool?: string;
  refused?: boolean;
  // done / error:
  result?: string;
  message?: string;
}

// Thrown when the backend can't run (no OpenAI key or no cua runner) — the
// caller treats this as "fall back to the mock", not a hard error.
export const EXEC_UNAVAILABLE = "exec-unavailable";

export interface RunBrowserExecOptions {
  goal: string;
  app?: string;
  windowId?: number;
  signal?: AbortSignal;
  onEvent: (event: RunnerEvent) => void;
}

// Drive a live browser run, calling onEvent for each runner event until the
// stream ends. Resolves when the run is over; rejects on transport error or
// EXEC_UNAVAILABLE. Aborting the signal stops the stream and (server-side, via
// request-context cancel) kills the runner subprocess.
export async function runBrowserExec(
  opts: RunBrowserExecOptions,
): Promise<void> {
  const res = await postStream(
    "/execute/browser",
    { goal: opts.goal, app: opts.app, window_id: opts.windowId },
    { signal: opts.signal },
  );
  if (res.status === 503) {
    throw new Error(EXEC_UNAVAILABLE);
  }
  if (!(res.ok && res.body)) {
    throw new Error(`browser exec failed: ${res.status}`);
  }

  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";

  for (;;) {
    const { value, done } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });
    // SSE frames are separated by a blank line.
    let sep: number;
    while ((sep = buffer.indexOf("\n\n")) !== -1) {
      const frame = buffer.slice(0, sep);
      buffer = buffer.slice(sep + 2);
      emitFrame(frame, opts.onEvent);
    }
  }
}

// Parse the `data:` line(s) of one SSE frame into a RunnerEvent. The closing
// `event: end` frame carries `data: {}` and is skipped.
function emitFrame(frame: string, onEvent: (event: RunnerEvent) => void): void {
  for (const line of frame.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed.startsWith("data:")) continue;
    const payload = trimmed.slice(5).trim();
    if (!payload || payload === "{}") continue;
    try {
      onEvent(JSON.parse(payload) as RunnerEvent);
    } catch {
      // A malformed frame is non-fatal — skip it and keep streaming.
    }
  }
}
