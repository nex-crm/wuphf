/**
 * SidebarSection — header + collapsible body shell shared by every
 * top-level sidebar section (Agents, Channels, Issues, Tools). The
 * structural pair is split so the caller can put the scrolling list
 * body anywhere the layout wants it without nesting the chrome.
 */

import type { ReactNode } from "react";

import { SidebarSectionHeader } from "./SidebarSectionHeader";

interface SidebarSectionProps {
  label: string;
  open: boolean;
  onToggle: () => void;
  children: ReactNode;
  /** Adds the `.is-team` modifier used by the Agents section. */
  variant?: "default" | "team";
  /** Small trailing affordance(s) in the header (e.g. "View all"). */
  headerActions?: ReactNode;
  /** Forwarded to the `.sidebar-section` wrapper for tests. */
  "data-testid"?: string;
}

export function SidebarSection({
  label,
  open,
  onToggle,
  children,
  variant = "default",
  headerActions,
  "data-testid": testId,
}: SidebarSectionProps) {
  const sectionClass = `sidebar-section${variant === "team" ? " is-team" : ""}${
    open ? "" : " is-collapsed"
  }`;
  // `inert` removes the collapsed subtree from focus order and the
  // accessibility tree. Without it the grid-template-rows: 0fr collapse
  // is purely visual — keyboard Tab still lands on hidden buttons and
  // screen readers still announce hidden labels.
  return (
    <div className={sectionClass} data-testid={testId}>
      <SidebarSectionHeader
        label={label}
        open={open}
        onToggle={onToggle}
        actions={headerActions}
      />
      <div
        className={`sidebar-collapsible${open ? " is-open" : ""}`}
        inert={!open}
        aria-hidden={!open}
      >
        {children}
      </div>
    </div>
  );
}
