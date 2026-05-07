import { Settings as SettingsIcon, SidebarCollapse } from "iconoir-react";

import { router } from "../../lib/router";
import { useCurrentApp } from "../../routes/useCurrentRoute";
import { useAppStore } from "../../stores/app";
import { TeamMemberBadge } from "../join/TeamMemberBadge";
import { AgentList } from "../sidebar/AgentList";
import { AppList } from "../sidebar/AppList";
import { ChannelList } from "../sidebar/ChannelList";
import { RecentObjectsPanel } from "../sidebar/RecentObjectsPanel";
import { SidebarColorPicker } from "../sidebar/SidebarColorPicker";
import { UsagePanel } from "../sidebar/UsagePanel";
import { WorkspaceSummary } from "../sidebar/WorkspaceSummary";
import { CollapsedSidebar } from "./CollapsedSidebar";

function SectionToggle({
  label,
  open,
  onToggle,
}: {
  label: string;
  open: boolean;
  onToggle: () => void;
}) {
  return (
    <button
      type="button"
      className="sidebar-section-title sidebar-section-toggle"
      onClick={onToggle}
      aria-expanded={open}
    >
      <span>{label}</span>
      <svg
        aria-hidden="true"
        focusable="false"
        style={{
          width: 10,
          height: 10,
          transform: open ? "rotate(90deg)" : "rotate(0deg)",
          transition: "transform 0.15s",
        }}
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
      >
        <path d="m9 18 6-6-6-6" />
      </svg>
    </button>
  );
}

// biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Existing cognitive complexity is baselined for a focused follow-up refactor.
export function Sidebar() {
  const sidebarAgentsOpen = useAppStore((s) => s.sidebarAgentsOpen);
  const toggleSidebarAgents = useAppStore((s) => s.toggleSidebarAgents);
  const sidebarChannelsOpen = useAppStore((s) => s.sidebarChannelsOpen);
  const toggleSidebarChannels = useAppStore((s) => s.toggleSidebarChannels);
  const sidebarAppsOpen = useAppStore((s) => s.sidebarAppsOpen);
  const toggleSidebarApps = useAppStore((s) => s.toggleSidebarApps);
  const sidebarCollapsed = useAppStore((s) => s.sidebarCollapsed);
  const toggleSidebarCollapsed = useAppStore((s) => s.toggleSidebarCollapsed);
  const sidebarBg = useAppStore((s) => s.sidebarBg);
  const currentApp = useCurrentApp();

  const asideStyle = sidebarBg ? { background: sidebarBg } : undefined;

  return (
    <aside
      className={`sidebar${sidebarCollapsed ? " sidebar-collapsed" : ""}`}
      style={asideStyle}
    >
      {sidebarCollapsed ? (
        <CollapsedSidebar />
      ) : (
        <>
          <div className="sidebar-header">
            <span className="sidebar-logo">WUPHF</span>
            <TeamMemberBadge />
            <div className="sidebar-header-actions">
              <button
                type="button"
                className="sidebar-icon-btn"
                aria-label="Collapse sidebar"
                title="Collapse sidebar"
                onClick={toggleSidebarCollapsed}
              >
                <SidebarCollapse />
              </button>
              <button
                type="button"
                className={`sidebar-icon-btn${currentApp === "settings" ? " active" : ""}`}
                aria-label="Open settings"
                title="Settings"
                onClick={() =>
                  router.navigate({
                    to: "/apps/$appId",
                    params: { appId: "settings" },
                  })
                }
              >
                <SettingsIcon />
              </button>
            </div>
          </div>

          <div
            className={`sidebar-section is-team${sidebarAgentsOpen ? "" : " is-collapsed"}`}
          >
            <SectionToggle
              label="Agents"
              open={sidebarAgentsOpen}
              onToggle={toggleSidebarAgents}
            />
          </div>
          <div
            className={`sidebar-collapsible${sidebarAgentsOpen ? " is-open" : ""}`}
          >
            <AgentList />
          </div>

          <div
            className={`sidebar-section${sidebarChannelsOpen ? "" : " is-collapsed"}`}
          >
            <SectionToggle
              label="Channels"
              open={sidebarChannelsOpen}
              onToggle={toggleSidebarChannels}
            />
          </div>
          <div
            className={`sidebar-collapsible${sidebarChannelsOpen ? " is-open" : ""}`}
          >
            <ChannelList />
          </div>

          <div
            className={`sidebar-section${sidebarAppsOpen ? "" : " is-collapsed"}`}
          >
            <SectionToggle
              label="Tools"
              open={sidebarAppsOpen}
              onToggle={toggleSidebarApps}
            />
          </div>
          <div
            className={`sidebar-collapsible${sidebarAppsOpen ? " is-open" : ""}`}
          >
            <AppList />
          </div>

          <RecentObjectsPanel />
          <WorkspaceSummary />
          <UsagePanel />
          <SidebarColorPicker />
        </>
      )}
    </aside>
  );
}
