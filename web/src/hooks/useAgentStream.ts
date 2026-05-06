import { useEffect, useRef, useState } from "react";

import { sseURL } from "../api/client";

export interface StreamLine {
  id: number;
  data: string;
  parsed?: Record<string, unknown>;
}

const MAX_LINES = 50;

// appendStreamLine is the pure update used by useAgentStream's
// onmessage handler. Pulled out so it's directly unit-testable
// (the hook itself wraps EventSource, which is awkward to mock).
//
// Coalescing: consecutive RAW chunks (no parsed JSON) merge into a
// single StreamLine. Without this, the local-LLM path renders one
// chunk per row in the Live Output panel — at ~5ms per chunk,
// that's a column of single tokens that looks like JSON spew. A
// structured event (parsed != undefined) always starts a new line.
export function appendStreamLine(
  prev: StreamLine[],
  eventData: string,
  parsed: Record<string, unknown> | undefined,
  nextId: number,
): { lines: StreamLine[]; usedId: boolean } {
  const isRaw = parsed === undefined;
  const last = prev[prev.length - 1];
  if (isRaw && last && last.parsed === undefined) {
    const merged: StreamLine = {
      id: last.id,
      data: last.data + eventData,
      parsed: undefined,
    };
    const next = [...prev.slice(0, -1), merged];
    return {
      lines: next.length > MAX_LINES ? next.slice(-MAX_LINES) : next,
      usedId: false,
    };
  }
  const line: StreamLine = { id: nextId, data: eventData, parsed };
  const next = [...prev, line];
  return {
    lines: next.length > MAX_LINES ? next.slice(-MAX_LINES) : next,
    usedId: true,
  };
}

// agentStreamURL builds the SSE URL for an agent's live output, optionally
// scoped to a single task. We must call sseURL on the bare path and append
// `task=` AFTER, because sseURL appends `?token=…` unconditionally; if the
// path already had `?task=…`, the result would be `?task=…?token=…` and the
// query parser would fold the token into the task value, breaking auth.
export function agentStreamURL(slug: string, taskId: string | null): string {
  const base = sseURL(`/agent-stream/${encodeURIComponent(slug)}`);
  const trimmed = taskId?.trim();
  if (!trimmed) return base;
  const sep = base.includes("?") ? "&" : "?";
  return `${base}${sep}task=${encodeURIComponent(trimmed)}`;
}

export function useAgentStream(
  slug: string | null,
  taskId: string | null = null,
) {
  const [lines, setLines] = useState<StreamLine[]>([]);
  const [connected, setConnected] = useState(false);
  const counterRef = useRef(0);
  const linesRef = useRef<StreamLine[]>([]);
  const sourceRef = useRef<EventSource | null>(null);

  useEffect(() => {
    if (!slug) {
      linesRef.current = [];
      counterRef.current = 0;
      setLines([]);
      setConnected(false);
      return;
    }

    linesRef.current = [];
    counterRef.current = 0;
    setLines([]);

    const url = agentStreamURL(slug, taskId);
    const source = new EventSource(url);
    sourceRef.current = source;

    source.onopen = () => setConnected(true);

    source.onmessage = (e) => {
      let parsed: Record<string, unknown> | undefined;
      try {
        parsed = JSON.parse(e.data);
      } catch {
        // raw text line
      }

      // Compute the next state outside the setLines updater. React 18+
      // Strict Mode runs updaters twice in dev and the scheduler can
      // replay them on bail-outs; mutating refs inside the updater would
      // double-bump counters. linesRef mirrors state so we still see the
      // latest snapshot here (the closure's `lines` is stale across many
      // SSE events without re-running the effect).
      const nextId = counterRef.current + 1;
      const { lines: nextLines, usedId } = appendStreamLine(
        linesRef.current,
        e.data,
        parsed,
        nextId,
      );
      linesRef.current = nextLines;
      if (usedId) counterRef.current = nextId;
      setLines(nextLines);

      // Auto-stop on idle
      if (parsed?.status === "idle" && counterRef.current > 1) {
        source.close();
        setConnected(false);
      }
    };

    source.onerror = () => {
      // Don't hard-close on transient errors — EventSource auto-reconnects
      // with back-off and Last-Event-ID. Only flip the indicator so the
      // UI shows "Disconnected" until the browser reopens the stream.
      // The useEffect cleanup closes on slug/taskId change or unmount.
      setConnected(false);
    };

    return () => {
      source.close();
      sourceRef.current = null;
      setConnected(false);
    };
  }, [slug, taskId]);

  return { lines, connected };
}
