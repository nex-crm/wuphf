import { useCallback, useEffect, useRef } from "react";
import { create } from "zustand";

import { type StoredActivitySnapshot, useAppStore } from "../stores/app";

// --- Timer constants ---

const HOVER_INTENT_MS = 300;
const CLOSE_GRACE_MS = 80;
// Long-press cancels on touchmove because a scroll gesture starting on a row
// should not open a peek mid-scroll.
const LONG_PRESS_MS = 500;

// Stable empty array returned when a slug has no history yet, so the selector
// never allocates a fresh [] on every render (which would trigger an infinite
// re-render cycle via React 19's useSyncExternalStore snapshot check).
const EMPTY_HISTORY: StoredActivitySnapshot[] = [];

// --- Single-instance open store (NOT part of useAppStore) ---

interface PeekOpenState {
  openSlug: string | null;
  setOpenSlug: (slug: string | null) => void;
}

export const usePeekOpenStore = create<PeekOpenState>((set) => ({
  openSlug: null,
  setOpenSlug: (slug) => set({ openSlug: slug }),
}));

// --- Public types ---

export interface PeekOptions {
  /** Override hover-intent delay in ms (default: 300). Tests use small values for speed. */
  hoverIntentMs?: number;
  /** Override close-grace delay in ms (default: 80). */
  closeGraceMs?: number;
  /** Override long-press delay in ms (default: 500). */
  longPressMs?: number;
}

export interface PeekState {
  isOpen: boolean;
  current: StoredActivitySnapshot | undefined;
  history: StoredActivitySnapshot[];
  open: () => void;
  close: () => void;
  toggle: () => void;
  hoverHandlers: {
    onMouseEnter: () => void;
    onMouseLeave: () => void;
  };
  longPressHandlers: {
    onTouchStart: () => void;
    onTouchEnd: () => void;
    onTouchCancel: () => void;
    onTouchMove: () => void;
  };
}

// --- Internal constants re-exported for tests ---

export const __internal = {
  HOVER_INTENT_MS,
  CLOSE_GRACE_MS,
  LONG_PRESS_MS,
  usePeekOpenStore,
} as const;

// --- Hook implementations ---

/**
 * Returns true only when the given slug is the currently open peek.
 * Used by AgentEventPill's chevron for aria-expanded without subscribing
 * to the full PeekState.
 */
export function usePeekIsOpen(slug: string): boolean {
  return usePeekOpenStore((s) => s.openSlug === slug);
}

/**
 * Full peek state for a single agent row. Attach hoverHandlers onto the row
 * container element and longPressHandlers onto the same container for touch.
 * Use open/close/toggle for keyboard activation.
 */
