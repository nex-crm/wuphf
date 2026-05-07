import type { ReactNode } from "react";
import { useCallback, useState } from "react";
import { NavArrowDown, NavArrowRight } from "iconoir-react";

interface CollapsibleSectionProps {
  title: ReactNode;
  /** Optional element rendered to the right of the title (counts, badges). */
  meta?: ReactNode;
  /** Defaults open. Pass `defaultOpen={false}` for sections that should start collapsed. */
  defaultOpen?: boolean;
  /**
   * Stable id used as the testid suffix and aria target. Kept optional so
   * callers without a strong identity (one-off sections) can omit it.
   */
  id?: string;
  /**
   * Keep the body mounted when collapsed and hide it via the `hidden` attribute
   * instead of unmounting. Use this for children that own live state — sockets,
   * media players, xterm scrollback — where unmount/remount would drop data the
   * collapse is supposed to merely hide.
   */
  keepMounted?: boolean;
  children: ReactNode;
}

export function CollapsibleSection({
  title,
  meta,
  defaultOpen = true,
  id,
  keepMounted = false,
  children,
}: CollapsibleSectionProps) {
  const [open, setOpen] = useState(defaultOpen);
  const toggle = useCallback(() => setOpen((value) => !value), []);
  const bodyId = id ? `collapsible-${id}` : undefined;

  return (
    <section
      className={`collapsible-section${open ? " is-open" : ""}`}
      data-testid={id ? `collapsible-${id}` : undefined}
    >
      <button
        type="button"
        className="collapsible-section-header"
        onClick={toggle}
        aria-expanded={open}
        aria-controls={bodyId}
      >
        <span className="collapsible-section-chevron" aria-hidden="true">
          {open ? (
            <NavArrowDown width={14} height={14} />
          ) : (
            <NavArrowRight width={14} height={14} />
          )}
        </span>
        <span className="collapsible-section-title">{title}</span>
        {meta ? <span className="collapsible-section-meta">{meta}</span> : null}
      </button>
      {keepMounted ? (
        <div className="collapsible-section-body" id={bodyId} hidden={!open}>
          {children}
        </div>
      ) : open ? (
        <div className="collapsible-section-body" id={bodyId}>
          {children}
        </div>
      ) : null}
    </section>
  );
}
