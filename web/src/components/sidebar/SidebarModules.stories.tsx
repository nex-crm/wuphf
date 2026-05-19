import type { Meta, StoryObj } from "@storybook/react-vite";

import { SidebarContext } from "../../../.storybook/sidebar-decorator";
import { SidebarColorPicker } from "./SidebarColorPicker";
import { UsagePanel } from "./UsagePanel";
import { WorkspaceSummary } from "./WorkspaceSummary";
import { AgentList } from "./AgentList";
import { AppList } from "./AppList";
import { ChannelList } from "./ChannelList";
import { InboxButton } from "./InboxButton";
import { IssuesGroup } from "./IssuesGroup";

/**
 * Each story mounts the REAL sidebar sub-component inside a sidebar shell,
 * with React Query seeded by SidebarContext. No mocks of the DOM.
 */
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
    <div className="sidebar-section is-team">
      <div className="sidebar-section-title">Agents</div>
      <div className="sidebar-collapsible is-open">
        <AgentList />
      </div>
    </div>
  ),
};

export const Channels: StoryObj = {
  render: () => (
    <div className="sidebar-section">
      <div className="sidebar-section-title">Channels</div>
      <div className="sidebar-collapsible is-open">
        <ChannelList />
      </div>
    </div>
  ),
};

export const Issues: StoryObj = {
  render: () => <IssuesGroup open onToggle={() => {}} />,
};

export const Apps: StoryObj = {
  render: () => (
    <div className="sidebar-section">
      <div className="sidebar-section-title">Tools</div>
      <div className="sidebar-collapsible is-open">
        <AppList />
      </div>
    </div>
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

export const ColorPicker: StoryObj = {
  name: "Color picker",
  render: () => <SidebarColorPicker />,
};
