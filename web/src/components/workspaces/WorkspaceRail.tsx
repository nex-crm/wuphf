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

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Plus } from "iconoir-react";

import {
  usePauseWorkspace,
  useResumeWorkspace,
  useShredWorkspace,
  useWorkspacesList,
  type Workspace,
} from "../../api/workspaces";
import { channelRoute, dmRoute, router } from "../../lib/router";
import { showNotice } from "../ui/Toast";
import { CreateWorkspaceModal } from "./CreateWorkspaceModal";
import { useRestoreToast } from "./RestoreToast";
import { ShredConfirmModal } from "./ShredConfirmModal";

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
    padding: "14px 0",
    gap: 14,
    height: "100vh",
    overflowY: "auto" as const,
    color: "var(--workspace-rail-fg, var(--neutral-100))",
  },
  icon: (
    active: boolean,
    paused: boolean,
    bg: string,
  ): React.CSSProperties => ({
    width: 36,
    height: 36,
    borderRadius: 8,
    border: active ? "2px solid var(--accent)" : "2px solid transparent",
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
  }),
  iconRow: {
    position: "relative" as const,
    display: "flex" as const,
    flexDirection: "column" as const,
    alignItems: "center" as const,
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
    zIndex: 1100,
    minWidth: 160,
    boxShadow: "0 8px 24px rgba(0,0,0,0.25)",
    fontSize: 13,
  },
  menuItem: {
    display: "block" as const,
    width: "100%",
    background: "transparent",
    border: "none",
    color: "var(--text)",
    padding: "6px 12px",
    cursor: "pointer" as const,
    textAlign: "left" as const,
    fontSize: 13,
    borderRadius: 4,
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
        <button
          type="button"
          role="menuitem"
          style={styles.menuItem}
          onClick={onPause}
        >
          Pause
        </button>
      ) : null}
      {showResume ? (
        <button
          type="button"
          role="menuitem"
          style={styles.menuItem}
          onClick={onResume}
        >
          Resume
        </button>
      ) : null}
      <button
        type="button"
        role="menuitem"
        style={styles.menuItem}
        onClick={onSettings}
      >
        Settings
      </button>
      <div style={styles.divider} aria-hidden="true" />
      <button
        type="button"
        role="menuitem"
        style={{ ...styles.menuItem, ...styles.menuItemDanger }}
        data-testid={`workspace-menu-shred-${workspace.name}`}
        onClick={onShred}
      >
        Shred…
      </button>
    </div>
  );
}

export function WorkspaceRail({
  navigate = defaultNavigate,
}: WorkspaceRailProps = {}) {
  const { data, isLoading } = useWorkspacesList();

  const [hoveredSlug, setHoveredSlug] = useState<string | null>(null);
  const [createOpen, setCreateOpen] = useState(false);
  const [kebab, setKebab] = useState<KebabState | null>(null);
  const [resume, setResume] = useState<ResumeState | null>(null);
  const [shredTarget, setShredTarget] = useState<Workspace | null>(null);

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
        showNotice(`Workspace '${name}' moved to trash.`, "info");
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

  const handleClick = useCallback(
    (ws: Workspace) => {
      if (ws.is_active || ws.name === activeName) {
        // Already here — bring focus back to the office. If the user is
        // currently on a non-conversation surface (an app panel, wiki,
        // notebooks, reviews), drop them back to #general; if they are
        // already in a conversation, this click is a no-op.
        const leaf = router.state.matches.at(-1);
        const onConversation =
          leaf?.routeId === channelRoute.id || leaf?.routeId === dmRoute.id;
        if (!onConversation) {
          void router.navigate({
            to: "/channels/$channelSlug",
            params: { channelSlug: "general" },
          });
        }
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
    [activeName, navigate],
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

      {workspaces.map((ws) => {
        const active = ws.is_active || ws.name === activeName;
        const paused = ws.state === "paused" || ws.state === "stopping";
        const showTooltip = hoveredSlug === ws.name;
        return (
          <div
            key={ws.name}
            style={styles.iconRow}
            onMouseEnter={() => setHoveredSlug(ws.name)}
            onMouseLeave={() => setHoveredSlug(null)}
          >
            <button
              type="button"
              className="workspace-rail-icon"
              data-testid={`workspace-icon-${ws.name}`}
              data-state={ws.state}
              data-active={active ? "true" : "false"}
              aria-label={`Switch to workspace ${ws.name} (${ws.state})`}
              aria-current={active ? "page" : undefined}
              style={styles.icon(active, paused, workspaceColor(ws.name))}
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
      })}

      <button
        type="button"
        className="workspace-rail-add"
        data-testid="workspace-add-button"
        aria-label="Create workspace"
        style={styles.addButton}
        onClick={() => setCreateOpen(true)}
      >
        <Plus width={16} height={16} strokeWidth={2} />
      </button>

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
