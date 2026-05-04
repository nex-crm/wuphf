import { useEffect, useRef, useState } from "react";

import { subscribeAgentStream } from "../lib/agentStreamClient";

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

export function useAgentStream(slug: string | null) {
  const [lines, setLines] = useState<StreamLine[]>([]);
  const [connected, setConnected] = useState(false);
  const counterRef = useRef(0);
  const linesRef = useRef<StreamLine[]>([]);

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
    const subscription = subscribeAgentStream(slug, {
      onOpen: () => setConnected(true),
      onLine: (eventData) => {
        let parsed: Record<string, unknown> | undefined;
        try {
          parsed = JSON.parse(eventData);
        } catch {
          // raw text line
        }

        const { lines: nextLines, usedId } = appendStreamLine(
          linesRef.current,
          eventData,
          parsed,
          counterRef.current + 1,
        );
        linesRef.current = nextLines;
        if (usedId) {
          counterRef.current += 1;
        }
        setLines(nextLines);

        // Auto-stop on idle
        if (parsed?.status === "idle" && counterRef.current > 1) {
          subscription.close();
          setConnected(false);
        }
      },
      onError: () => setConnected(false),
      onClose: () => setConnected(false),
    });

    return () => {
      subscription.close();
      setConnected(false);
    };
  }, [slug]);

  return { lines, connected };
}
