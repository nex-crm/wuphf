import type { Meta, StoryObj } from "@storybook/react-vite";

import { SidebarContext } from "../../../.storybook/sidebar-decorator";
import { AgentList } from "./AgentList";
import { AppList } from "./AppList";
import { ChannelList } from "./ChannelList";
import { InboxButton } from "./InboxButton";
import { IssuesGroup } from "./IssuesGroup";
import { UsagePanel } from "./UsagePanel";
import { WorkspaceSummary } from "./WorkspaceSummary";

/**
 * Each story mounts the REAL sidebar sub-component inside a sidebar shell,
 * with React Query seeded by SidebarContext. No mocks of the DOM.
 *
 * .sidebar-collapsible normally uses `flex: 1 1 0` to fill the remaining
 * height of the 100vh sidebar. In an isolated story the sidebar collapses
 * to content height, so flex:1 1 0 resolves to 0 and the collapsible body
 * disappears. Story-only override: `flex: none` + `display: block` so the
 * list renders at its natural content height.
 */
const collapsibleNaturalHeight = {
  flex: "none",
  display: "block",
} as const;
const meta: Meta = {
  title: "Sidebar/Modules",
  parameters: {
    layout: "padded",
    backgrounds: { default: "elevated" },
  },
  decorators: [
    (Story) => (
      <SidebarContext>
        {/* Real .sidebar styles minus the `height: 100vh` lock so each
            module renders at its natural height. Width matches the live
            sidebar's 240px default. */}
        <aside
          className="sidebar"
          style={{
            width: 240,
            height: "auto",
            minHeight: 0,
            overflow: "visible",
            borderRadius: "var(--radius-md)",
          }}
        >
          <Story />
        </aside>
      </SidebarContext>
    ),
  ],
};

export default meta;

export const InboxModule: StoryObj = {
  name: "Inbox button",
  render: () => (
    <div className="sidebar-primary">
      <InboxButton />
    </div>
  ),
};

export const Agents: StoryObj = {
  render: () => (
    <>
      <div className="sidebar-section is-team">
        <div className="sidebar-section-title">Agents</div>
      </div>
      <div
        className="sidebar-collapsible is-open"
        style={collapsibleNaturalHeight}
      >
        <AgentList />
      </div>
    </>
  ),
};

export const Channels: StoryObj = {
  render: () => (
    <>
      <div className="sidebar-section">
        <div className="sidebar-section-title">Channels</div>
      </div>
      <div
        className="sidebar-collapsible is-open"
        style={collapsibleNaturalHeight}
      >
        <ChannelList />
      </div>
    </>
  ),
};

export const Issues: StoryObj = {
  render: () => (
    <>
      <div className="sidebar-section">
        <div className="sidebar-section-title">Issues</div>
      </div>
      <div
        className="sidebar-collapsible is-open"
        style={collapsibleNaturalHeight}
      >
        <IssuesGroup open={true} />
      </div>
    </>
  ),
};

export const Apps: StoryObj = {
  render: () => (
    <>
      <div className="sidebar-section">
        <div className="sidebar-section-title">Tools</div>
      </div>
      <div
        className="sidebar-collapsible is-open"
        style={collapsibleNaturalHeight}
      >
        <AppList />
      </div>
    </>
  ),
};

export const Workspace: StoryObj = {
  name: "Workspace summary",
  render: () => <WorkspaceSummary />,
};

export const Usage: StoryObj = {
  name: "Usage panel",
  render: () => <UsagePanel />,
};
