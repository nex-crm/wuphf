/**
 * Hook for simulated streaming text reveal.
 *
 * Reveals text word-by-word using setInterval, giving an
 * animated "typing" appearance. When real SSE streaming is available,
 * this can be replaced with actual token-level streaming.
 *
 * Features:
 *  - Word-level chunking at configurable interval (default 30ms)
 *  - `isStreaming` flag for trailing cursor display
 *  - `stopStreaming()` immediately shows full text
 */

import { useState, useEffect, useRef, useCallback } from "react";

// ── Types ────────────────────────────────────────────────────────────

export interface UseStreamingReturn {
  /** The currently visible portion of the text */
  text: string;
  /** Whether text is still being revealed */
  isStreaming: boolean;
  /**
   * Start streaming a text string word-by-word.
   * @param fullText The complete text to reveal
   * @param chunkMs Interval between word reveals in ms (default 30)
   */
  startStreaming: (fullText: string, chunkMs?: number) => void;
  /** Immediately show all remaining text and stop streaming */
  stopStreaming: () => void;
}

// ── Helpers ──────────────────────────────────────────────────────────

/**
 * Split text into word-level chunks, preserving whitespace.
 * Each chunk is a word plus its trailing whitespace.
 * E.g. "hello world" → ["hello ", "world"]
 */
export function splitIntoWordChunks(text: string): string[] {
  const chunks: string[] = [];
  const regex = /\S+\s*/g;
  let match: RegExpExecArray | null;
  while ((match = regex.exec(text)) !== null) {
    chunks.push(match[0]);
  }
  // Handle leading whitespace
  if (chunks.length === 0 && text.length > 0) {
    chunks.push(text);
  }
  return chunks;
}

// ── Hook ─────────────────────────────────────────────────────────────

export function useStreaming(): UseStreamingReturn {
  const [text, setText] = useState("");
  const [isStreaming, setIsStreaming] = useState(false);

  const fullTextRef = useRef("");
  const chunksRef = useRef<string[]>([]);
  const chunkIndexRef = useRef(0);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const cleanup = useCallback(() => {
    if (intervalRef.current !== null) {
      clearInterval(intervalRef.current);
      intervalRef.current = null;
    }
  }, []);

  const stopStreaming = useCallback(() => {
    cleanup();
    setText(fullTextRef.current);
    setIsStreaming(false);
  }, [cleanup]);

  const startStreaming = useCallback(
    (fullText: string, chunkMs: number = 30) => {
      cleanup();

      fullTextRef.current = fullText;
      const chunks = splitIntoWordChunks(fullText);
      chunksRef.current = chunks;
      chunkIndexRef.current = 0;

      if (fullText.length === 0 || chunks.length === 0) {
        setText(fullText);
        setIsStreaming(false);
        return;
      }

      setText("");
      setIsStreaming(true);

      intervalRef.current = setInterval(() => {
        chunkIndexRef.current++;
        const revealed = chunksRef.current
          .slice(0, chunkIndexRef.current)
          .join("");
        const done = chunkIndexRef.current >= chunksRef.current.length;

        setText(revealed);

        if (done) {
          setIsStreaming(false);
          cleanup();
        }
      }, chunkMs);
    },
    [cleanup],
  );

  // Cleanup on unmount
  useEffect(() => {
    return cleanup;
  }, [cleanup]);

  return { text, isStreaming, startStreaming, stopStreaming };
}

// ── Standalone async generator for non-React contexts ────────────────

export interface StreamingOptions {
  /** Interval between word reveals in ms. Default: 30 */
  chunkMs?: number;
}

/**
 * Async generator that yields progressively revealed text, word by word.
 */
export async function* streamText(
  fullText: string,
  options: StreamingOptions = {},
  signal?: AbortSignal,
): AsyncGenerator<{ text: string; isStreaming: boolean }> {
  const { chunkMs = 30 } = options;
  const chunks = splitIntoWordChunks(fullText);

  if (chunks.length === 0) return;

  for (let i = 0; i < chunks.length; i++) {
    if (signal?.aborted) {
      yield { text: fullText, isStreaming: false };
      return;
    }

    const revealed = chunks.slice(0, i + 1).join("");
    const done = i === chunks.length - 1;

    yield { text: revealed, isStreaming: !done };

    if (!done) {
      await new Promise((resolve) => setTimeout(resolve, chunkMs));
    }
  }
}
