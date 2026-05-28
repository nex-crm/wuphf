import { SidebarCollapse } from "iconoir-react";

import { useWorkspacesList } from "../../api/workspaces";
import { useResizablePane } from "../../hooks/useResizablePane";
import { useAppStore } from "../../stores/app";
import { TeamMemberBadge } from "../join/TeamMemberBadge";
import { SidebarPreviewOverlay } from "../onboarding/SidebarPreviewOverlay";
import { AgentList } from "../sidebar/AgentList";
import { ChannelList } from "../sidebar/ChannelList";
import { SidebarSection } from "../sidebar/SidebarSection";
import { UsagePanel } from "../sidebar/UsagePanel";
import { CollapsedSidebar } from "./CollapsedSidebar";
import { PaneResizeHandle } from "./PaneResizeHandle";

// Mirrors the WORKSPACE_PALETTE in WorkspaceRail so the sidebar chip
// matches the rail icon for the same workspace.
const WORKSPACE_CHIP_PALETTE = [
  "#069de4",
  "#9f4dbf",
  "#e0833e",
  "#3aa76d",
  "#e25c7a",
  "#5a7bd9",
  "#d4a017",
  "#4cb6ad",
];
function workspaceChipBg(name: string): string {
  let hash = 0;
  for (let i = 0; i < name.length; i++) {
    hash = (hash * 31 + name.charCodeAt(i)) >>> 0;
  }
  return WORKSPACE_CHIP_PALETTE[hash % WORKSPACE_CHIP_PALETTE.length];
}

export const SIDEBAR_DEFAULT_WIDTH = 280;
export const SIDEBAR_MIN_WIDTH = 240;
export const SIDEBAR_MAX_WIDTH = 360;
export const SIDEBAR_WIDTH_STORAGE_KEY = "wuphf-sidebar-width";

export function Sidebar() {
  const sidebarAgentsOpen = useAppStore((s) => s.sidebarAgentsOpen);
  const toggleSidebarAgents = useAppStore((s) => s.toggleSidebarAgents);
  const sidebarChannelsOpen = useAppStore((s) => s.sidebarChannelsOpen);
  const toggleSidebarChannels = useAppStore((s) => s.toggleSidebarChannels);
  const sidebarCollapsed = useAppStore((s) => s.sidebarCollapsed);
  const toggleSidebarCollapsed = useAppStore((s) => s.toggleSidebarCollapsed);

  const resize = useResizablePane({
    storageKey: SIDEBAR_WIDTH_STORAGE_KEY,
    defaultWidth: SIDEBAR_DEFAULT_WIDTH,
    minWidth: SIDEBAR_MIN_WIDTH,
    maxWidth: SIDEBAR_MAX_WIDTH,
    edge: "right",
  });

  const { data: workspacesData } = useWorkspacesList();
  const activeWorkspace = workspacesData?.workspaces.find(
    (w) => w.is_active || w.name === workspacesData?.active,
  );
  const workspaceTitle =
    activeWorkspace?.company_name?.trim() ||
    activeWorkspace?.name ||
    "WUPHF";
  const workspaceChipColor = activeWorkspace
    ? workspaceChipBg(activeWorkspace.name)
    : null;

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
            <span
              className={`sidebar-logo${activeWorkspace ? " is-workspace" : ""}`}
              title={activeWorkspace?.name}
              style={
                workspaceChipColor
                  ? ({
                      "--workspace-chip-bg": workspaceChipColor,
                    } as React.CSSProperties)
                  : undefined
              }
            >
              {workspaceTitle}
            </span>
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
            </div>
          </div>

          {/* Inbox moved to the WorkspaceRail (above Tools); kept the
              shell quiet so Agents anchors the top of the scroll list. */}

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

            {/* Issues moved to the WorkspaceRail tools column — main
                deleted IssuesGroup, the /issues page is the source of
                truth now. */}

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
