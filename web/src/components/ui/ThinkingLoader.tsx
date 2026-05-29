/**
 * ThinkingLoader — a Claude-style "a response is materializing here" loader.
 *
 * Two variants:
 *  - `inline` (default): three wave dots tucked inside a soft incoming-bubble
 *    pill with a traveling sheen. Used by TypingIndicator so the loader sits
 *    exactly where the next message bubble will render.
 *  - `block`: a centered shimmer label for whole-surface loading states
 *    (e.g. the message feed's first fetch).
 *
 * Motion is theme-adaptive (color-mix off --text/--accent) and fully disabled
 * under prefers-reduced-motion via CSS.
 */
interface ThinkingLoaderProps {
  variant?: "inline" | "block";
  /** Accessible description announced to screen readers (e.g. "CEO is typing"). */
  label?: string;
  className?: string;
}

export function ThinkingLoader({
  variant = "inline",
  label,
  className,
}: ThinkingLoaderProps) {
  if (variant === "block") {
    return (
      <div
        className={`thinking-loader thinking-loader-block${className ? ` ${className}` : ""}`}
        role="status"
        aria-live="polite"
      >
        <span className="thinking-shimmer-text">{label ?? "Loading…"}</span>
      </div>
    );
  }

  return (
    <div
      className={`thinking-loader thinking-loader-inline${className ? ` ${className}` : ""}`}
      role="status"
      aria-live="polite"
      aria-label={label}
    >
      <span className="thinking-bubble" aria-hidden="true">
        <span className="thinking-dot" />
        <span className="thinking-dot" />
        <span className="thinking-dot" />
      </span>
      {label ? <span className="sr-only">{label}</span> : null}
    </div>
  );
}
