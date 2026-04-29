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

import {
  usePauseWorkspace,
  useResumeWorkspace,
  useShredWorkspace,
  useWorkspacesList,
  type Workspace,
} from "../../api/workspaces";
import { useAppStore } from "../../stores/app";
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

const styles = {
  rail: {
    width: 56,
    flexShrink: 0,
    background: "var(--neutral-900)",
    borderRight: "1px solid var(--border)",
    display: "flex" as const,
    flexDirection: "column" as const,
    alignItems: "center" as const,
    padding: "10px 0",
    gap: 6,
    height: "100vh",
    overflowY: "auto" as const,
    color: "var(--neutral-100)",
  },
  icon: (active: boolean, paused: boolean): React.CSSProperties => ({
    width: 36,
    height: 36,
    borderRadius: 8,
    border: active ? "2px solid var(--accent)" : "2px solid transparent",
    background: paused ? "var(--neutral-700)" : "var(--neutral-600)",
    color: paused ? "var(--neutral-300)" : "white",
    fontWeight: 700,
    fontSize: 13,
    fontFamily: "var(--font-sans)",
    display: "flex",
    alignItems: "center",
    justifyContent: "center",
    cursor: "pointer",
    position: "relative",
    opacity: paused ? 0.65 : 1,
    transition: "transform 0.12s, border-color 0.12s, opacity 0.12s",
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
    borderRadius: 8,
    border: "1px dashed var(--neutral-500)",
    background: "transparent",
    color: "var(--neutral-300)",
    cursor: "pointer",
    fontSize: 18,
    lineHeight: 1,
    fontFamily: "var(--font-sans)",
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
  const setCurrentApp = useAppStore((s) => s.setCurrentApp);

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
      navigate(resp.url);
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
      const name = vars.name;
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
        // Already here — bring focus back to the office.
        setCurrentApp(null);
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
    [activeName, navigate, setCurrentApp],
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
              data-testid={`workspace-icon-${ws.name}`}
              data-state={ws.state}
              data-active={active ? "true" : "false"}
              style={styles.icon(active, paused)}
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
                <div style={{ fontWeight: 600 }}>{ws.name}</div>
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
        data-testid="workspace-add-button"
        aria-label="Create workspace"
        style={styles.addButton}
        onClick={() => setCreateOpen(true)}
      >
        +
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
            setCurrentApp("settings");
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
