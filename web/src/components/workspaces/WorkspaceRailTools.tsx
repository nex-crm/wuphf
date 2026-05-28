import { useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import type { ComponentType } from "react";
import {
  Binocular,
  Book,
  Calendar,
  ChatBubbleWarning,
  CheckCircle,
  ClipboardCheck,
  DotsGrid3x3,
  Flash,
  HomeSimple,
  Package,
  Page,
  Play,
  Search,
  Settings as SettingsIcon,
  ShareAndroid,
  Shield,
  Terminal,
} from "iconoir-react";

import { router } from "../../lib/router";
import { navigateToSidebarApp } from "../../lib/sidebarNav";
import {
  SIDEBAR_TOOLS,
  WIKI_SURFACE_APP_IDS,
} from "../../routes/routeRegistry";
import { useCurrentApp, useCurrentRoute } from "../../routes/useCurrentRoute";
import { RailIconButton, type RailHint } from "./RailIconButton";
import { useInboxCount } from "./useInboxCount";

const WIKI_SURFACE_APPS = new Set<string>(WIKI_SURFACE_APP_IDS);

// Tools that get an inline rail icon; everything else (and not "settings")
// gets tucked behind the "More tools" popover so the rail stays scannable.
const PRIMARY_TOOL_IDS = new Set<string>([
  "overview",
  "wiki",
  "calendar",
  "skills",
]);

const TOOL_ICONS: Record<string, ComponentType<{ className?: string }>> = {
  overview: Binocular,
  studio: Play,
  wiki: Book,
  console: Terminal,
  tasks: CheckCircle,
  requests: ClipboardCheck,
  graph: ShareAndroid,
  policies: Shield,
  calendar: Calendar,
  skills: Flash,
  activity: Package,
  receipts: Page,
  "health-check": Search,
  settings: SettingsIcon,
};

/**
 * Tools group rendered under the active workspace tile. Inbox sits at
 * the top, then primary tools (Overview, Wiki, Issues, Calendar,
 * Skills), then a "More tools" trigger that opens a popover with the
 * remaining secondary tools.
 */
export function WorkspaceRailTools() {
  const currentApp = useCurrentApp();
  const route = useCurrentRoute();
  const [hint, setHint] = useState<RailHint | null>(null);
  const [moreOpen, setMoreOpen] = useState(false);
  const moreTriggerRef = useRef<HTMLButtonElement | null>(null);
  const [moreAnchor, setMoreAnchor] = useState<{
    bottom: number;
    left: number;
  }>({ bottom: 16, left: 64 });

  const inboxCount = useInboxCount();
  const inboxActive = currentApp === "inbox";

  const primaryTools = useMemo(
    () =>
      SIDEBAR_TOOLS.filter(
        (t) => t.id !== "settings" && PRIMARY_TOOL_IDS.has(t.id),
      ),
    [],
  );
  const secondaryTools = useMemo(
    () =>
      SIDEBAR_TOOLS.filter(
        (t) => t.id !== "settings" && !PRIMARY_TOOL_IDS.has(t.id),
      ),
    [],
  );
  const moreActive = secondaryTools.some((t) => currentApp === t.id);

  function isToolActive(toolId: string) {
    return toolId === "wiki"
      ? WIKI_SURFACE_APPS.has(currentApp ?? "")
      : currentApp === toolId;
  }

  useEffect(() => {
    if (!moreOpen) return;
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") setMoreOpen(false);
    }
    function onDown(e: MouseEvent) {
      const target = e.target as Node | null;
      if (
        target instanceof Element &&
        (target.closest("[data-rail-more-popover]") ||
          target.closest("[data-rail-more-trigger]"))
      ) {
        return;
      }
      setMoreOpen(false);
    }
    document.addEventListener("keydown", onKey);
    document.addEventListener("mousedown", onDown);
    return () => {
      document.removeEventListener("keydown", onKey);
      document.removeEventListener("mousedown", onDown);
    };
  }, [moreOpen]);

  useEffect(() => {
    if (!moreOpen || !moreTriggerRef.current) return;
    const rect = moreTriggerRef.current.getBoundingClientRect();
    setMoreAnchor({
      bottom: window.innerHeight - rect.bottom,
      left: rect.right + 8,
    });
  }, [moreOpen]);

  return (
    <div
      data-testid="workspace-rail-tools"
      style={{
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        gap: 4,
        paddingTop: 10,
        paddingBottom: 4,
        width: "100%",
      }}
    >
      <div style={{ position: "relative" }}>
        <RailIconButton
          testId="workspace-rail-tool-inbox"
          label="Inbox"
          active={inboxActive}
          onClick={() => navigateToSidebarApp("inbox")}
          onHint={setHint}
        >
          <HomeSimple className="workspace-rail-tool-icon" />
        </RailIconButton>
        {inboxCount > 0 ? (
          <span
            aria-hidden="true"
            data-testid="inbox-unread-badge"
            style={{
              position: "absolute",
              top: -2,
              right: -2,
              minWidth: 16,
              height: 16,
              padding: "0 4px",
              borderRadius: 999,
              background: "var(--red, #e25c7a)",
              color: "#fff",
              fontSize: 10,
              fontWeight: 700,
              lineHeight: "16px",
              textAlign: "center",
              border: "2px solid var(--workspace-rail-bg, #0a0a0a)",
              boxSizing: "content-box",
            }}
          >
            {inboxCount > 9 ? "9+" : inboxCount}
          </span>
        ) : null}
      </div>

      {primaryTools.flatMap((tool) => {
        const Icon = TOOL_ICONS[tool.id];
        const btn = (
          <RailIconButton
            key={tool.id}
            testId={`workspace-rail-tool-${tool.id}`}
            label={tool.label}
            active={isToolActive(tool.id)}
            onClick={() => navigateToSidebarApp(tool.id)}
            onHint={setHint}
          >
            {Icon ? (
              <Icon className="workspace-rail-tool-icon" />
            ) : (
              <span style={{ fontSize: 14 }}>{tool.icon}</span>
            )}
          </RailIconButton>
        );
        // Issues sits between Wiki and Calendar in the rail. It's not a
        // SIDEBAR_TOOLS entry (it has its own dedicated /issues route),
        // so we render it inline here.
        if (tool.id === "wiki") {
          return [
            btn,
            <RailIconButton
              key="issues"
              testId="workspace-rail-tool-issues"
              label="Issues"
              active={
                route.kind === "issues-list" ||
                route.kind === "issue-detail" ||
                route.kind === "issue-new"
              }
              onClick={() => void router.navigate({ to: "/issues" })}
              onHint={setHint}
            >
              <ChatBubbleWarning className="workspace-rail-tool-icon" />
            </RailIconButton>,
          ];
        }
        return [btn];
      })}

      {secondaryTools.length > 0 ? (
        <RailIconButton
          ref={moreTriggerRef}
          testId="workspace-rail-more-trigger"
          label="More tools"
          active={moreOpen || moreActive}
          onClick={() => setMoreOpen((v) => !v)}
          onHint={setHint}
          dataAttrs={{ "data-rail-more-trigger": "true" }}
          ariaExpanded={moreOpen}
        >
          <DotsGrid3x3 className="workspace-rail-tool-icon" />
        </RailIconButton>
      ) : null}

      {hint && !moreOpen
        ? createPortal(
            <div
              role="tooltip"
              style={{
                position: "fixed",
                left: 56 + 6,
                top: hint.y,
                transform: "translateY(-50%)",
                padding: "5px 10px",
                background: "var(--text)",
                color: "var(--bg-card)",
                borderRadius: "var(--radius-sm)",
                fontSize: 11,
                fontWeight: 500,
                whiteSpace: "nowrap",
                pointerEvents: "none",
                zIndex: 1200,
                boxShadow: "0 4px 14px rgba(0,0,0,0.15)",
              }}
            >
              {hint.label}
            </div>,
            document.body,
          )
        : null}

      {moreOpen
        ? createPortal(
            <div
              data-rail-more-popover
              role="menu"
              aria-label="More tools"
              style={{
                position: "fixed",
                left: moreAnchor.left,
                bottom: moreAnchor.bottom,
                minWidth: 180,
                padding: 4,
                background: "var(--bg-card)",
                color: "var(--text)",
                border: "1px solid var(--border)",
                borderRadius: 10,
                boxShadow:
                  "0 1px 2px rgba(0, 0, 0, 0.06), 0 8px 20px rgba(0, 0, 0, 0.10)",
                zIndex: 1300,
                transformOrigin: "bottom left",
                animation:
                  "rail-more-popover-in 160ms cubic-bezier(0.23, 1, 0.32, 1)",
              }}
            >
              {secondaryTools.map((tool) => {
                const Icon = TOOL_ICONS[tool.id];
                const isActive = isToolActive(tool.id);
                return (
                  <button
                    key={tool.id}
                    type="button"
                    role="menuitem"
                    data-testid={`workspace-rail-tool-${tool.id}`}
                    aria-current={isActive ? "page" : undefined}
                    onClick={() => {
                      navigateToSidebarApp(tool.id);
                      setMoreOpen(false);
                    }}
                    style={{
                      display: "flex",
                      alignItems: "center",
                      gap: 8,
                      width: "100%",
                      padding: "5px 8px",
                      background: isActive ? "var(--cyan-400)" : "transparent",
                      color: isActive ? "#0a1f24" : "var(--text)",
                      border: "none",
                      borderRadius: 6,
                      cursor: "pointer",
                      textAlign: "left",
                      fontSize: 12.5,
                      fontWeight: isActive ? 600 : 500,
                      lineHeight: 1.2,
                      transition: "background 0.12s, color 0.12s",
                    }}
                    onMouseEnter={(e) => {
                      if (!isActive)
                        (
                          e.currentTarget as HTMLButtonElement
                        ).style.background = "var(--bg-warm)";
                    }}
                    onMouseLeave={(e) => {
                      if (!isActive)
                        (
                          e.currentTarget as HTMLButtonElement
                        ).style.background = "transparent";
                    }}
                  >
                    <span
                      style={{
                        width: 18,
                        height: 18,
                        display: "inline-flex",
                        alignItems: "center",
                        justifyContent: "center",
                        flexShrink: 0,
                      }}
                    >
                      {Icon ? (
                        <Icon className="sidebar-item-icon" />
                      ) : (
                        <span>{tool.icon}</span>
                      )}
                    </span>
                    <span>{tool.label}</span>
                  </button>
                );
              })}
            </div>,
            document.body,
          )
        : null}
    </div>
  );
}