export function useAgentEventPeek(
  slug: string,
  options?: PeekOptions,
): PeekState {
  const hoverIntentMs = options?.hoverIntentMs ?? HOVER_INTENT_MS;
  const closeGraceMs = options?.closeGraceMs ?? CLOSE_GRACE_MS;
  const longPressMs = options?.longPressMs ?? LONG_PRESS_MS;

  const setOpenSlug = usePeekOpenStore((s) => s.setOpenSlug);
  const isOpen = usePeekOpenStore((s) => s.openSlug === slug);

  // Two separate selectors avoid creating a new object reference on every render
  // (object identity would always be fresh and trigger an infinite re-render loop
  // with useSyncExternalStore in React 19).
  const current = useAppStore((s) => s.agentActivitySnapshots[slug]);
  // Read the array reference directly — Zustand's store produces a stable
  // reference when the slice hasn't changed, so the selector never returns a
  // freshly-allocated array on every render (which would cause an infinite
  // re-render loop via useSyncExternalStore in React 19).
  const historyOrUndef = useAppStore((s) => s.agentActivityHistory[slug]);
  const history: StoredActivitySnapshot[] = historyOrUndef ?? EMPTY_HISTORY;

  // Timer refs — never stored in state to avoid triggering re-renders
  // on every schedule/cancel cycle.
  const hoverOpenTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const closeGraceTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const longPressTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const clearHoverOpenTimer = useCallback(() => {
    if (hoverOpenTimerRef.current !== null) {
      clearTimeout(hoverOpenTimerRef.current);
      hoverOpenTimerRef.current = null;
    }
  }, []);

  const clearCloseGraceTimer = useCallback(() => {
    if (closeGraceTimerRef.current !== null) {
      clearTimeout(closeGraceTimerRef.current);
      closeGraceTimerRef.current = null;
    }
  }, []);

  const clearLongPressTimer = useCallback(() => {
    if (longPressTimerRef.current !== null) {
      clearTimeout(longPressTimerRef.current);
      longPressTimerRef.current = null;
    }
  }, []);

  // Flush all pending timers when slug changes or on unmount. `slug` is read
  // here so biome's useExhaustiveDependencies rule sees it as a real
  // dependency — the cleanup body itself doesn't need it, but the slug-change
  // re-run is the whole point: timers from the previous slug get cleared
  // before any new ones bind.
  useEffect(() => {
    void slug;
    return () => {
      clearHoverOpenTimer();
      clearCloseGraceTimer();
      clearLongPressTimer();
    };
  }, [slug, clearHoverOpenTimer, clearCloseGraceTimer, clearLongPressTimer]);

  const open = useCallback(() => {
    setOpenSlug(slug);
  }, [slug, setOpenSlug]);

  const close = useCallback(() => {
    setOpenSlug(null);
  }, [setOpenSlug]);

  // We read isOpen from the store inside toggle via a fresh selector rather
  // than from the captured closure so the flip is always based on latest state.
  const toggle = useCallback(() => {
    const currentlyOpen = usePeekOpenStore.getState().openSlug === slug;
    if (currentlyOpen) {
      setOpenSlug(null);
    } else {
      setOpenSlug(slug);
    }
  }, [slug, setOpenSlug]);

  const onMouseEnter = useCallback(() => {
    // Cancel any in-flight close-grace so re-entering keeps the peek open.
    clearCloseGraceTimer();

    // Skip re-scheduling if we are already open (cursor returned to already-open row).
    const alreadyOpen = usePeekOpenStore.getState().openSlug === slug;
    if (alreadyOpen) return;

    // Start hover-intent timer; only opens if cursor stays for the full duration.
    clearHoverOpenTimer();
    hoverOpenTimerRef.current = setTimeout(() => {
      hoverOpenTimerRef.current = null;
      setOpenSlug(slug);
    }, hoverIntentMs);
  }, [
    slug,
    hoverIntentMs,
    setOpenSlug,
    clearHoverOpenTimer,
    clearCloseGraceTimer,
  ]);

  const onMouseLeave = useCallback(() => {
    // If the intent timer hasn't fired yet, cancel it — cursor left too soon.
    clearHoverOpenTimer();

    const alreadyOpen = usePeekOpenStore.getState().openSlug === slug;
    if (!alreadyOpen) return;

    // Start close-grace; re-entering before it fires cancels it (onMouseEnter).
    clearCloseGraceTimer();
    closeGraceTimerRef.current = setTimeout(() => {
      closeGraceTimerRef.current = null;
      // Only close if this slug is still the open one (another slug may have
      // stolen focus in the grace window).
      if (usePeekOpenStore.getState().openSlug === slug) {
        setOpenSlug(null);
      }
    }, closeGraceMs);
  }, [
    slug,
    closeGraceMs,
    setOpenSlug,
    clearHoverOpenTimer,
    clearCloseGraceTimer,
  ]);

  const onTouchStart = useCallback(() => {
    clearLongPressTimer();
    longPressTimerRef.current = setTimeout(() => {
      longPressTimerRef.current = null;
      setOpenSlug(slug);
    }, longPressMs);
  }, [slug, longPressMs, setOpenSlug, clearLongPressTimer]);

  // touchend and touchcancel cancel the timer but do NOT close the peek
  // if it has already opened — touch users dismiss by tapping outside.
  const onTouchEnd = useCallback(() => {
    clearLongPressTimer();
  }, [clearLongPressTimer]);

  const onTouchCancel = useCallback(() => {
    clearLongPressTimer();
  }, [clearLongPressTimer]);

  // A scroll attempt during the long-press window should not open a peek.
  const onTouchMove = useCallback(() => {
    clearLongPressTimer();
  }, [clearLongPressTimer]);

  return {
    isOpen,
    current,
    history,
    open,
    close,
    toggle,
    hoverHandlers: { onMouseEnter, onMouseLeave },
    longPressHandlers: { onTouchStart, onTouchEnd, onTouchCancel, onTouchMove },
  };
}
