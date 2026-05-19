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
        <aside
          className="sidebar"
          style={{
            width: 240,
            minHeight: 320,
            borderRadius: "var(--radius-md)",
            display: "flex",
            flexDirection: "column",
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
  render: () => (
    <div style={{ marginTop: "auto" }}>
      <WorkspaceSummary />
    </div>
  ),
};

export const Usage: StoryObj = {
  name: "Usage panel",
  render: () => (
    <div style={{ marginTop: "auto" }}>
      <UsagePanel />
    </div>
  ),
};

export const ColorPicker: StoryObj = {
  name: "Color picker",
  render: () => (
    <div style={{ marginTop: "auto" }}>
      <SidebarColorPicker />
    </div>
  ),
};
