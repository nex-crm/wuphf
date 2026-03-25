/**
 * Shared TUI context for passing store state to all views.
 * This file is intentionally separate from app.tsx to avoid circular imports:
 * app.tsx imports register-views.tsx (side-effect), and register-views.tsx
 * needs access to the context. By extracting the context here, both can
 * import it without circular dependency.
 */

import React, { createContext, useContext } from "react";
import type { TuiState, Dispatch, Store } from "./store.js";

export interface TuiContextValue {
  state: TuiState;
  dispatch: Dispatch;
  store: Store;
  /** Cancel the current loading operation (Ctrl+C). No-op when not loading. */
  cancel: () => void;
}

export const TuiContext = createContext<TuiContextValue | null>(null);

/**
 * Hook for views to read the global TUI state (mode, input, loading, etc.)
 * and dispatch actions. Returns null if used outside the App provider (e.g. in tests).
 */
export function useTuiState(): TuiContextValue | null {
  return useContext(TuiContext);
}
