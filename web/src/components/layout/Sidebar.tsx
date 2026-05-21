import { Settings as SettingsIcon, SidebarCollapse } from "iconoir-react";

import { useResizablePane } from "../../hooks/useResizablePane";
import { router } from "../../lib/router";
import { useCurrentApp, useCurrentRoute } from "../../routes/useCurrentRoute";
import { useAppStore } from "../../stores/app";
import { TeamMemberBadge } from "../join/TeamMemberBadge";
import { SidebarPreviewOverlay } from "../onboarding/SidebarPreviewOverlay";
import { AgentList } from "../sidebar/AgentList";
import { AppList } from "../sidebar/AppList";
import { ChannelList } from "../sidebar/ChannelList";
import { InboxButton } from "../sidebar/InboxButton";
import { IssuesGroup } from "../sidebar/IssuesGroup";
import { SidebarSection } from "../sidebar/SidebarSection";
import { UsagePanel } from "../sidebar/UsagePanel";
import { CollapsedSidebar } from "./CollapsedSidebar";
import { PaneResizeHandle } from "./PaneResizeHandle";

export const SIDEBAR_DEFAULT_WIDTH = 280;
export const SIDEBAR_MIN_WIDTH = 180;
export const SIDEBAR_MAX_WIDTH = 420;
export const SIDEBAR_WIDTH_STORAGE_KEY = "wuphf-sidebar-width";

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
  const currentApp = useCurrentApp();
  const route = useCurrentRoute();
  const issuesListActive = route.kind === "issues-list";

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
  const asideStyle = (
    sidebarCollapsed ? null : { "--sidebar-resize-width": `${resize.width}px` }
  ) as React.CSSProperties | null;

  return (
    <aside
      className={`sidebar${sidebarCollapsed ? " sidebar-collapsed" : ""}`}
      style={asideStyle ?? undefined}
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

          <div className="sidebar-scroll">
            <SidebarSection
              label="Agents"
              variant="team"
              open={sidebarAgentsOpen}
              onToggle={toggleSidebarAgents}
            >
              <AgentList />
            </SidebarSection>

            <SidebarSection
              label="Channels"
              open={sidebarChannelsOpen}
              onToggle={toggleSidebarChannels}
            >
              <ChannelList />
            </SidebarSection>

            {/* Phase 3 — Issues group (between Channels and Tools, per spec Surface 2 layout). */}
            <SidebarSection
              label="Issues"
              open={sidebarIssuesOpen}
              onToggle={toggleSidebarIssues}
              data-testid="issues-group-header"
              headerActions={
                <button
                  type="button"
                  className={`sidebar-section-action${issuesListActive ? " active" : ""}`}
                  onClick={() => void router.navigate({ to: "/issues" })}
                  title="View all issues"
                  data-testid="issues-sidebar-view-all"
                >
                  View all
                </button>
              }
            >
              <IssuesGroup open={sidebarIssuesOpen} />
            </SidebarSection>

            <SidebarSection
              label="Tools"
              open={sidebarAppsOpen}
              onToggle={toggleSidebarApps}
            >
              <AppList />
            </SidebarSection>

            {/* Phase 2 onboarding preview overlay — shows staged channels/agents
                forming as the user answers CEO questions. Hidden once onboarded. */}
            <SidebarPreviewOverlay />
          </div>
          {/* WorkspaceSummary intentionally not rendered here — the stats
              it shows (agents active, tasks open, tokens) are redundant
              with the Agents/Issues sections and the Usage footer. The
              component file is preserved so it can be re-used inside a
              future Usage popover or Settings surface. */}
          <UsagePanel />
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
