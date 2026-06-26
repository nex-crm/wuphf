/**
 * SidebarItem — unified row used by every list inside a sidebar section
 * (channels, issues, tools, plus the trailing "+ new" / "view all" rows).
 *
 * Slots: leading icon, label, trailing badge, trailing shortcut. Variants:
 * "default" (rows), "add" (+ new …), "view-all" (muted nav link). All four
 * compose the same `.sidebar-item` chrome so spacing, hover, focus, and
 * active states stay identical across sections.
 */

import type { CSSProperties, ReactNode } from "react";

import { SidebarItemLabel } from "./SidebarItemLabel";

export type SidebarItemVariant = "default" | "add" | "view-all";

interface SidebarItemProps {
  label: string;
  icon?: ReactNode;
  badge?: ReactNode;
  shortcut?: ReactNode;
  active?: boolean;
  variant?: SidebarItemVariant;
  onClick?: () => void;
  title?: string;
  "aria-label"?: string;
  "data-testid"?: string;
}

const LEADING_SLOT_STYLE: CSSProperties = {
  width: 18,
  textAlign: "center",
  flexShrink: 0,
  display: "inline-block",
};

function variantClass(variant: SidebarItemVariant): string {
  switch (variant) {
    case "add":
      return " sidebar-add-btn";
    case "view-all":
      return " sidebar-view-all";
    default:
      return "";
  }
}

export function SidebarItem({
  label,
  icon,
  badge,
  shortcut,
  active = false,
  variant = "default",
  onClick,
  title,
  "aria-label": ariaLabel,
  "data-testid": testId,
}: SidebarItemProps) {
  const labelMuted = variant === "view-all";
  return (
    <button
      type="button"
      className={`sidebar-item${active ? " active" : ""}${variantClass(variant)}`}
      onClick={onClick}
      title={title}
      aria-label={ariaLabel}
      data-testid={testId}
    >
      <span style={LEADING_SLOT_STYLE}>{icon}</span>
      {labelMuted ? (
        <span style={{ color: "var(--text-tertiary)", flex: 1, minWidth: 0 }}>
          {label}
        </span>
      ) : (
        <SidebarItemLabel>{label}</SidebarItemLabel>
      )}
      {badge}
      {shortcut}
    </button>
  );
}
