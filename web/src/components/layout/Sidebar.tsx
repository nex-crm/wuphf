import { Settings as SettingsIcon, SidebarCollapse } from "iconoir-react";

import { useResizablePane } from "../../hooks/useResizablePane";
import { router } from "../../lib/router";
import { useCurrentApp } from "../../routes/useCurrentRoute";
import { useAppStore } from "../../stores/app";
import { TeamMemberBadge } from "../join/TeamMemberBadge";
import { SidebarPreviewOverlay } from "../onboarding/SidebarPreviewOverlay";
import { AgentList } from "../sidebar/AgentList";
import { AppList } from "../sidebar/AppList";
import { ChannelList } from "../sidebar/ChannelList";
import { InboxButton } from "../sidebar/InboxButton";
import { IssuesGroup } from "../sidebar/IssuesGroup";
import { RecentObjectsPanel } from "../sidebar/RecentObjectsPanel";
import { SidebarColorPicker } from "../sidebar/SidebarColorPicker";
import { UsagePanel } from "../sidebar/UsagePanel";
import { WorkspaceSummary } from "../sidebar/WorkspaceSummary";
import { CollapsedSidebar } from "./CollapsedSidebar";
import { PaneResizeHandle } from "./PaneResizeHandle";

export const SIDEBAR_DEFAULT_WIDTH = 220;
export const SIDEBAR_MIN_WIDTH = 180;
export const SIDEBAR_MAX_WIDTH = 420;
export const SIDEBAR_WIDTH_STORAGE_KEY = "wuphf-sidebar-width";

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
  const sidebarIssuesOpen = useAppStore((s) => s.sidebarIssuesOpen);
  const toggleSidebarIssues = useAppStore((s) => s.toggleSidebarIssues);
  const sidebarAppsOpen = useAppStore((s) => s.sidebarAppsOpen);
  const toggleSidebarApps = useAppStore((s) => s.toggleSidebarApps);
  const sidebarCollapsed = useAppStore((s) => s.sidebarCollapsed);
  const toggleSidebarCollapsed = useAppStore((s) => s.toggleSidebarCollapsed);
  const sidebarBg = useAppStore((s) => s.sidebarBg);
  const currentApp = useCurrentApp();

  const resize = useResizablePane({
    storageKey: SIDEBAR_WIDTH_STORAGE_KEY,
    defaultWidth: SIDEBAR_DEFAULT_WIDTH,
    minWidth: SIDEBAR_MIN_WIDTH,
    maxWidth: SIDEBAR_MAX_WIDTH,
    edge: "right",
  });

  // Collapsed rail keeps its fixed CSS width; only the expanded sidebar
  // honors the user's drag. We hand the dragged width to CSS as a custom
  // property rather than `width:` directly so the mobile media queries
  // (which clamp the sidebar to 240px / full overlay) can still win
  // — inline `width` would beat them with normal cascade rules.
  const asideStyle = {
    ...(sidebarBg ? { background: sidebarBg } : null),
    ...(sidebarCollapsed
      ? null
      : { "--sidebar-resize-width": `${resize.width}px` }),
  } as React.CSSProperties;

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

          <div className="sidebar-primary">
            <InboxButton />
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

          {/* Phase 3 — Issues group (between Channels and Tools, per spec Surface 2 layout). */}
          <div
            className={`sidebar-section${sidebarIssuesOpen ? "" : " is-collapsed"}`}
          >
            <IssuesGroup
              open={sidebarIssuesOpen}
              onToggle={toggleSidebarIssues}
            />
          </div>
          <div
            className={`sidebar-collapsible${sidebarIssuesOpen ? " is-open" : ""}`}
          >
            {/* Issue list rows are rendered inside IssuesGroup when open */}
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

          {/* Phase 2 onboarding preview overlay — shows staged channels/agents
              forming as the user answers CEO questions. Hidden once onboarded. */}
          <SidebarPreviewOverlay />

          <RecentObjectsPanel />
          <WorkspaceSummary />
          <UsagePanel />
          <SidebarColorPicker />
        </>
      )}
      {!sidebarCollapsed && (
        <PaneResizeHandle
          edge="right"
          ariaLabel="Resize sidebar"
          onPointerDown={resize.onPointerDown}
          isResizing={resize.isResizing}
          onReset={resize.reset}
          onStepResize={resize.stepResize}
          valueNow={resize.width}
          valueMin={SIDEBAR_MIN_WIDTH}
          valueMax={SIDEBAR_MAX_WIDTH}
        />
      )}
    </aside>
  );
}
