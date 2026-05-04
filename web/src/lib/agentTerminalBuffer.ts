const DEFAULT_MAX_PENDING_CHARS = 80_000;
const TRUNCATED_NOTICE =
  "\r\n[older output dropped while the terminal caught up]\r\n";

export type TerminalWriter = (text: string) => void;
export type TerminalScheduler = (callback: () => void) => () => void;

export interface TerminalWriteBufferOptions {
  maxPendingChars?: number;
  schedule?: TerminalScheduler;
}

export interface TerminalWriteBuffer {
  enqueue: (text: string) => void;
  flush: () => void;
  dispose: () => void;
}

export function formatAgentTerminalChunk(line: string): string | null {
  if (line === "[connected]") return null;
  if (line.length === 0) return null;
  return line.includes("\n") || line.includes("\r") ? line : `${line}\r\n`;
}

export function createTerminalWriteBuffer(
  write: TerminalWriter,
  options: TerminalWriteBufferOptions = {},
): TerminalWriteBuffer {
  const maxPendingChars = options.maxPendingChars ?? DEFAULT_MAX_PENDING_CHARS;
  const schedule = options.schedule ?? animationFrameScheduler;
  let pending = "";
  let cancelScheduled: (() => void) | null = null;
  let disposed = false;

  function flush() {
    if (disposed) return;
    cancelScheduled = null;
    if (!pending) return;
    const next = pending;
    pending = "";
    write(next);
  }

  function enqueue(text: string) {
    if (disposed || !text) return;
    pending += text;
    if (pending.length > maxPendingChars) {
      pending = TRUNCATED_NOTICE + pending.slice(-maxPendingChars);
    }
    if (!cancelScheduled) {
      cancelScheduled = schedule(flush);
    }
  }

  function dispose() {
    disposed = true;
    pending = "";
    cancelScheduled?.();
    cancelScheduled = null;
  }

  return { enqueue, flush, dispose };
}

function animationFrameScheduler(callback: () => void): () => void {
  if (typeof window === "undefined" || !window.requestAnimationFrame) {
    const id = globalThis.setTimeout(callback, 16);
    return () => globalThis.clearTimeout(id);
  }
  const id = window.requestAnimationFrame(callback);
  return () => window.cancelAnimationFrame(id);
}
