import { useEffect, useState } from "react";
import { Settings as SettingsIcon, SidebarCollapse } from "iconoir-react";

import { useResizablePane } from "../../hooks/useResizablePane";
import { router } from "../../lib/router";
import { useCurrentApp } from "../../routes/useCurrentRoute";
import { useAppStore } from "../../stores/app";
import { TeamMemberBadge } from "../join/TeamMemberBadge";
import { SidebarPreviewOverlay } from "../onboarding/SidebarPreviewOverlay";
import { AppList } from "../sidebar/AppList";
import { UsagePanel } from "../sidebar/UsagePanel";
import { CollapsedSidebar } from "./CollapsedSidebar";
import { PaneResizeHandle } from "./PaneResizeHandle";

export const SIDEBAR_DEFAULT_WIDTH = 280;
export const SIDEBAR_MIN_WIDTH = 180;
export const SIDEBAR_MAX_WIDTH = 420;
export const SIDEBAR_WIDTH_STORAGE_KEY = "wuphf-sidebar-width";
const MOBILE_RAIL_QUERY = "(max-width: 768px)";

function useMobileRail(): boolean {
  const [matches, setMatches] = useState(() => {
    if (typeof window === "undefined" || !window.matchMedia) return false;
    return window.matchMedia(MOBILE_RAIL_QUERY).matches;
  });

  useEffect(() => {
    if (typeof window === "undefined" || !window.matchMedia) return;
    const query = window.matchMedia(MOBILE_RAIL_QUERY);
    const onChange = (event: MediaQueryListEvent) => setMatches(event.matches);
    setMatches(query.matches);
    query.addEventListener("change", onChange);
    return () => query.removeEventListener("change", onChange);
  }, []);

  return matches;
}

export function Sidebar() {
  const sidebarCollapsed = useAppStore((s) => s.sidebarCollapsed);
  const toggleSidebarCollapsed = useAppStore((s) => s.toggleSidebarCollapsed);
  const currentApp = useCurrentApp();
  const mobileRail = useMobileRail();
  const [mobileExpanded, setMobileExpanded] = useState(false);

  useEffect(() => {
    if (!mobileRail) setMobileExpanded(false);
  }, [mobileRail]);

  const effectiveCollapsed = mobileRail ? !mobileExpanded : sidebarCollapsed;
  const collapseSidebar = mobileRail
    ? () => setMobileExpanded(false)
    : toggleSidebarCollapsed;

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
    effectiveCollapsed
      ? null
      : { "--sidebar-resize-width": `${resize.width}px` }
  ) as React.CSSProperties | null;

  return (
    <aside
      className={`sidebar${effectiveCollapsed ? " sidebar-collapsed" : ""}`}
      style={asideStyle ?? undefined}
    >
      {effectiveCollapsed ? (
        <CollapsedSidebar
          onExpand={mobileRail ? () => setMobileExpanded(true) : undefined}
        />
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
                onClick={collapseSidebar}
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

          <div className="sidebar-scroll">
            {/* The sidebar nav is three labeled groups — Work / Knowledge /
                Config — rendered by AppList. Inbox lives in Work; there is no
                separate flat task list, and "Tasks" in Work opens the task
                surface. Channels are per task (reached via the task detail). */}
            <AppList />

            {/* Phase 2 onboarding preview overlay — shows staged channels/agents
                forming as the user answers CEO questions. Hidden once onboarded. */}
            <SidebarPreviewOverlay />
          </div>
          {/* WorkspaceSummary intentionally not rendered here — the stats
              it shows (agents active, tasks open, tokens) are redundant
              with the Tasks nav and the Usage footer. The component file is
              preserved so it can be re-used inside a future Usage popover
              or Settings surface. */}
          <UsagePanel />
        </>
      )}
      {!(effectiveCollapsed || mobileRail) && (
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
