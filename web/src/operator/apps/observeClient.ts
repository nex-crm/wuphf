// observeClient.ts — the OBSERVE capture during a demo call. POST the broker's
// /observe/browser SSE endpoint, which runs the cua observer (reads the real
// frontmost window's component tree + visible text via cua-driver), and hand
// each snapshot / navigate event to the caller. The call accumulates these and
// folds them into the build handoff so the AI reads the actual page, not just
// screenshots. On a 503 (no observer on the host) it throws OBSERVE_UNAVAILABLE
// and the call simply proceeds without the structured capture. See
// docs/specs/operator-cua-migration.md §7 (C5).

import { postStream } from "../../api/client";
import { readEventStream } from "../exec/sse";

// One captured screen — mirrors runner/cua_observe.py's snapshot emit.
export interface ObserveSnapshot {
  type: "snapshot";
  tick: number;
  app: string;
  title: string;
  components: Array<{ role: string; label: string }>;
  text_excerpt?: string;
}

// A screen change inferred from a snapshot diff.
export interface ObserveNavigate {
  type: "event";
  tick: number;
  app: string;
  title: string;
}

// One distinct screen the operator demonstrated, reduced from the raw snapshot
// stream — the unit the build handoff reasons over.
export interface ObservedScreen {
  app: string;
  title: string;
  components: Array<{ role: string; label: string }>;
  text?: string;
}

const MAX_SCREENS = 10;
const MAX_COMPONENTS_PER_SCREEN = 30;
const MAX_TEXT = 400;

// Reduce the raw per-tick snapshots to the distinct screens in first-seen order
// (the workflow sequence), keeping the richest snapshot of each and bounding the
// size so the handoff prompt stays reasonable.
export function reduceObserved(snapshots: ObserveSnapshot[]): ObservedScreen[] {
  const order: string[] = [];
  const richest = new Map<string, ObserveSnapshot>();
  for (const snap of snapshots) {
    const key = `${snap.app} | ${snap.title}`;
    const prev = richest.get(key);
    if (!prev) order.push(key);
    if (!prev || snap.components.length > prev.components.length) {
      richest.set(key, snap);
    }
  }
  return order.slice(0, MAX_SCREENS).map((key) => {
    const snap = richest.get(key) as ObserveSnapshot;
    return {
      app: snap.app,
      title: snap.title,
      components: snap.components.slice(0, MAX_COMPONENTS_PER_SCREEN),
      ...(snap.text_excerpt
        ? { text: snap.text_excerpt.slice(0, MAX_TEXT) }
        : {}),
    };
  });
}

export const OBSERVE_UNAVAILABLE = "observe-unavailable";

export interface RunObserveOptions {
  signal?: AbortSignal;
  intervalSec?: number;
  onSnapshot?: (snapshot: ObserveSnapshot) => void;
  onNavigate?: (navigate: ObserveNavigate) => void;
}

// Run the observe capture loop, calling the callbacks for each event until the
// stream ends (call over) or the signal aborts. Rejects with OBSERVE_UNAVAILABLE
// when the host has no observer, so the caller can carry on without it.
export async function runObserve(opts: RunObserveOptions): Promise<void> {
  const path =
    opts.intervalSec && opts.intervalSec >= 1
      ? `/observe/browser?interval=${opts.intervalSec}`
      : "/observe/browser";
  const res = await postStream(path, {}, { signal: opts.signal });
  if (res.status === 503) {
    throw new Error(OBSERVE_UNAVAILABLE);
  }
  if (!(res.ok && res.body)) {
    throw new Error(`observe failed: ${res.status}`);
  }
  await readEventStream(res, (data) => {
    const evt = data as { type?: string };
    if (evt.type === "snapshot") {
      opts.onSnapshot?.(data as ObserveSnapshot);
    } else if (evt.type === "event") {
      opts.onNavigate?.(data as ObserveNavigate);
    }
  });
}
