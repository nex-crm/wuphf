import { useCallback } from "react";

const STEP_PX = 16;
const STEP_PX_LARGE = 64;

interface PaneResizeHandleProps {
  /** Pointer-down from useResizablePane(). */
  onPointerDown: (event: React.PointerEvent<HTMLElement>) => void;
  /** True while the user is actively dragging. Adds an "is-active" hook. */
  isResizing: boolean;
  /** Double-click target — reset pane to its default width. */
  onReset: () => void;
  /** Keyboard step resize — positive widens the pane from the user's POV. */
  onStepResize: (signedDelta: number) => void;
  /** Which edge of the parent pane this handle anchors to. */
  edge: "right" | "left";
  /** Accessibility label, e.g. "Resize sidebar". */
  ariaLabel: string;
  /** Current pane width — surfaced to assistive tech via aria-valuenow. */
  valueNow: number;
  /** Minimum pane width — surfaced to assistive tech via aria-valuemin. */
  valueMin: number;
  /** Maximum pane width — surfaced to assistive tech via aria-valuemax. */
  valueMax: number;
}

/**
 * Thin draggable splitter rendered on the edge of a resizable pane. The
 * pane element positions this absolutely; the handle expands its hit area
 * via a pseudo-element so the visible 1px rule is still easy to grab.
 *
 * Keyboard map (mirrors the WAI-ARIA separator pattern):
 *   ArrowLeft / ArrowRight  → ±16px from the user's viewpoint
 *   Shift + Arrow           → ±64px (coarse step)
 *   Home / End              → snap to min / max
 *   Enter                   → reset to default width
 */
export function PaneResizeHandle({
  onPointerDown,
  isResizing,
  onReset,
  onStepResize,
  edge,
  ariaLabel,
  valueNow,
  valueMin,
  valueMax,
}: PaneResizeHandleProps) {
  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLDivElement>) => {
      // Resolve direction from the user's perspective: pressing ArrowRight
      // should widen a right-edge pane and narrow a left-edge pane. The
      // hook expects a signed delta where positive = widen, so we flip
      // the arrow sign for left-edge handles.
      const stepBase = e.shiftKey ? STEP_PX_LARGE : STEP_PX;
      const widen = edge === "right" ? stepBase : -stepBase;
      const narrow = -widen;

      switch (e.key) {
        case "ArrowRight":
          e.preventDefault();
          onStepResize(widen);
          return;
        case "ArrowLeft":
          e.preventDefault();
          onStepResize(narrow);
          return;
        case "Home":
          e.preventDefault();
          onStepResize(Number.NEGATIVE_INFINITY);
          return;
        case "End":
          e.preventDefault();
          onStepResize(Number.POSITIVE_INFINITY);
          return;
        case "Enter":
          e.preventDefault();
          onReset();
          return;
      }
    },
    [edge, onReset, onStepResize],
  );

  return (
    <div
      role="separator"
      aria-orientation="vertical"
      aria-label={ariaLabel}
      aria-valuenow={Math.round(valueNow)}
      aria-valuemin={valueMin}
      aria-valuemax={valueMax}
      tabIndex={0}
      className={`pane-resize-handle pane-resize-handle--${edge}${
        isResizing ? " is-active" : ""
      }`}
      onPointerDown={onPointerDown}
      onDoubleClick={onReset}
      onKeyDown={handleKeyDown}
    />
  );
}
