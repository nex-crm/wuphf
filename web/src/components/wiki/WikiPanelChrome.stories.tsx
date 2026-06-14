import type { Meta, StoryObj } from "@storybook/react-vite";

import { CollapsedPanelRail, PanelCollapseButton } from "./WikiPanelChrome";
import "../../styles/wiki.css";

/**
 * Fold-away chrome shared by the wiki's two flanking panels (left page tree,
 * right details rail): a quiet in-panel collapse button, and the thin
 * full-height rail the panel folds into. The lucide Panel{Left,Right} glyphs
 * encode both which side and which direction, so no extra chevrons are needed.
 */
const meta: Meta = {
  title: "Wiki / WikiPanelChrome",
};
export default meta;

type Story = StoryObj;

/** The in-panel collapse buttons for each side. */
export const CollapseButtons: Story = {
  render: () => (
    <div
      className="wiki-root"
      style={{ display: "flex", gap: 24, padding: 24, alignItems: "center" }}
    >
      <PanelCollapseButton side="left" label="Pages" onClick={() => {}} />
      <PanelCollapseButton side="right" label="Details" onClick={() => {}} />
    </div>
  ),
};

/** The collapsed rails as they sit in the layout — flanking the reading column. */
export const Rails: Story = {
  render: () => (
    <div className="wiki-root" style={{ display: "flex", height: 320 }}>
      <div
        style={{
          width: 44,
          background: "var(--wk-sidebar-bg)",
          borderRight: "1px solid var(--wk-border)",
        }}
      >
        <CollapsedPanelRail side="left" label="Pages" onExpand={() => {}} />
      </div>
      <div
        style={{
          flex: 1,
          padding: 24,
          color: "var(--wk-text-muted)",
          fontSize: 13,
        }}
      >
        Reading column
      </div>
      <div
        style={{
          width: 44,
          background: "var(--wk-sidebar-bg)",
          borderLeft: "1px solid var(--wk-border)",
        }}
      >
        <CollapsedPanelRail side="right" label="Details" onExpand={() => {}} />
      </div>
    </div>
  ),
};
