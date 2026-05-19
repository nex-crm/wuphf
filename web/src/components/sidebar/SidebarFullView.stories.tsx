import type { Meta, StoryObj } from "@storybook/react-vite";

import { SidebarContext } from "../../../.storybook/sidebar-decorator";
import { Sidebar } from "../layout/Sidebar";

/**
 * Mounts the REAL Sidebar (from components/layout/Sidebar) with React Query
 * + memory router seeded by SidebarContext. The right pane is just a
 * scaffold so the sidebar has a layout neighbour.
 */
const meta: Meta = {
  title: "Sidebar/Full view",
  parameters: { layout: "fullscreen" },
};

export default meta;

function MockContent({ title }: { title: string }) {
  return (
    <main
      style={{
        flex: 1,
        padding: 32,
        color: "var(--text)",
        background: "var(--bg)",
        minHeight: "100vh",
      }}
    >
      <h2 style={{ margin: 0, marginBottom: 8 }}>{title}</h2>
      <p style={{ color: "var(--text-secondary)", maxWidth: 520 }}>
        Switch themes from the toolbar to see the sidebar reskin via tokens.
      </p>
    </main>
  );
}

export const Expanded: StoryObj = {
  render: () => (
    <SidebarContext initialUrl="/channels/architecture">
      <div style={{ display: "flex", minHeight: "100vh" }}>
        <Sidebar />
        <MockContent title="#architecture" />
      </div>
    </SidebarContext>
  ),
};

export const OnAppRoute: StoryObj = {
  name: "On an app route",
  render: () => (
    <SidebarContext initialUrl="/apps/wiki">
      <div style={{ display: "flex", minHeight: "100vh" }}>
        <Sidebar />
        <MockContent title="Wiki" />
      </div>
    </SidebarContext>
  ),
};

export const HeavyUnread: StoryObj = {
  name: "Heavy unread",
  render: () => (
    <SidebarContext
      initialUrl="/channels/incidents"
      unreadByChannel={{
        architecture: 3,
        deploys: 11,
        wiki: 1,
        incidents: 47,
      }}
    >
      <div style={{ display: "flex", minHeight: "100vh" }}>
        <Sidebar />
        <MockContent title="#incidents" />
      </div>
    </SidebarContext>
  ),
};
