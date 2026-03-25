/**
 * Hook for cancellable async operations with AbortController pattern.
 *
 * Provides:
 *  - `signal` — pass to fetch / async operations
 *  - `cancel()` — aborts the current controller
 *  - `reset()` — creates a fresh AbortController
 *  - `isCancelled` — true after cancel(), cleared on reset()
 */

import { useState, useRef, useCallback } from "react";

// ── Hook ─────────────────────────────────────────────────────────────

export interface UseCancellableReturn {
  signal: AbortSignal;
  cancel: () => void;
  reset: () => void;
  isCancelled: boolean;
}

export function useCancellable(): UseCancellableReturn {
  const controllerRef = useRef(new AbortController());
  const [isCancelled, setIsCancelled] = useState(false);

  const cancel = useCallback(() => {
    controllerRef.current.abort();
    setIsCancelled(true);
  }, []);

  const reset = useCallback(() => {
    controllerRef.current = new AbortController();
    setIsCancelled(false);
  }, []);

  return {
    signal: controllerRef.current.signal,
    cancel,
    reset,
    isCancelled,
  };
}

// ── Ctrl+C handler (standalone, for app.tsx) ─────────────────────────

/**
 * Creates a Ctrl+C handler with the following behavior:
 *  - During loading: first Ctrl+C cancels the operation
 *  - When idle: first Ctrl+C records timestamp and returns "pending_exit",
 *    second Ctrl+C within `windowMs` exits the process
 *
 * Returns "cancelled" | "exit" | "pending_exit".
 */
export interface CtrlCHandler {
  handle: () => "cancelled" | "exit" | "pending_exit";
}

export function createCtrlCHandler(
  cancelFn: () => boolean,
  exitFn: () => void = () => process.exit(0),
  windowMs: number = 1000,
): CtrlCHandler {
  let lastPressTime = 0;

  return {
    handle() {
      const now = Date.now();

      // Try to cancel a running operation first
      const wasCancelled = cancelFn();
      if (wasCancelled) {
        lastPressTime = now;
        return "cancelled";
      }

      // Nothing to cancel — double-press check
      if (now - lastPressTime <= windowMs) {
        exitFn();
        return "exit";
      }

      // First idle press — record time, show hint
      lastPressTime = now;
      return "pending_exit";
    },
  };
}
