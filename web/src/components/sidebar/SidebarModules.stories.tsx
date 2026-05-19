import type { Meta, StoryObj } from "@storybook/react-vite";

import { SidebarContext } from "../../../.storybook/sidebar-decorator";
import { UsagePanel } from "./UsagePanel";
import { WorkspaceSummary } from "./WorkspaceSummary";
import { AgentList } from "./AgentList";
import { AppList } from "./AppList";
import { ChannelList } from "./ChannelList";
import { InboxButton } from "./InboxButton";
import { IssuesGroup } from "./IssuesGroup";
import { RecentObjectsPanel } from "./RecentObjectsPanel";
import { SidebarSection } from "./SidebarSection";

const RECENT_STORAGE_KEY = "wuphf-recent-objects";

const SAMPLE_RECENT = [
  {
    ref: { kind: "task", id: "bookkeeping-invoicing-service-3" },
    label: "Task: bookkeeping-invoicing-service-3",
    href: "#/tasks/bookkeeping-invoicing-service-3",
    visitedAtMs: Date.now() - 1_000,
  },
  {
    ref: { kind: "task", id: "bookkeeping-invoicing-service-4" },
    label: "Task: bookkeeping-invoicing-service-4",
    href: "#/tasks/bookkeeping-invoicing-service-4",
    visitedAtMs: Date.now() - 2_000,
  },
  {
    ref: { kind: "wiki-page", id: "people/nazz" },
    label: "Wiki: people/nazz",
    href: "#/wiki/people/nazz",
    visitedAtMs: Date.now() - 3_000,
  },
  {
    ref: { kind: "agent", id: "atlas" },
    label: "Agent: atlas",
    href: "#/agents/atlas",
    visitedAtMs: Date.now() - 4_000,
  },
  {
    ref: { kind: "settings-section", id: "workspace" },
    label: "Settings: Workspace",
    href: "#/apps/settings?section=workspace",
    visitedAtMs: Date.now() - 5_000,
  },
];

// Seed at module load so the panel's synchronous localStorage read on its
// first render already has sample data. Effects fire too late — the panel
// returns null on the empty initial read.
if (typeof window !== "undefined") {
  localStorage.setItem(RECENT_STORAGE_KEY, JSON.stringify(SAMPLE_RECENT));
}

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
      <div className="sidebar-collapsible is-open" style={collapsibleNaturalHeight}>
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
      <div className="sidebar-collapsible is-open" style={collapsibleNaturalHeight}>
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
      <div className="sidebar-collapsible is-open" style={collapsibleNaturalHeight}>
        <IssuesGroup open />
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
      <div className="sidebar-collapsible is-open" style={collapsibleNaturalHeight}>
        <AppList />
      </div>
    </>
  ),
};

export const Recent: StoryObj = {
  render: () => (
    <>
      <style>{`.sidebar-collapsible.is-open { flex: none; display: block; }`}</style>
      <SidebarSection label="Recent" open onToggle={() => {}}>
        <RecentObjectsPanel />
      </SidebarSection>
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
