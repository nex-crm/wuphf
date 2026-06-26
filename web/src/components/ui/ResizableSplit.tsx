import {
  type CSSProperties,
  type ReactNode,
  useCallback,
  useRef,
  useState,
} from "react";

interface ResizableSplitProps {
  left: ReactNode;
  right: ReactNode;
  /** localStorage key the chosen ratio persists under. */
  storageKey: string;
  /** Left-pane fraction (0..1) used before the human drags. Default 0.55. */
  initialRatio?: number;
  /** Clamp bounds so neither pane can be dragged shut. Defaults 0.25 / 0.8. */
  minRatio?: number;
  maxRatio?: number;
  /** Accessible label for the drag handle. */
  ariaLabel?: string;
  className?: string;
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

function readStoredRatio(key: string, fallback: number): number {
  if (typeof window === "undefined") return fallback;
  const raw = window.localStorage.getItem(key);
  if (!raw) return fallback;
  const parsed = Number.parseFloat(raw);
  return Number.isFinite(parsed) ? parsed : fallback;
}

/**
 * ResizableSplit is a two-pane horizontal split with a draggable divider. The
 * left pane is sized by a ratio the human controls (drag the divider or use the
 * arrow keys); the right pane takes the rest. The ratio persists per
 * `storageKey` so the layout the operator picked survives reloads.
 *
 * The divider captures the pointer and the panes drop pointer-events mid-drag,
 * so a drag that crosses a sandboxed iframe (the app preview) keeps tracking
 * instead of being swallowed by the frame.
 */
export function ResizableSplit({
  left,
  right,
  storageKey,
  initialRatio = 0.55,
  minRatio = 0.25,
  maxRatio = 0.8,
  ariaLabel = "Resize panes",
  className,
}: ResizableSplitProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [ratio, setRatioState] = useState(() =>
    clamp(readStoredRatio(storageKey, initialRatio), minRatio, maxRatio),
  );
  const [dragging, setDragging] = useState(false);

  const persist = useCallback(
    (next: number) => {
      if (typeof window !== "undefined") {
        window.localStorage.setItem(storageKey, next.toFixed(4));
      }
    },
    [storageKey],
  );

  const setRatio = useCallback(
    (next: number, save = false) => {
      const clamped = clamp(next, minRatio, maxRatio);
      setRatioState(clamped);
      if (save) persist(clamped);
    },
    [minRatio, maxRatio, persist],
  );

  const onPointerMove = useCallback(
    (e: React.PointerEvent) => {
      if (!dragging) return;
      const el = containerRef.current;
      if (!el) return;
      const rect = el.getBoundingClientRect();
      if (rect.width <= 0) return;
      setRatio((e.clientX - rect.left) / rect.width);
    },
    [dragging, setRatio],
  );

  const endDrag = useCallback(
    (e: React.PointerEvent) => {
      if (!dragging) return;
      setDragging(false);
      persist(ratio);
      try {
        e.currentTarget.releasePointerCapture(e.pointerId);
      } catch {
        // Pointer capture may already be released; ignore.
      }
    },
    [dragging, persist, ratio],
  );

  const onKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      const step = e.shiftKey ? 0.1 : 0.02;
      if (e.key === "ArrowLeft") {
        e.preventDefault();
        setRatio(ratio - step, true);
      } else if (e.key === "ArrowRight") {
        e.preventDefault();
        setRatio(ratio + step, true);
      } else if (e.key === "Home") {
        e.preventDefault();
        setRatio(minRatio, true);
      } else if (e.key === "End") {
        e.preventDefault();
        setRatio(maxRatio, true);
      }
    },
    [ratio, setRatio, minRatio, maxRatio],
  );

  const pct = Math.round(ratio * 100);

  return (
    <div
      ref={containerRef}
      className={`resizable-split${dragging ? " resizable-split--dragging" : ""}${
        className ? ` ${className}` : ""
      }`}
    >
      <div
        className="resizable-split__pane resizable-split__pane--left"
        style={{ "--rs-left-basis": `${ratio * 100}%` } as CSSProperties}
      >
        {left}
      </div>
      {/* biome-ignore lint/a11y/useSemanticElements: this is the WAI-ARIA window-splitter pattern — a focusable, draggable handle with aria-valuenow/orientation. <hr> can't carry the drag + arrow-key resize affordance. */}
      <div
        role="separator"
        aria-orientation="vertical"
        aria-label={ariaLabel}
        aria-valuemin={Math.round(minRatio * 100)}
        aria-valuemax={Math.round(maxRatio * 100)}
        aria-valuenow={pct}
        tabIndex={0}
        className="resizable-split__divider"
        onPointerDown={(e) => {
          e.preventDefault();
          setDragging(true);
          e.currentTarget.setPointerCapture(e.pointerId);
        }}
        onPointerMove={onPointerMove}
        onPointerUp={endDrag}
        onPointerCancel={endDrag}
        onKeyDown={onKeyDown}
        onDoubleClick={() => setRatio(initialRatio, true)}
      >
        <span className="resizable-split__grip" aria-hidden="true" />
      </div>
      <div className="resizable-split__pane resizable-split__pane--right">
        {right}
      </div>
    </div>
  );
}
