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

      const line: StreamLine = {
        id: ++counterRef.current,
        data: e.data,
        parsed,
      };

      setLines((prev) => {
        const next = [...prev, line];
        // Keep max 50 lines
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
