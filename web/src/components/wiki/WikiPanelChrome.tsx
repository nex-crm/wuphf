import {
  PanelLeftClose,
  PanelLeftOpen,
  PanelRightClose,
  PanelRightOpen,
} from "lucide-react";

import type { WikiPanelSide } from "../../hooks/useWikiPanels";

/**
 * Shared chrome for the wiki's collapsible flanking panels. Both the left page
 * tree and the right details rail reuse these two pieces so the collapse
 * affordance reads identically on either side:
 *
 *   • PanelCollapseButton — a quiet icon button shown inside an open panel;
 *     clicking it folds the panel away.
 *   • CollapsedPanelRail — the panel reduced to a thin, full-height strip with
 *     a vertical label; clicking anywhere on it restores the panel.
 *
 * The lucide Panel{Left,Right}{Open,Close} glyphs already encode "which side"
 * and "which direction", so no extra chevrons are needed.
 */

interface PanelCollapseButtonProps {
  side: WikiPanelSide;
  /** Human label for the panel, used in the accessible name (e.g. "Pages"). */
  label: string;
  onClick: () => void;
}

export function PanelCollapseButton({
  side,
  label,
  onClick,
}: PanelCollapseButtonProps) {
  const Icon = side === "left" ? PanelLeftClose : PanelRightClose;
  return (
    <button
      type="button"
      className={`wk-panel-collapse wk-panel-collapse--${side}`}
      onClick={onClick}
      aria-label={`Collapse ${label} panel`}
      aria-expanded={true}
      title={`Collapse ${label.toLowerCase()}`}
    >
      <Icon size={16} aria-hidden="true" />
    </button>
  );
}

interface CollapsedPanelRailProps {
  side: WikiPanelSide;
  label: string;
  onExpand: () => void;
}

export function CollapsedPanelRail({
  side,
  label,
  onExpand,
}: CollapsedPanelRailProps) {
  const Icon = side === "left" ? PanelLeftOpen : PanelRightOpen;
  return (
    <button
      type="button"
      className={`wk-panel-rail wk-panel-rail--${side}`}
      onClick={onExpand}
      aria-label={`Expand ${label} panel`}
      aria-expanded={false}
      title={`Expand ${label.toLowerCase()}`}
    >
      <Icon size={16} aria-hidden="true" className="wk-panel-rail-icon" />
      <span className="wk-panel-rail-label">{label}</span>
    </button>
  );
}
