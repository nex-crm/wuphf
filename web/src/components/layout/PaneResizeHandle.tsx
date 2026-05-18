import { useCallback } from "react";

interface PaneResizeHandleProps {
  /** Pointer-down from useResizablePane(). */
  onPointerDown: (event: React.PointerEvent<HTMLElement>) => void;
  /** True while the user is actively dragging. Adds an "is-active" hook. */
  isResizing: boolean;
  /** Double-click target — reset pane to its default width. */
  onReset: () => void;
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
 */
export function PaneResizeHandle({
  onPointerDown,
  isResizing,
  onReset,
  edge,
  ariaLabel,
  valueNow,
  valueMin,
  valueMax,
}: PaneResizeHandleProps) {
  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLDivElement>) => {
      // Double-tap Enter / Space to reset is unusual; expose plain Enter
      // because there's no good keyboard-driven width-set affordance.
      if (e.key === "Enter") {
        e.preventDefault();
        onReset();
      }
    },
    [onReset],
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