/**
 * Footer pinned to the bottom of the rail. Holds the Settings shortcut.
 */
export function WorkspaceRailFooter() {
  const currentApp = useCurrentApp();
  const [hint, setHint] = useState<RailHint | null>(null);
  const settingsActive = currentApp === "settings";
  return (
    <div
      data-testid="workspace-rail-footer"
      style={{
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        paddingTop: 8,
        paddingBottom: 8,
        borderTop:
          "1px solid var(--workspace-rail-border, rgba(255, 255, 255, 0.08))",
        width: "100%",
      }}
    >
      <RailIconButton
        testId="workspace-rail-tool-settings"
        label="Settings"
        active={settingsActive}
        onClick={() => navigateToSidebarApp("settings")}
        onHint={setHint}
      >
        <SettingsIcon className="workspace-rail-tool-icon" />
      </RailIconButton>
      {hint
        ? createPortal(
            <div
              role="tooltip"
              style={{
                position: "fixed",
                left: 56 + 6,
                top: hint.y,
                transform: "translateY(-50%)",
                padding: "5px 10px",
                background: "var(--text)",
                color: "var(--bg-card)",
                borderRadius: "var(--radius-sm)",
                fontSize: 11,
                fontWeight: 500,
                whiteSpace: "nowrap",
                pointerEvents: "none",
                zIndex: 1200,
                boxShadow: "0 4px 14px rgba(0,0,0,0.15)",
              }}
            >
              {hint.label}
            </div>,
            document.body,
          )
        : null}
    </div>
  );
}
