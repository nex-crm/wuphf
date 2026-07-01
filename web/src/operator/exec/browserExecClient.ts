// browserExecClient.ts — the LIVE EXECUTE path: POST a goal to the broker's
// /execute/browser SSE endpoint, which spawns the cua runner (OpenAI plans,
// cua-driver drives the real browser) and streams one JSON event per step. We
// parse the SSE frames and hand each event to the caller. On a 503 (no key or
// no runner on the host) we throw EXEC_UNAVAILABLE so the modal falls back to
// the scripted mock. See docs/specs/operator-cua-migration.md.

import { post, postStream } from "../../api/client";
import { readEventStream } from "./sse";

// One recorded action, keyed by the element's STABLE identity (role + label),
// so a later run can match + replay it. Mirrors cua_exec.py's trajectory step.
export interface TrajectoryStep {
  action: string;
  role?: string;
  label?: string;
  text?: string;
  key?: string;
}

export interface Trajectory {
  goal: string;
  app: string;
  steps: TrajectoryStep[];
}

// One event from the runner — mirrors runner/cua_exec.py's emit() shapes.
export interface RunnerEvent {
  type:
    | "status"
    | "action"
    | "done"
    | "error"
    | "trajectory"
    | "run"
    | "approval_request";
  // status: running | thinking | replaying | healing
  status?: string;
  detail?: string;
  // action:
  label?: string;
  reasoning?: string;
  tool?: string;
  refused?: boolean;
  replayed?: boolean;
  healed?: boolean;
  skipped?: boolean;
  // done / error:
  result?: string;
  message?: string;
  // trajectory (emitted on finish): the recorded/healed steps to persist.
  goal?: string;
  app?: string;
  steps?: TrajectoryStep[];
  // run (first frame): the id used to POST a send-approval back to this run.
  run_id?: string;
}

// Forward the operator's send decision to a paused run (the runner is blocked on
// its stdin waiting for an external send). "approve" sends; "deny" skips it.
export async function approveExec(
  runId: string,
  decision: "approve" | "deny",
): Promise<void> {
  await post("/execute/approve", { run_id: runId, decision });
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

export interface RunBrowserReplayOptions {
  trajectory: Trajectory;
  windowId?: number;
  signal?: AbortSignal;
  onEvent: (event: RunnerEvent) => void;
}

// Replay a recorded trajectory deterministically (the runner matches each step's
// element by role+label and executes it, healing only the steps whose element is
// gone). Same event stream as a live run, but `replayed`/`healed` flags mark each
// action. Rejects with EXEC_UNAVAILABLE when the host can't run it.
export async function runBrowserReplay(
  opts: RunBrowserReplayOptions,
): Promise<void> {
  const res = await postStream(
    "/execute/replay",
    { trajectory: opts.trajectory, window_id: opts.windowId },
    { signal: opts.signal },
  );
  if (res.status === 503) {
    throw new Error(EXEC_UNAVAILABLE);
  }
  if (!(res.ok && res.body)) {
    throw new Error(`browser replay failed: ${res.status}`);
  }
  await readEventStream(res, (data) => opts.onEvent(data as RunnerEvent));
}
