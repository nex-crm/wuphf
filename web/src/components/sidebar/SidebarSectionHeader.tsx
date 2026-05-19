/**
 * SidebarSectionHeader — the title bar used by every collapsible sidebar
 * section. One shared shape for AGENTS / CHANNELS / ISSUES / TOOLS so the
 * label styling, chevron, hit target, focus ring, and a11y semantics stay
 * identical across sections.
 *
 * The title bar itself uses `position: sticky` (see layout.css) so it
 * pins to the top of the scrolling sidebar while its section's body is
 * on screen — Slack-style. As the section scrolls past, the next
 * section's title bar takes over naturally because each is scoped to its
 * own section box.
 */

import { useEffect, useRef, type ReactNode } from "react";

interface SidebarSectionHeaderProps {
  label: string;
  open: boolean;
  onToggle: () => void;
  actions?: ReactNode;
  testId?: string;
}

function Chevron({ open }: { open: boolean }) {
  return (
    <svg
      className="sidebar-section-chevron"
      aria-hidden="true"
      focusable="false"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      data-open={open}
    >
      <path d="m9 18 6-6-6-6" />
    </svg>
  );
}

export function SidebarSectionHeader({
  label,
  open,
  onToggle,
  actions,
  testId,
}: SidebarSectionHeaderProps) {
  // Detect when the sticky title bar is actually pinned to the top of
  // the scroll container so CSS can fade in a light shadow underneath
  // it. Compare the title bar's top against the scroll container's top
  // on every scroll — a 1px tolerance because sub-pixel rounding can
  // leave a fractional gap. Cheap, deterministic; IO with a sticky
  // child has known quirks across browsers.
  const tbRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const titleBar = tbRef.current;
    if (!titleBar) return;
    const scrollRoot = titleBar.closest<HTMLElement>(".sidebar-scroll");
    if (!scrollRoot) return;
    const update = () => {
      const tbTop = titleBar.getBoundingClientRect().top;
      const rootTop = scrollRoot.getBoundingClientRect().top;
      titleBar.dataset.stuck = tbTop - rootTop < 1 ? "true" : "false";
    };
    // Initial layout may not be settled when the effect runs; rAF after
    // commit gives the sticky pin a chance to resolve before measurement.
    const raf = requestAnimationFrame(update);
    scrollRoot.addEventListener("scroll", update, { passive: true });
    // Recompute on layout changes (section open/close, viewport resize).
    const ro = new ResizeObserver(update);
    ro.observe(scrollRoot);
    return () => {
      cancelAnimationFrame(raf);
      scrollRoot.removeEventListener("scroll", update);
      ro.disconnect();
    };
  }, []);

  return (
    <div
      ref={tbRef}
      className="sidebar-section-title-bar"
      data-testid={testId}
    >
      <button
        type="button"
        className="sidebar-section-title sidebar-section-toggle"
        onClick={onToggle}
        aria-expanded={open}
      >
        <Chevron open={open} />
        <span className="sidebar-section-label">{label}</span>
      </button>
      {actions ? (
        <div className="sidebar-section-actions">{actions}</div>
      ) : null}
    </div>
  );
}
