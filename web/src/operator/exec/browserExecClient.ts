// browserExecClient.ts — the LIVE EXECUTE path: POST a goal to the broker's
// /execute/browser SSE endpoint, which spawns the cua runner (OpenAI plans,
// cua-driver drives the real browser) and streams one JSON event per step. We
// parse the SSE frames and hand each event to the caller. On a 503 (no key or
// no runner on the host) we throw EXEC_UNAVAILABLE so the modal falls back to
// the scripted mock. See docs/specs/operator-cua-migration.md.

import { postStream } from "../../api/client";
import { readEventStream } from "./sse";

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
  await readEventStream(res, (data) => opts.onEvent(data as RunnerEvent));
}
