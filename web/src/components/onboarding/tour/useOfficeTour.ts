/**
 * useOfficeTour — visibility controller for the guided office tour.
 *
 * Behavior (spec section 4):
 *   - Auto-opens the tour exactly ONCE per browser, gated on the office
 *     actually being loaded and onboarded. The "seen it" flag is the
 *     localStorage key `wuphf.office-tour-done`.
 *   - Re-openable on demand (a Help entry, a keyboard shortcut) via a
 *     window CustomEvent `wuphf:show-office-tour`. `requestShowOfficeTour()`
 *     is the dispatcher helper callers use so the event name lives in one
 *     place.
 *   - `markDone()` persists the flag and closes; `skip()` is the same thing
 *     under a name that reads correctly at call sites.
 *
 * All window / localStorage access is guarded behind
 * `typeof window !== "undefined"` so the hook is safe under SSR / tests with
 * no DOM. The initial auto-open decision runs in `useLayoutEffect` so it
 * lands before the browser paints, avoiding a flash of office-without-tour.
 */

import { useCallback, useEffect, useLayoutEffect, useState } from "react";

/** localStorage key recording that this browser has seen the tour. */
export const OFFICE_TOUR_DONE_KEY = "wuphf.office-tour-done";

/** Window event that re-opens the tour (replay). */
export const OFFICE_TOUR_SHOW_EVENT = "wuphf:show-office-tour";

/**
 * Dispatch the replay event. Safe to call from anywhere (Help menu, command
 * palette, keyboard shortcut). No-op when there is no window.
 */
export function requestShowOfficeTour(): void {
  if (typeof window === "undefined") return;
  window.dispatchEvent(new CustomEvent(OFFICE_TOUR_SHOW_EVENT));
}

/** Whether this browser has already completed or skipped the tour. */
function hasSeenTour(): boolean {
  if (typeof window === "undefined") return true;
  try {
    return window.localStorage.getItem(OFFICE_TOUR_DONE_KEY) === "1";
  } catch {
    // Private mode / storage disabled: treat as seen so we never trap the
    // user in an un-dismissable auto-open loop.
    return true;
  }
}

/** Persist the "seen it" flag. Swallows storage errors (private mode). */
function persistSeen(): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(OFFICE_TOUR_DONE_KEY, "1");
  } catch {
    // Best-effort: if storage is unavailable the worst case is the tour
    // auto-opens again next load, which is acceptable.
  }
}

export interface UseOfficeTourResult {
  /** Whether the tour should currently be shown. */
  open: boolean;
  /**
   * True when this open is a REPLAY (the user reopened the tour from Help on an
   * already-onboarded office), false on the first-run auto-open. The caller
   * uses this to decide placement: first-run renders the tour as onboarding's
   * final full-screen act (no office behind it, so the flow reads as one arc);
   * replay overlays it on the live office Shell.
   */
  replay: boolean;
  /** Force the tour open (replay), without touching the done flag. */
  show: () => void;
  /** Mark the tour done (persist flag) and close it. */
  markDone: () => void;
  /** Skip the tour: identical to markDone, named for skip call sites. */
  skip: () => void;
}

/**
 * @param enabled the caller passes `onboarded` — the tour only auto-opens
 *   once the office is loaded and onboarded. Passing false keeps it closed
 *   and defers the one-shot auto-open until the office is ready.
 */
export function useOfficeTour(enabled: boolean): UseOfficeTourResult {
  const [open, setOpen] = useState(false);
  const [replay, setReplay] = useState(false);

  const show = useCallback(() => {
    setReplay(true);
    setOpen(true);
  }, []);

  const close = useCallback(() => {
    persistSeen();
    setOpen(false);
    setReplay(false);
  }, []);

  // One-shot auto-open before paint. Runs whenever `enabled` flips true; the
  // localStorage gate inside ensures it opens at most once per browser even
  // across remounts. This is the first-run open (replay stays false), so the
  // caller renders the tour as onboarding's final act. useLayoutEffect (not
  // useEffect) so the decision lands before the office paints without the tour.
  useLayoutEffect(() => {
    if (!enabled) return;
    if (hasSeenTour()) return;
    setReplay(false);
    setOpen(true);
  }, [enabled]);

  // Replay listener. Bound for the lifetime of the hook, independent of the
  // auto-open gate, so Help → "Replay the office tour" works at any time. This
  // is a replay open (the office Shell is already mounted), so the caller
  // overlays the tour rather than replacing the office.
  useEffect(() => {
    if (typeof window === "undefined") return;
    const handler = () => {
      setReplay(true);
      setOpen(true);
    };
    window.addEventListener(OFFICE_TOUR_SHOW_EVENT, handler);
    return () => window.removeEventListener(OFFICE_TOUR_SHOW_EVENT, handler);
  }, []);

  return { open, replay, show, markDone: close, skip: close };
}
