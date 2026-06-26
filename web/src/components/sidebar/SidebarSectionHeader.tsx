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

import { type ReactNode, useEffect, useRef } from "react";

import { registerStickyHeader } from "./stickyPinRegistry";

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
  // Pin detection lives in stickyPinRegistry so we share one scroll
  // listener (rAF-batched, one root-rect read per frame) across all
  // mounted section headers instead of attaching N listeners that each
  // re-read the scroll root.
  const tbRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const titleBar = tbRef.current;
    if (!titleBar) return;
    const scrollRoot = titleBar.closest<HTMLElement>(".sidebar-scroll");
    if (!scrollRoot) return;
    return registerStickyHeader(scrollRoot, titleBar);
  }, []);

  return (
    <div ref={tbRef} className="sidebar-section-title-bar" data-testid={testId}>
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
