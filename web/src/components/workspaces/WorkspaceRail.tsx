// biome-ignore-all lint/a11y/useKeyWithClickEvents: Pointer handler is paired with an existing modal, image, or routed-control keyboard path; preserving current interaction model.
// biome-ignore-all lint/a11y/noStaticElementInteractions: Intentional wrapper/backdrop or SVG hover target; interactive child controls and keyboard paths are handled nearby.
/**
 * WorkspaceRail — left-edge sidebar for the multi-workspace surface.
 *
 * Always 56px wide, always visible. Renders one icon per workspace
 * registered in `~/.wuphf-spaces/registry.json` (delivered via
 * /workspaces/list). Supports:
 *
 *   - Click on a non-active running workspace → window.location.assign
 *     (page reload — see design doc "Switch Protocol — Page Reload").
 *   - Click on a paused workspace → Resume confirm modal → POST
 *     /workspaces/resume → page reload to the new URL.
 *   - Right-click / kebab menu → Pause | Resume | Restore | Shred |
 *     Settings.
 *   - "+" button → opens CreateWorkspaceModal.
 *
 * The SPA only ever talks to its served broker. Lifecycle calls go
 * through that broker; cross-broker orchestration happens server-side.
 */

import {
  forwardRef,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { createPortal } from "react-dom";
import type { ComponentType } from "react";
import {
  Binocular,
  Book,
  Calendar,
  CheckCircle,
  ClipboardCheck,
  DotsGrid3x3,
  MoreHoriz,
  Flash,
  HomeSimple,
  Package,
  Page,
  Play,
  ChatBubbleWarning,
  Plus,
  Search,
  Settings as SettingsIcon,
  ShareAndroid,
  Shield,
  Terminal,
} from "iconoir-react";

import { useQuery } from "@tanstack/react-query";
import { getInboxItems } from "../../api/lifecycle";
import { playInboxDing } from "../../lib/notificationSound";
import type { InboxItem } from "../../lib/types/inbox";

import {
  usePauseWorkspace,
  useResumeWorkspace,
  useShredWorkspace,
  useWorkspacesList,
  type Workspace,
} from "../../api/workspaces";
import { router } from "../../lib/router";
import { navigateToSidebarApp } from "../../lib/sidebarNav";
import {
  SIDEBAR_TOOLS,
  WIKI_SURFACE_APP_IDS,
} from "../../routes/routeRegistry";
import { useCurrentApp, useCurrentRoute } from "../../routes/useCurrentRoute";
import { showNotice } from "../ui/Toast";
import { CreateWorkspaceModal } from "./CreateWorkspaceModal";
import { useRestoreToast } from "./RestoreToast";
import { ShredConfirmModal } from "./ShredConfirmModal";

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

interface MenuPosition {
  x: number;
  y: number;
}

interface KebabState {
  workspace: Workspace;
  position: MenuPosition;
}

interface ResumeState {
  workspace: Workspace;
}

// Returns relative luminance per WCAG 2.1 (sRGB inputs 0-255). Expects a
// 7-char `#rrggbb` hex; anything else (3-char shorthand, named colour, CSS
// var) falls back to a neutral mid-grey so the contrast helper still picks
// a sane foreground instead of NaN.
function relativeLuminance(hex: string): number {
  if (hex.length !== 7 || !hex.startsWith("#")) return 0.5;
  const r = Number.parseInt(hex.slice(1, 3), 16) / 255;
  const g = Number.parseInt(hex.slice(3, 5), 16) / 255;
  const b = Number.parseInt(hex.slice(5, 7), 16) / 255;
  if (Number.isNaN(r) || Number.isNaN(g) || Number.isNaN(b)) return 0.5;
  const lin = (c: number) =>
    c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
}

// Pick white or near-black for the workspace initial so the WCAG AA
// 4.5:1 contrast threshold is met regardless of the palette colour.
function readableTextOn(hex: string): string {
  return relativeLuminance(hex) < 0.4 ? "white" : "#1a1a1a";
}

const styles = {
  rail: {
    width: 56,
    flexShrink: 0,
    background: "var(--workspace-rail-bg, #0a0a0a)",
    borderRight:
      "1px solid var(--workspace-rail-border, rgba(255, 255, 255, 0.1))",
    display: "flex" as const,
    flexDirection: "column" as const,
    alignItems: "center" as const,
    padding: "14px 0 0",
    height: "100vh",
    overflow: "hidden" as const,
    color: "var(--workspace-rail-fg, var(--neutral-100))",
  },
  icon: (
    active: boolean,
    paused: boolean,
    bg: string,
  ): React.CSSProperties => ({
    width: 36,
    height: 36,
    borderRadius: 10,
    border: active
      ? "2px solid var(--cyan-400)"
      : "2px solid transparent",
    background: paused
      ? "var(--workspace-rail-icon-paused-bg, rgba(255,255,255,0.08))"
      : bg,
    color: paused
      ? "var(--workspace-rail-icon-paused-fg, rgba(255,255,255,0.55))"
      : readableTextOn(bg),
    fontWeight: 700,
    fontSize: 13,
    fontFamily: "var(--font-sans)",
    display: "flex",
    alignItems: "center",
    justifyContent: "center",
    cursor: "pointer",
    position: "relative",
    opacity: paused ? 0.65 : 1,
    boxShadow: active
      ? "0 0 0 4px rgba(0, 204, 255, 0.18)"
      : "none",
    transition:
      "transform 0.16s ease, box-shadow 0.16s ease, opacity 0.16s ease",
  }),
  iconRow: {
    position: "relative" as const,
    display: "flex" as const,
    flexDirection: "column" as const,
    alignItems: "center" as const,
  },
  activeRailBar: {
    position: "absolute" as const,
    left: -14,
    top: "50%",
    transform: "translateY(-50%)",
    width: 3,
    height: 22,
    borderRadius: "0 3px 3px 0",
    background: "var(--cyan-400)",
    boxShadow: "0 0 10px rgba(0, 204, 255, 0.45)",
    pointerEvents: "none" as const,
  },
  stateDot: (state: Workspace["state"]): React.CSSProperties => {
    const color =
      state === "running"
        ? "var(--green)"
        : state === "error"
          ? "var(--red)"
          : "var(--neutral-400)";
    return {
      position: "absolute",
      bottom: 0,
      right: 0,
      width: 10,
      height: 10,
      borderRadius: 999,
      background: color,
      border: "2px solid var(--neutral-900)",
    };
  },
  addButton: {
    width: 36,
    height: 36,
    cursor: "pointer",
    fontSize: 18,
    lineHeight: 1,
    marginTop: 10,
  },
  tooltip: {
    position: "absolute" as const,
    left: 56,
    top: 0,
    background: "var(--neutral-900)",
    color: "var(--neutral-100)",
    border: "1px solid var(--border)",
    borderRadius: 6,
    padding: "6px 10px",
    fontSize: 12,
    whiteSpace: "nowrap" as const,
    zIndex: 200,
    boxShadow: "0 6px 20px rgba(0,0,0,0.4)",
  },
  menu: {
    position: "fixed" as const,
    background: "var(--bg-card)",
    color: "var(--text)",
    border: "1px solid var(--border)",
    borderRadius: 8,
    padding: 4,
    zIndex: 1400,
    minWidth: 160,
    boxShadow: "0 8px 24px rgba(0,0,0,0.25)",
    fontSize: 13,
  },
  menuItem: {
    display: "flex" as const,
    alignItems: "center" as const,
    width: "100%",
    background: "transparent",
    border: "none",
    color: "var(--text)",
    padding: "6px 10px",
    cursor: "pointer" as const,
    textAlign: "left" as const,
    fontSize: 13,
    borderRadius: 6,
    transition: "background 0.12s, color 0.12s",
  },
  menuItemDanger: {
    color: "var(--red)",
  },
  divider: {
    height: 1,
    background: "var(--border)",
    margin: "4px 0",
  },
  resumeOverlay: {
    position: "fixed" as const,
    inset: 0,
    background: "rgba(0,0,0,0.6)",
    display: "flex" as const,
    alignItems: "center" as const,
    justifyContent: "center" as const,
    zIndex: 1000,
  },
  resumePanel: {
    width: "min(420px, calc(100vw - 40px))",
    background: "var(--bg-card)",
    border: "1px solid var(--border)",
    borderRadius: "var(--radius-md)",
    padding: 24,
    boxShadow: "0 20px 60px rgba(0,0,0,0.4)",
  },
};

function relativeFromNow(ts?: string | null): string {
  if (!ts) return "never used";
  const d = new Date(ts);
  if (Number.isNaN(d.getTime())) return ts;
  const diff = Date.now() - d.getTime();
  const minutes = Math.floor(diff / 60_000);
  if (minutes < 1) return "just now";
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}

function workspaceInitial(name: string): string {
  const ch = name.replace(/[^a-z0-9]/gi, "")[0] ?? "?";
  return ch.toUpperCase();
}

// Palette of similar-brightness hues — picked from the design system tokens'
// 500-band so workspace tiles feel like siblings, not a rainbow.
const WORKSPACE_PALETTE = [
  "#069de4", // cyan-500
  "#9f4dbf", // purple
  "#e0833e", // amber
  "#3aa76d", // green
  "#e25c7a", // rose
  "#5a7bd9", // indigo
  "#d4a017", // ochre
  "#4cb6ad", // teal
];

function workspaceColor(name: string): string {
  let hash = 0;
  for (let i = 0; i < name.length; i++) {
    hash = (hash * 31 + name.charCodeAt(i)) >>> 0;
  }
  return WORKSPACE_PALETTE[hash % WORKSPACE_PALETTE.length];
}

interface WorkspaceRailProps {
  /** Optional override for tests — defaults to window.location.assign. */
  navigate?: (url: string) => void;
}

/**
 * Page-reload navigator. Default uses window.location.assign so the new
 * SPA loads fresh from the target broker (no cross-broker auth in the
 * SPA — see design doc).
 */
function defaultNavigate(url: string) {
  window.location.assign(url);
}

interface ResumePromptProps {
  workspace: Workspace;
  pending: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}

/** Resume confirmation overlay — split out so the rail body stays compact. */
function ResumePrompt({
  workspace,
  pending,
  onCancel,
  onConfirm,
}: ResumePromptProps) {
  return (
    <div
      style={styles.resumeOverlay}
      role="dialog"
      aria-modal="true"
      onClick={(e) => {
        if (e.target === e.currentTarget) onCancel();
      }}
      data-testid="workspace-resume-modal"
    >
      <div style={styles.resumePanel} className="card">
        <h3 style={{ fontSize: 16, fontWeight: 700, marginBottom: 8 }}>
          Resume &lsquo;{workspace.name}&rsquo;?
        </h3>
        <p
          style={{
            fontSize: 13,
            color: "var(--text-secondary)",
            marginBottom: 16,
          }}
        >
          Spawns the broker on port {workspace.broker_port} and opens the
          workspace in this tab.
        </p>
        <div style={{ display: "flex", gap: 8, justifyContent: "flex-end" }}>
          <button
            type="button"
            className="btn btn-ghost btn-sm"
            onClick={onCancel}
            disabled={pending}
          >
            Cancel
          </button>
          <button
            type="button"
            className="btn btn-primary btn-sm"
            disabled={pending}
            data-testid="workspace-resume-confirm"
            onClick={onConfirm}
          >
            {pending ? "Resuming..." : "Resume"}
          </button>
        </div>
      </div>
    </div>
  );
}

interface KebabMenuProps {
  workspace: Workspace;
  position: MenuPosition;
  onPause: () => void;
  onResume: () => void;
  onSettings: () => void;
  onShred: () => void;
}

interface MenuItemProps {
  onClick: () => void;
  children: React.ReactNode;
  danger?: boolean;
  "data-testid"?: string;
}

function MenuItem({
  onClick,
  children,
  danger,
  "data-testid": testId,
}: MenuItemProps) {
  return (
    <button
      type="button"
      role="menuitem"
      data-testid={testId}
      onClick={onClick}
      style={{
        ...styles.menuItem,
        ...(danger ? styles.menuItemDanger : null),
      }}
      onMouseEnter={(e) => {
        (e.currentTarget as HTMLButtonElement).style.background = danger
          ? "rgba(226, 92, 122, 0.12)"
          : "var(--bg-warm)";
      }}
      onMouseLeave={(e) => {
        (e.currentTarget as HTMLButtonElement).style.background = "transparent";
      }}
      onFocus={(e) => {
        (e.currentTarget as HTMLButtonElement).style.background = danger
          ? "rgba(226, 92, 122, 0.12)"
          : "var(--bg-warm)";
      }}
      onBlur={(e) => {
        (e.currentTarget as HTMLButtonElement).style.background = "transparent";
      }}
    >
      {children}
    </button>
  );
}

/** Right-click context menu over a workspace icon. */
function KebabMenu({
  workspace,
  position,
  onPause,
  onResume,
  onSettings,
  onShred,
}: KebabMenuProps) {
  const showResume =
    workspace.state === "paused" ||
    workspace.state === "never_started" ||
    workspace.state === "error";
  return (
    <div
      data-workspace-menu={true}
      data-testid={`workspace-menu-${workspace.name}`}
      role="menu"
      style={{
        ...styles.menu,
        left: Math.min(position.x, window.innerWidth - 200),
        top: Math.min(position.y, window.innerHeight - 220),
      }}
    >
      {workspace.state === "running" ? (
        <MenuItem onClick={onPause}>Pause</MenuItem>
      ) : null}
      {showResume ? (
        <MenuItem onClick={onResume}>Resume</MenuItem>
      ) : null}
      <MenuItem onClick={onSettings}>Settings</MenuItem>
      <div style={styles.divider} aria-hidden="true" />
      <MenuItem
        danger
        data-testid={`workspace-menu-shred-${workspace.name}`}
        onClick={onShred}
      >
        Shred…
      </MenuItem>
    </div>
  );
}

/**
 * Bottom of the workspace rail:
 *   • Every tool from `SIDEBAR_TOOLS` (Overview, Wiki, Console, …) as a
 *     compact 36×36 icon button stack.
 *   • Settings sits at the very end, after a thin divider.
 *
 * Hovering a button portals a tooltip to the right of the rail so the
 * label is visible without unmounting the rail's overflow clipping.
 */
const ATTENTION_TASK_STATES = new Set([
  "decision",
  "review",
  "changes_requested",
  "blocked_on_pr_merge",
]);

function isAttentionItem(item: InboxItem): boolean {
  if (item.kind === "request" || item.kind === "review") return true;
  if (item.kind === "task") {
    const state = item.task?.state ?? "";
    return ATTENTION_TASK_STATES.has(state);
  }
  return false;
}

function useInboxCount(): number {
  const { data } = useQuery({
    queryKey: ["inbox-badge"],
    queryFn: () => getInboxItems("all"),
    refetchInterval: 5_000,
  });
  const count = useMemo(() => {
    const items = data?.items ?? [];
    let total = 0;
    for (const item of items) {
      if (isAttentionItem(item)) total += 1;
    }
    return total;
  }, [data]);
  const lastCountRef = useRef<number | null>(null);
  useEffect(() => {
    const prev = lastCountRef.current;
    if (prev !== null && count > prev) playInboxDing();
    lastCountRef.current = count;
  }, [count]);
  return count;
}

function WorkspaceRailFooter() {
  const currentApp = useCurrentApp();
  const [hint, setHint] = useState<{ label: string; y: number } | null>(null);
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

function WorkspaceRailTools() {
  const currentApp = useCurrentApp();
  const route = useCurrentRoute();
  const [hint, setHint] = useState<{ label: string; y: number } | null>(null);
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
              active={route.kind === "issues-list" || route.kind === "issue-detail" || route.kind === "issue-new"}
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
                      background: isActive
                        ? "var(--cyan-400)"
                        : "transparent",
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

interface RailIconButtonProps {
  testId: string;
  label: string;
  active: boolean;
  onClick: () => void;
  onHint: (hint: { label: string; y: number } | null) => void;
  children: React.ReactNode;
  dataAttrs?: Record<string, string>;
  ariaExpanded?: boolean;
  /** Override the unselected icon colour. Defaults to the neutral rail tone. */
  idleColor?: string;
}

const RailIconButton = forwardRef<HTMLButtonElement, RailIconButtonProps>(
  function RailIconButton(
    {
      testId,
      label,
      active,
      onClick,
      onHint,
      children,
      dataAttrs,
      ariaExpanded,
      idleColor,
    },
    ref,
  ) {
    return (
      <button
        ref={ref}
        type="button"
        data-testid={testId}
        aria-label={label}
        aria-current={active ? "page" : undefined}
        aria-expanded={ariaExpanded}
        onClick={onClick}
        onMouseEnter={(e) => {
          const rect = (
            e.currentTarget as HTMLButtonElement
          ).getBoundingClientRect();
          onHint({ label, y: rect.top + rect.height / 2 });
        }}
        onMouseLeave={() => onHint(null)}
        onFocus={(e) => {
          const rect = (
            e.currentTarget as HTMLButtonElement
          ).getBoundingClientRect();
          onHint({ label, y: rect.top + rect.height / 2 });
        }}
        onBlur={() => onHint(null)}
        {...(dataAttrs ?? {})}
        style={{
          width: 36,
          height: 36,
          borderRadius: 10,
          border: "1px solid transparent",
          background: active
            ? "var(--cyan-400)"
            : "transparent",
          color: active
            ? "#0a1f24"
            : (idleColor ?? "var(--neutral-300, rgba(255,255,255,0.7))"),
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          cursor: "pointer",
          transition: "background 0.16s ease, color 0.16s ease",
        }}
        onMouseOver={(e) => {
          if (!active)
            (e.currentTarget as HTMLButtonElement).style.background =
              "rgba(255,255,255,0.12)";
        }}
        onMouseOut={(e) => {
          if (!active)
            (e.currentTarget as HTMLButtonElement).style.background =
              "transparent";
        }}
      >
        {children}
      </button>
    );
  },
);

export function WorkspaceRail({
  navigate = defaultNavigate,
}: WorkspaceRailProps = {}) {
  const { data, isLoading } = useWorkspacesList();

  const [hoveredSlug, setHoveredSlug] = useState<string | null>(null);
  const [createOpen, setCreateOpen] = useState(false);
  const [kebab, setKebab] = useState<KebabState | null>(null);
  const [resume, setResume] = useState<ResumeState | null>(null);
  const [shredTarget, setShredTarget] = useState<Workspace | null>(null);
  const [switcherOpen, setSwitcherOpen] = useState(false);
  const activeTileRef = useRef<HTMLButtonElement | null>(null);

  const restoreToast = useRestoreToast();

  const pauseMutation = usePauseWorkspace({
    onError: (err) =>
      showNotice(
        err instanceof Error ? `Pause failed: ${err.message}` : "Pause failed.",
        "error",
      ),
    onSuccess: () => showNotice("Workspace paused.", "info"),
  });

  const resumeMutation = useResumeWorkspace({
    onSuccess: (resp) => {
      setResume(null);
      // Broker returns {ok, name}; resolve the URL from the cached
      // workspace list (which the mutation just invalidated) by name.
      const ws = data?.workspaces.find((w) => w.name === resp.name);
      if (ws?.web_port) {
        navigate(`http://localhost:${ws.web_port}/`);
      } else {
        showNotice(`Workspace '${resp.name}' resumed.`, "info");
      }
    },
    onError: (err) =>
      showNotice(
        err instanceof Error
          ? `Resume failed: ${err.message}`
          : "Resume failed.",
        "error",
      ),
  });

  const shredMutation = useShredWorkspace({
    onSuccess: (resp, vars) => {
      const { name } = vars;
      setShredTarget(null);
      if (vars.permanent) {
        showNotice(`Workspace '${name}' shredded permanently.`, "info");
        return;
      }
      if (resp.trash_id) {
        restoreToast.fire(name, resp.trash_id);
      } else {
        showNotice(`Workspace '${name}' backed up and shredded.`, "info");
      }
    },
    onError: (err) =>
      showNotice(
        err instanceof Error ? `Shred failed: ${err.message}` : "Shred failed.",
        "error",
      ),
  });

  // Close any open kebab menu on Esc / outside click.
  const railRef = useRef<HTMLElement | null>(null);
  useEffect(() => {
    if (!kebab) return;
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") setKebab(null);
    }
    function onClick(e: MouseEvent) {
      const node = e.target as Node;
      if (
        node &&
        !(node instanceof Element && node.closest("[data-workspace-menu]"))
      ) {
        setKebab(null);
      }
    }
    document.addEventListener("keydown", onKey);
    document.addEventListener("mousedown", onClick);
    return () => {
      document.removeEventListener("keydown", onKey);
      document.removeEventListener("mousedown", onClick);
    };
  }, [kebab]);

  const workspaces = useMemo(() => data?.workspaces ?? [], [data?.workspaces]);
  const activeName = data?.active;
  const activeWorkspace = useMemo(
    () =>
      workspaces.find((w) => w.is_active || w.name === activeName) ??
      workspaces[0],
    [workspaces, activeName],
  );
  const otherWorkspaces = useMemo(
    () => workspaces.filter((w) => w.name !== activeWorkspace?.name),
    [workspaces, activeWorkspace],
  );

  // Close switcher popover on Esc / outside click.
  useEffect(() => {
    if (!switcherOpen) return;
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") setSwitcherOpen(false);
    }
    function onDown(e: MouseEvent) {
      const target = e.target as Node | null;
      if (
        target instanceof Element &&
        (target.closest("[data-workspace-switcher]") ||
          target.closest("[data-workspace-active-tile]"))
      ) {
        return;
      }
      setSwitcherOpen(false);
    }
    document.addEventListener("keydown", onKey);
    document.addEventListener("mousedown", onDown);
    return () => {
      document.removeEventListener("keydown", onKey);
      document.removeEventListener("mousedown", onDown);
    };
  }, [switcherOpen]);

  const handleClick = useCallback(
    (ws: Workspace) => {
      if (
        ws.is_active ||
        ws.name === activeName ||
        ws.name === activeWorkspace?.name
      ) {
        // Active tile → toggle the switcher popover.
        setSwitcherOpen((v) => !v);
        return;
      }
      if (ws.state === "running") {
        navigate(`http://localhost:${ws.web_port}/`);
        return;
      }
      if (ws.state === "paused" || ws.state === "never_started") {
        setResume({ workspace: ws });
        return;
      }
      if (ws.state === "error") {
        showNotice(
          `Workspace '${ws.name}' is in an error state. Run 'wuphf workspace doctor' to investigate.`,
          "error",
        );
        return;
      }
      // starting / stopping — just notify; user will retry.
      showNotice(`Workspace '${ws.name}' is ${ws.state}.`, "info");
    },
    [activeName, activeWorkspace, navigate],
  );

  const openKebab = useCallback((ws: Workspace, x: number, y: number) => {
    setKebab({ workspace: ws, position: { x, y } });
  }, []);

  return (
    <aside
      ref={railRef}
      className="workspace-rail"
      style={styles.rail}
      data-testid="workspace-rail"
      aria-label="Workspace switcher"
    >
      {isLoading && workspaces.length === 0 ? (
        <div style={{ fontSize: 11, color: "var(--neutral-400)" }}>...</div>
      ) : null}

      {activeWorkspace
        ? (() => {
            const ws = activeWorkspace;
            const paused = ws.state === "paused" || ws.state === "stopping";
            const showTooltip = !switcherOpen && hoveredSlug === ws.name;
            return (
              <div
                data-workspace-active-tile
                style={styles.iconRow}
                onMouseEnter={() => setHoveredSlug(ws.name)}
                onMouseLeave={() => setHoveredSlug(null)}
              >
                <span aria-hidden="true" style={styles.activeRailBar} />
                <button
                  ref={activeTileRef}
                  type="button"
                  className="workspace-rail-icon"
                  data-testid={`workspace-icon-${ws.name}`}
                  data-state={ws.state}
                  data-active="true"
                  aria-haspopup="menu"
                  aria-expanded={switcherOpen}
                  aria-label={`Workspace ${ws.name} (${ws.state}) — open switcher`}
                  style={styles.icon(true, paused, workspaceColor(ws.name))}
                  title={`${ws.name} · ${ws.state}`}
                  onClick={() => handleClick(ws)}
                  onContextMenu={(e) => {
                    e.preventDefault();
                    openKebab(ws, e.clientX, e.clientY);
                  }}
                >
                  {workspaceInitial(ws.name)}
                  <span aria-hidden="true" style={styles.stateDot(ws.state)} />
                </button>
                {showTooltip ? (
                  <div role="tooltip" style={styles.tooltip}>
                    <div style={{ fontWeight: 600 }}>
                      {ws.company_name?.trim() || ws.name}
                    </div>
                    {ws.company_name?.trim() &&
                    ws.company_name.trim() !== ws.name ? (
                      <div
                        style={{
                          fontSize: 11,
                          color: "var(--neutral-500)",
                          marginTop: 1,
                        }}
                      >
                        {ws.name}
                      </div>
                    ) : null}
                    <div
                      style={{
                        fontSize: 11,
                        color: "var(--neutral-400)",
                        marginTop: 2,
                      }}
                    >
                      {ws.state} · {relativeFromNow(ws.last_used_at)}
                    </div>
                  </div>
                ) : null}
              </div>
            );
          })()
        : null}

      {switcherOpen
        ? createPortal(
            <div
              role="dialog"
              aria-modal="true"
              aria-label="Switch workspace"
              onClick={(e) => {
                if (e.target === e.currentTarget) setSwitcherOpen(false);
              }}
              style={{
                position: "fixed",
                inset: 0,
                background: "rgba(0,0,0,0.55)",
                display: "flex",
                alignItems: "center",
                justifyContent: "center",
                zIndex: 1300,
                animation: "sidebar-rail-popover-in 0.14s ease-out",
              }}
            >
              <div
                data-workspace-switcher
                style={{
                  width: "min(360px, calc(100vw - 40px))",
                  maxHeight: "calc(100vh - 80px)",
                  background: "var(--bg-card)",
                  color: "var(--text)",
                  border: "1px solid var(--border)",
                  borderRadius: 20,
                  boxShadow:
                    "0 1px 2px rgba(0, 0, 0, 0.06), 0 12px 32px rgba(0, 0, 0, 0.12)",
                  overflow: "hidden",
                  display: "flex",
                  flexDirection: "column",
                }}
              >
                <div
                  style={{
                    padding: "16px 20px 8px",
                    fontSize: 14,
                    fontWeight: 700,
                    color: "var(--text)",
                  }}
                >
                  Switch workspace
                </div>
                <div
                  style={{
                    padding: "4px 8px 8px",
                    overflowY: "auto",
                    flex: "1 1 auto",
                  }}
                >
                  {otherWorkspaces.length === 0 ? (
                    <div
                      style={{
                        padding: "8px 12px",
                        fontSize: 12,
                        color: "var(--text-tertiary)",
                      }}
                    >
                      No other workspaces.
                    </div>
                  ) : (
                    otherWorkspaces.map((ws) => {
                      const paused =
                        ws.state === "paused" || ws.state === "stopping";
                      const bg = workspaceColor(ws.name);
                      return (
                        <div
                          key={ws.name}
                          style={{
                            display: "flex",
                            alignItems: "center",
                            gap: 12,
                            width: "100%",
                            padding: "8px",
                            borderRadius: 12,
                            cursor: "pointer",
                            color: "var(--text)",
                            transition: "background 0.12s",
                          }}
                          onMouseEnter={(e) => {
                            (e.currentTarget as HTMLDivElement).style.background =
                              "var(--bg-warm)";
                          }}
                          onMouseLeave={(e) => {
                            (e.currentTarget as HTMLDivElement).style.background =
                              "transparent";
                          }}
                        >
                          <button
                            type="button"
                            role="menuitem"
                            data-testid={`workspace-switcher-item-${ws.name}`}
                            onClick={() => {
                              setSwitcherOpen(false);
                              handleClick(ws);
                            }}
                            style={{
                              display: "flex",
                              alignItems: "center",
                              gap: 12,
                              flex: "1 1 auto",
                              minWidth: 0,
                              background: "transparent",
                              border: "none",
                              padding: 0,
                              cursor: "pointer",
                              textAlign: "left",
                              color: "inherit",
                            }}
                          >
                            <span
                              aria-hidden="true"
                              style={{
                                width: 36,
                                height: 36,
                                borderRadius: 10,
                                background: paused
                                  ? "var(--workspace-rail-icon-paused-bg, rgba(255,255,255,0.08))"
                                  : bg,
                                color: paused
                                  ? "var(--workspace-rail-icon-paused-fg, rgba(255,255,255,0.55))"
                                  : readableTextOn(bg),
                                fontWeight: 700,
                                fontSize: 14,
                                display: "flex",
                                alignItems: "center",
                                justifyContent: "center",
                                flexShrink: 0,
                              }}
                            >
                              {workspaceInitial(ws.name)}
                            </span>
                            <span style={{ minWidth: 0, flex: "1 1 auto" }}>
                              <span
                                style={{
                                  display: "block",
                                  fontSize: 13,
                                  fontWeight: 600,
                                  whiteSpace: "nowrap",
                                  overflow: "hidden",
                                  textOverflow: "ellipsis",
                                }}
                              >
                                {ws.company_name?.trim() || ws.name}
                              </span>
                              <span
                                style={{
                                  display: "block",
                                  fontSize: 11,
                                  color: "var(--text-tertiary)",
                                }}
                              >
                                {ws.state}
                              </span>
                            </span>
                          </button>
                          <button
                            type="button"
                            aria-label={`More actions for ${ws.name}`}
                            data-testid={`workspace-switcher-menu-${ws.name}`}
                            onClick={(e) => {
                              e.stopPropagation();
                              const rect = (
                                e.currentTarget as HTMLButtonElement
                              ).getBoundingClientRect();
                              openKebab(ws, rect.right, rect.bottom);
                            }}
                            style={{
                              width: 28,
                              height: 28,
                              borderRadius: 8,
                              background: "transparent",
                              border: "none",
                              color: "var(--text-tertiary)",
                              display: "inline-flex",
                              alignItems: "center",
                              justifyContent: "center",
                              cursor: "pointer",
                              flexShrink: 0,
                            }}
                            onMouseEnter={(e) => {
                              (e.currentTarget as HTMLButtonElement).style.background =
                                "rgba(0,0,0,0.06)";
                              (e.currentTarget as HTMLButtonElement).style.color =
                                "var(--text)";
                            }}
                            onMouseLeave={(e) => {
                              (e.currentTarget as HTMLButtonElement).style.background =
                                "transparent";
                              (e.currentTarget as HTMLButtonElement).style.color =
                                "var(--text-tertiary)";
                            }}
                          >
                            <MoreHoriz width={16} height={16} />
                          </button>
                        </div>
                      );
                    })
                  )}
                  <button
                    type="button"
                    role="menuitem"
                    data-testid="workspace-switcher-create"
                    onClick={() => {
                      setSwitcherOpen(false);
                      setCreateOpen(true);
                    }}
                    style={{
                      display: "flex",
                      alignItems: "center",
                      gap: 12,
                      width: "100%",
                      padding: "8px",
                      background: "transparent",
                      border: "none",
                      borderRadius: 12,
                      cursor: "pointer",
                      textAlign: "left",
                      color: "var(--text)",
                      fontSize: 13,
                    }}
                    onMouseEnter={(e) => {
                      (e.currentTarget as HTMLButtonElement).style.background =
                        "var(--bg-warm)";
                    }}
                    onMouseLeave={(e) => {
                      (e.currentTarget as HTMLButtonElement).style.background =
                        "transparent";
                    }}
                  >
                    <span
                      aria-hidden="true"
                      style={{
                        width: 36,
                        height: 36,
                        borderRadius: 10,
                        border:
                          "1px dashed var(--border-strong, var(--border))",
                        display: "flex",
                        alignItems: "center",
                        justifyContent: "center",
                        color: "var(--text-tertiary)",
                        flexShrink: 0,
                      }}
                    >
                      <Plus width={16} height={16} strokeWidth={2} />
                    </span>
                    New workspace
                  </button>
                </div>
              </div>
            </div>,
            document.body,
          )
        : null}

      <button
        type="button"
        className="workspace-rail-add"
        data-testid="workspace-add-button"
        aria-label="Create workspace"
        style={{ ...styles.addButton, display: "none" }}
        onClick={() => setCreateOpen(true)}
      >
        <Plus width={16} height={16} strokeWidth={2} />
      </button>

      <WorkspaceRailTools />

      <div style={{ flex: "1 1 auto" }} />

      <WorkspaceRailFooter />

      <CreateWorkspaceModal
        open={createOpen}
        onClose={() => setCreateOpen(false)}
      />

      {resume ? (
        <ResumePrompt
          workspace={resume.workspace}
          pending={resumeMutation.isPending}
          onCancel={() => setResume(null)}
          onConfirm={() =>
            resumeMutation.mutate({ name: resume.workspace.name })
          }
        />
      ) : null}

      {shredTarget ? (
        <ShredConfirmModal
          workspace={shredTarget}
          busy={shredMutation.isPending}
          onCancel={() => setShredTarget(null)}
          onConfirm={({ permanent }) =>
            shredMutation.mutate({ name: shredTarget.name, permanent })
          }
        />
      ) : null}

      {kebab ? (
        <KebabMenu
          workspace={kebab.workspace}
          position={kebab.position}
          onPause={() => {
            pauseMutation.mutate({ name: kebab.workspace.name });
            setKebab(null);
          }}
          onResume={() => {
            setResume({ workspace: kebab.workspace });
            setKebab(null);
          }}
          onSettings={() => {
            // The kebab menu opens for `kebab.workspace`, which may not
            // be the workspace the user is currently in. Routing through
            // the SPA's local router would always open this workspace's
            // settings page, which is wrong (and can land edits in the
            // wrong workspace). When the kebab targets a different
            // workspace, page-reload to that broker's /apps/settings
            // instead — same protocol the workspace icon uses for
            // switching tabs.
            const isCurrent =
              kebab.workspace.is_active === true ||
              kebab.workspace.name === activeName;
            if (isCurrent) {
              void router.navigate({
                to: "/apps/$appId",
                params: { appId: "settings" },
              });
            } else if (
              kebab.workspace.state === "running" &&
              kebab.workspace.web_port
            ) {
              navigate(
                `http://localhost:${kebab.workspace.web_port}/#/apps/settings`,
              );
            } else {
              showNotice(
                `Workspace '${kebab.workspace.name}' is ${kebab.workspace.state}; resume it before opening Settings.`,
                "info",
              );
            }
            setKebab(null);
          }}
          onShred={() => {
            setShredTarget(kebab.workspace);
            setKebab(null);
          }}
        />
      ) : null}
    </aside>
  );
}
