import { createContext, useContext, useId, useState } from "react";

/**
 * Inline citation badge for a compiled-article `^[source-id]` marker.
 *
 * Renders a small superscript pill (`[n]`, Wikipedia-style) numbered by the
 * surrounding {@link CitationNumberContext}. On hover/focus/click it reveals a
 * lightweight popover with the raw citation id.
 *
 * The knowledge backend is now owned by gbrain, which serves wiki pages
 * directly (markdown otherwise). The old WUPHF source store — and the
 * `GET /sources/read` lookup this badge once used to hydrate a clicked
 * citation — has been retired, so the badge no longer calls any backend. It
 * degrades gracefully to showing the citation id itself; gbrain pages use
 * their own citation style, so richer badge-jump is a later concern.
 */

/**
 * Maps each cited source id to its 1-based citation number (first-appearance
 * order, repeated ids share a number). Provided by the read view; defaults to
 * an empty registry so a standalone badge still renders a generic marker.
 */
export const CitationNumberContext = createContext<ReadonlyMap<string, number>>(
  new Map(),
);

interface CitationBadgeProps {
  /** The cited source id, e.g. "task-wup-12". */
  sourceId: string;
}

export default function CitationBadge({ sourceId }: CitationBadgeProps) {
  const numbers = useContext(CitationNumberContext);
  const [open, setOpen] = useState(false);
  const popoverId = useId();

  const number = numbers.get(sourceId);
  const label = number !== undefined ? `[${number}]` : "[cite]";

  return (
    <sup className="wk-cite">
      <button
        type="button"
        className="wk-cite-badge"
        aria-expanded={open}
        aria-describedby={open ? popoverId : undefined}
        onMouseEnter={() => setOpen(true)}
        onFocus={() => setOpen(true)}
        onMouseLeave={() => setOpen(false)}
        onBlur={() => setOpen(false)}
        onClick={() => setOpen((prev) => !prev)}
      >
        {label}
      </button>
      {open ? (
        <span className="wk-cite-popover" id={popoverId} role="tooltip">
          <span className="wk-cite-title">{sourceId}</span>
        </span>
      ) : null}
    </sup>
  );
}
