import { useCallback, useEffect, useRef, useState } from "react";

export type ResizeEdge = "right" | "left";

export interface UseResizablePaneOptions {
  /** localStorage key used to persist the width across reloads. */
  storageKey: string;
  /** Default width in CSS pixels when no persisted value exists. */
  defaultWidth: number;
  /** Minimum width in CSS pixels the pane is allowed to be dragged to. */
  minWidth: number;
  /** Maximum width in CSS pixels the pane is allowed to be dragged to. */
  maxWidth: number;
  /**
   * Which edge of the pane the resize handle lives on. "right" widens by
   * dragging away from the pane's left origin (e.g. left sidebar). "left"
   * widens by dragging towards the pane's right origin (e.g. right thread
   * panel that anchors to the viewport edge).
   */
  edge: ResizeEdge;
}

interface ResizablePane {
  /** Current width in CSS pixels. */
  width: number;
  /** Pointer-down handler for the resize handle element. */
  onPointerDown: (event: React.PointerEvent<HTMLElement>) => void;
  /** True while the user is actively dragging the handle. */
  isResizing: boolean;
  /** Reset to the default width (handle double-click target). */
  reset: () => void;
  /**
   * Apply a signed pixel delta from the user's perspective: positive widens
   * the pane, negative narrows it. The hook resolves the edge orientation
   * internally so callers don't need to care. ±Infinity snaps to the
   * configured bounds (used by Home/End on the keyboard handle).
   */
  stepResize: (signedDelta: number) => void;
}

function readStoredWidth(key: string): number | null {
  if (typeof window === "undefined") return null;
  try {
    const raw = window.localStorage.getItem(key);
    if (!raw) return null;
    const n = Number(raw);
    return Number.isFinite(n) && n > 0 ? n : null;
  } catch {
    return null;
  }
}

function persistWidth(key: string, value: number): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(key, String(Math.round(value)));
  } catch {
    /* Safari private mode + sandboxed iframes throw. The width still
       updates for the live session; only persistence is lost. */
  }
}

function clampWidth(width: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, width));
}

/**
 * Drives a draggable resize handle for a side pane (sidebar, thread panel,
 * etc.). The hook owns:
 *   - persisted width clamped to [minWidth, maxWidth]
 *   - a pointer-capture drag loop that updates width as the user drags
 *   - body-level cursor + select-none affordances during the drag
 *   - a reset() helper for double-click-to-restore-default
 *
 * Pass the returned `onPointerDown` to your handle element and apply
 * `width` to the pane via inline style or a CSS variable.
 */
export function useResizablePane({
  storageKey,
  defaultWidth,
  minWidth,
  maxWidth,
  edge,
}: UseResizablePaneOptions): ResizablePane {
  const [width, setWidth] = useState<number>(() => {
    const stored = readStoredWidth(storageKey);
    return clampWidth(stored ?? defaultWidth, minWidth, maxWidth);
  });
  const [isResizing, setIsResizing] = useState(false);
  // Refs keep the drag loop allocation-free and avoid the stale-closure
  // problem when the pointermove handler reads the starting geometry.
  const startXRef = useRef(0);
  const startWidthRef = useRef(width);
  // Held so an unmount mid-drag can tear down the active pointer
  // listeners and pointer capture, even when onUp never fires.
  const cleanupDragRef = useRef<(() => void) | null>(null);

  useEffect(() => {
    persistWidth(storageKey, width);
  }, [storageKey, width]);

  const onPointerDown = useCallback(
    (event: React.PointerEvent<HTMLElement>) => {
      // Left button only — secondary buttons should fall through so the
      // browser's context menu / middle-click semantics keep working.
      if (event.button !== 0) return;
      event.preventDefault();
      startXRef.current = event.clientX;
      startWidthRef.current = width;
      setIsResizing(true);

      const handle = event.currentTarget;
      const pointerId = event.pointerId;
      try {
        handle.setPointerCapture(pointerId);
      } catch {
        /* setPointerCapture can throw if the pointer was already
           captured elsewhere; the move/up listeners still fire. */
      }

      const onMove = (e: PointerEvent) => {
        const delta = e.clientX - startXRef.current;
        const signed = edge === "right" ? delta : -delta;
        const next = clampWidth(
          startWidthRef.current + signed,
          minWidth,
          maxWidth,
        );
        setWidth(next);
      };

      const teardown = () => {
        try {
          handle.releasePointerCapture(pointerId);
        } catch {
          /* Same rationale as setPointerCapture above. */
        }
        window.removeEventListener("pointermove", onMove);
        window.removeEventListener("pointerup", onUp);
        window.removeEventListener("pointercancel", onUp);
        cleanupDragRef.current = null;
      };

      const onUp = () => {
        teardown();
        setIsResizing(false);
      };

      window.addEventListener("pointermove", onMove);
      window.addEventListener("pointerup", onUp);
      window.addEventListener("pointercancel", onUp);
      cleanupDragRef.current = teardown;
    },
    [edge, maxWidth, minWidth, width],
  );

  // Unmount safety: if the component unmounts mid-drag, the user's pointer
  // is still down somewhere on the page. Removing the listeners here keeps
  // them from continuing to call setWidth / setIsResizing on a stale
  // hook instance, and also strips the body class so the cursor returns
  // to normal even though the consumer disappeared.
  useEffect(() => {
    return () => {
      cleanupDragRef.current?.();
      cleanupDragRef.current = null;
      if (typeof document !== "undefined") {
        document.body.classList.remove("resizing-pane");
      }
    };
  }, []);

  const stepResize = useCallback(
    (signedDelta: number) => {
      setWidth((prev) => {
        if (signedDelta === Number.POSITIVE_INFINITY) return maxWidth;
        if (signedDelta === Number.NEGATIVE_INFINITY) return minWidth;
        return clampWidth(prev + signedDelta, minWidth, maxWidth);
      });
    },
    [maxWidth, minWidth],
  );

  // Toggle a body-level class so the global cursor + user-select rule
  // applies across the whole viewport during the drag. Without this,
  // dragging fast can leave the pointer over the main content where
  // text gets selected and the cursor reverts to text.
  useEffect(() => {
    if (typeof document === "undefined") return;
    const cls = "resizing-pane";
    if (isResizing) {
      document.body.classList.add(cls);
    } else {
      document.body.classList.remove(cls);
    }
    return () => {
      document.body.classList.remove(cls);
    };
  }, [isResizing]);

  const reset = useCallback(() => {
    setWidth(clampWidth(defaultWidth, minWidth, maxWidth));
  }, [defaultWidth, maxWidth, minWidth]);

  return { width, onPointerDown, isResizing, reset, stepResize };
}
