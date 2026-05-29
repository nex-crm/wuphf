import { useCyclingPhrase } from "../../hooks/useCyclingPhrase";

const NO_PHRASES: readonly string[] = [];

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
 * Pass `phrases` to rotate a cycling word à la the Claude Code spinner — we use
 * a The Office–themed set (see officeLoadingPhrases). The cycling text is
 * decorative and aria-hidden; the stable `label` carries the accessible name so
 * screen readers are not spammed on every tick.
 *
 * Motion is theme-adaptive (color-mix off --text/--accent) and fully disabled
 * under prefers-reduced-motion via CSS.
 */
interface ThinkingLoaderProps {
  variant?: "inline" | "block";
  /** Accessible description announced to screen readers (e.g. "CEO is typing"). */
  label?: string;
  /** Rotating decorative phrases (e.g. OFFICE_LOADING_PHRASES). */
  phrases?: readonly string[];
  className?: string;
}

export function ThinkingLoader({
  variant = "inline",
  label,
  phrases,
  className,
}: ThinkingLoaderProps) {
  const list = phrases ?? NO_PHRASES;
  const phrase = useCyclingPhrase(list, 2400, list.length > 0);
  const hasPhrase = list.length > 0 && phrase.length > 0;

  if (variant === "block") {
    return (
      <div
        className={`thinking-loader thinking-loader-block${className ? ` ${className}` : ""}`}
        role="status"
        aria-live="polite"
        aria-label={label}
      >
        {hasPhrase ? (
          <span
            key={phrase}
            className="thinking-shimmer-text thinking-shimmer-text-cycle"
            aria-hidden="true"
          >
            {phrase}
            <span className="thinking-ellipsis" aria-hidden="true">
              …
            </span>
          </span>
        ) : (
          <span className="thinking-shimmer-text">{label ?? "Loading…"}</span>
        )}
        {label ? <span className="sr-only">{label}</span> : null}
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
        <span className="thinking-dots">
          <span className="thinking-dot" />
          <span className="thinking-dot" />
          <span className="thinking-dot" />
        </span>
        {hasPhrase ? (
          <span key={phrase} className="thinking-bubble-phrase">
            {phrase}
            <span className="thinking-ellipsis">…</span>
          </span>
        ) : null}
      </span>
      {label ? <span className="sr-only">{label}</span> : null}
    </div>
  );
}
