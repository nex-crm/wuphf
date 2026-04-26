import { useEffect, useRef, useState } from "react";

import { sseURL } from "../api/client";

export interface StreamLine {
  id: number;
  data: string;
  parsed?: Record<string, unknown>;
}

export function useAgentStream(slug: string | null) {
  const [lines, setLines] = useState<StreamLine[]>([]);
  const [connected, setConnected] = useState(false);
  const counterRef = useRef(0);
  const sourceRef = useRef<EventSource | null>(null);

  useEffect(() => {
    if (!slug) {
      setLines([]);
      setConnected(false);
      return;
    }

    const url = sseURL(`/agent-stream/${encodeURIComponent(slug)}`);
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

      setLines((prev) => {
        // Coalesce consecutive raw text chunks into a single line.
        // The local-LLM path emits one chunk per ~5ms while the model
        // streams; without coalescing each chunk renders as its own
        // <div>, producing the "every word on its own line" wall the
        // user reported. We only merge when BOTH the previous AND
        // current event are raw (no parsed JSON) — structured events
        // (mcp_tool_event, item.completed, etc.) keep their own row.
        const isRaw = parsed === undefined;
        const last = prev[prev.length - 1];
        if (isRaw && last && last.parsed === undefined) {
          const merged: StreamLine = {
            id: last.id,
            data: last.data + e.data,
            parsed: undefined,
          };
          const next = [...prev.slice(0, -1), merged];
          return next.length > 50 ? next.slice(-50) : next;
        }
        const line: StreamLine = {
          id: ++counterRef.current,
          data: e.data,
          parsed,
        };
        const next = [...prev, line];
        return next.length > 50 ? next.slice(-50) : next;
      });

      // Auto-stop on idle
      if (parsed?.status === "idle" && counterRef.current > 1) {
        source.close();
        setConnected(false);
      }
    };

    source.onerror = () => {
      source.close();
      setConnected(false);
    };

    return () => {
      source.close();
      sourceRef.current = null;
      setConnected(false);
    };
  }, [slug]);

  return { lines, connected };
}
