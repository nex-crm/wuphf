import { useCallback, useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Xmark } from "iconoir-react";

import type { AgentLog, OfficeMember } from "../../api/client";
import { createDM, getAgentLogs, post } from "../../api/client";
import { useAgentStream } from "../../hooks/useAgentStream";
import { useDefaultHarness } from "../../hooks/useConfig";
import { useChannelMembers, useOfficeMembers } from "../../hooks/useMembers";
import { resolveHarness } from "../../lib/harness";
import { directChannelSlug, useAppStore } from "../../stores/app";
import { StreamLineView } from "../messages/StreamLineView";
import { confirm } from "../ui/ConfirmDialog";
import { HarnessBadge } from "../ui/HarnessBadge";
import { PixelAvatar } from "../ui/PixelAvatar";
import { showNotice } from "../ui/Toast";

interface AgentPanelViewProps {
  agent: OfficeMember;
  onClose: () => void;
}

function StreamSection({ slug }: { slug: string }) {
  const { lines, connected } = useAgentStream(slug);
  const scrollRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const el = scrollRef.current;
    if (el) {
      el.scrollTop = el.scrollHeight;
    }
  }, []);

  return (
    <div className="agent-panel-section">
      <div className="agent-panel-section-title">Live stream</div>
      <div className="agent-stream-status">
        <span
          className={`status-dot ${connected ? "active pulse" : "lurking"}`}
        />
        {connected ? "Connected" : "Disconnected"}
      </div>
      <div className="agent-stream-log" ref={scrollRef}>
        {lines.length === 0 ? (
          <div className="agent-stream-empty">No output yet</div>
        ) : (
          lines.map((line) => (
            <StreamLineView key={line.id} line={line} compact={true} />
          ))
        )}
      </div>
    </div>
  );
}

function LogsSection({ slug }: { slug: string }) {
  const [logs, setLogs] = useState<AgentLog[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);

    getAgentLogs({ limit: 10 })
      .then((data) => {
        if (!cancelled) {
          const agentLogs = data.logs.filter((l) => l.agent === slug);
          setLogs(agentLogs.slice(0, 10));
          setLoading(false);
        }
      })
      .catch(() => {
        if (!cancelled) setLoading(false);
      });

    return () => {
      cancelled = true;
    };
  }, [slug]);

  function formatTime(timestamp: string | undefined): string {
    if (!timestamp) return "";
    try {
      const d = new Date(timestamp);
      return d.toLocaleTimeString(undefined, {
        hour: "2-digit",
        minute: "2-digit",
      });
    } catch {
      return "";
    }
  }

  return (
    <div className="agent-panel-logs">
      <div className="agent-panel-section">
        <div className="agent-panel-section-title">Recent activity</div>
      </div>
      {loading ? (
        <div className="agent-log-empty">Loading...</div>
      ) : logs.length === 0 ? (
        <div className="agent-log-empty">No recent activity</div>
      ) : (
        logs.map((log) => (
          <div key={log.id} className="agent-log-item">
            {log.action && <div className="agent-log-action">{log.action}</div>}
            {log.content && (
              <div className="agent-log-content">{log.content}</div>
            )}
            <div className="agent-log-time">{formatTime(log.timestamp)}</div>
          </div>
        ))
      )}
    </div>
  );
}

function AgentPanelView({ agent, onClose }: AgentPanelViewProps) {
  const enterDM = useAppStore((s) => s.enterDM);
  const setActiveAgentSlug = useAppStore((s) => s.setActiveAgentSlug);
  const currentChannel = useAppStore((s) => s.currentChannel);
  const queryClient = useQueryClient();
  const [dmLoading, setDmLoading] = useState(false);
  const [view, setView] = useState<"stream" | "logs">("stream");
  const [toggling, setToggling] = useState(false);
  const [removing, setRemoving] = useState(false);
  const defaultHarness = useDefaultHarness();

  // Derive the per-channel enabled state. An agent is "enabled" in the current
  // channel when it appears in /members and is not flagged disabled.
  const { data: channelMembers = [] } = useChannelMembers(currentChannel);
  const channelEntry = channelMembers.find((m) => m.slug === agent.slug);
  const enabled = Boolean(channelEntry) && channelEntry?.disabled !== true;

  // Broker rejects remove / disable for any `built_in` member (lead agent).
  // Use `!== true` (not `!agent.built_in`) so an absent field isn't silently
  // treated as "removable" — we want explicit permission, not optimistic.
  // Keep the `ceo` literal as legacy fallback for stored rosters that
  // predate the BuiltIn field getting serialized.
  const isLead = agent.built_in === true || agent.slug === "ceo";
  const canRemove = !isLead;
  const canToggle = !isLead;

  async function handleOpenDM() {
    setDmLoading(true);
    try {
      const result = await createDM(agent.slug);
      const channel = result.slug || directChannelSlug(agent.slug);
      enterDM(agent.slug, channel);
      setActiveAgentSlug(null);
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : "Failed to open DM";
      showNotice(message, "error");
    } finally {
      setDmLoading(false);
    }
  }

  async function handleToggleEnabled(next: boolean) {
    if (!canToggle || toggling) return;
    setToggling(true);
    try {
      // Broker's `enable` action only lifts the Disabled flag — it doesn't
      // add a non-member. Translate to `add` so flipping the toggle ON does
      // what the user expects regardless of prior channel membership.
      const action = next ? (channelEntry ? "enable" : "add") : "disable";
      await post("/channel-members", {
        channel: currentChannel,
        slug: agent.slug,
        action,
      });
      await queryClient.refetchQueries({
        queryKey: ["channel-members", currentChannel],
      });
      await queryClient.invalidateQueries({ queryKey: ["office-members"] });
      showNotice(
        `${agent.name || agent.slug} ${next ? "enabled" : "disabled"}`,
        "success",
      );
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : "Toggle failed";
      showNotice(message, "error");
    } finally {
      setToggling(false);
    }
  }

  function handleRemove() {
    if (!canRemove) return;
    const label = agent.name || agent.slug;
    confirm({
      title: "Remove agent",
      message: `Remove ${label}? This cannot be undone.`,
      confirmLabel: "Remove",
      danger: true,
      onConfirm: async () => {
        setRemoving(true);
        try {
          await post("/office-members", { action: "remove", slug: agent.slug });
          await queryClient.invalidateQueries({ queryKey: ["office-members"] });
          await queryClient.invalidateQueries({
            queryKey: ["channel-members", currentChannel],
          });
          showNotice(`${label} removed`, "success");
          onClose();
        } catch (err: unknown) {
          const message = err instanceof Error ? err.message : "Remove failed";
          showNotice(message, "error");
        } finally {
          setRemoving(false);
        }
      },
    });
  }

  const statusClass = agent.status === "active" ? "active pulse" : "lurking";

  return (
    <div className="agent-panel">
      {/* Header */}
      <div className="agent-panel-header">
        <div className="agent-panel-identity">
          <div className="agent-panel-avatar avatar-with-harness">
            <PixelAvatar
              slug={agent.slug}
              size={36}
              className="pixel-avatar-panel"
            />
            <HarnessBadge
              kind={resolveHarness(agent.provider, defaultHarness)}
              size={18}
              className="harness-badge-on-avatar"
            />
          </div>
          <div
            style={{
              minWidth: 0,
              flex: 1,
              display: "flex",
              flexDirection: "column",
              gap: 2,
            }}
          >
            <div
              style={{ display: "inline-flex", alignItems: "center", gap: 6 }}
            >
              <span className="agent-panel-name">
                {agent.name || agent.slug}
              </span>
              <span
                className={`status-dot ${statusClass}`}
                style={{ marginLeft: -2 }}
              />
            </div>
            {agent.role && (
              <span className="agent-panel-role">{agent.role}</span>
            )}
          </div>
        </div>
        <button
          className="agent-panel-close"
          onClick={onClose}
          aria-label="Close agent panel"
        >
          <Xmark width={20} height={20} />
        </button>
      </div>

      {/* Info */}
      <div className="agent-panel-section">
        <div className="agent-panel-info">
          <div className="agent-panel-info-row">
            <span className="agent-panel-info-label">slug</span>
            <span className="agent-panel-info-value">{agent.slug}</span>
          </div>
          {(() => {
            const p = agent.provider;
            const label = typeof p === "string" ? p : p?.kind;
            return label ? (
              <div className="agent-panel-info-row">
                <span className="agent-panel-info-label">provider</span>
                <span className="agent-panel-info-value">{label}</span>
              </div>
            ) : null;
          })()}
          {agent.status && (
            <div className="agent-panel-info-row">
              <span className="agent-panel-info-label">status</span>
              <span className="agent-panel-info-value">{agent.status}</span>
            </div>
          )}
          {agent.task && (
            <div className="agent-panel-info-row">
              <span className="agent-panel-info-label">task</span>
              <span className="agent-panel-info-value">{agent.task}</span>
            </div>
          )}
        </div>
      </div>

      {/* Enable/disable — controls whether this agent participates in #{currentChannel} */}
      {canToggle && (
        <div className="agent-panel-section">
          <div className="agent-panel-stat">
            <span className="agent-panel-stat-label">
              Enabled in <strong>#{currentChannel}</strong>
            </span>
            <label
              className="agent-toggle"
              aria-label={`Toggle ${agent.name || agent.slug} in #${currentChannel}`}
            >
              <input
                type="checkbox"
                checked={enabled}
                disabled={toggling}
                onChange={(e) => handleToggleEnabled(e.target.checked)}
              />
              <span className="agent-toggle-slider" />
            </label>
          </div>
        </div>
      )}

      {/* Primary actions */}
      <div className="agent-panel-actions">
        <button
          className="btn btn-primary btn-sm"
          onClick={handleOpenDM}
          disabled={dmLoading}
        >
          {dmLoading ? "Opening..." : "Open DM"}
        </button>
        <button
          className="btn btn-ghost btn-sm"
          onClick={() => setView(view === "logs" ? "stream" : "logs")}
        >
          {view === "logs" ? "Live stream" : "View logs"}
        </button>
      </div>

      {/* Destructive — shown only when the broker will accept a remove */}
      {canRemove && (
        <div className="agent-panel-actions-stack">
          <button
            className="btn btn-ghost btn-sm"
            onClick={handleRemove}
            disabled={removing}
            style={{ color: "var(--red)" }}
          >
            {removing ? "Removing..." : "Remove agent"}
          </button>
        </div>
      )}

      {/* Stream or Logs */}
      {view === "stream" ? (
        <StreamSection slug={agent.slug} />
      ) : (
        <LogsSection slug={agent.slug} />
      )}
    </div>
  );
}

export function AgentPanel() {
  const activeAgentSlug = useAppStore((s) => s.activeAgentSlug);
  const setActiveAgentSlug = useAppStore((s) => s.setActiveAgentSlug);
  const currentChannel = useAppStore((s) => s.currentChannel);
  const currentApp = useAppStore((s) => s.currentApp);
  const { data: members = [] } = useOfficeMembers();
  const panelRef = useRef<HTMLDivElement>(null);

  const close = useCallback(
    () => setActiveAgentSlug(null),
    [setActiveAgentSlug],
  );

  // Close when user navigates to a different sidebar section. The intent is
  // "nav away from the agent panel" — driven by currentChannel / currentApp,
  // NOT by activeAgentSlug itself. The previous version depended on
  // activeAgentSlug and closed whenever one was set, so clicking any agent
  // instantly un-selected it and the panel never mounted (React #31 guard
  // e2e regression).
  useEffect(() => {
    close();
  }, [currentChannel, currentApp, close]);

  // Close on outside click — ignore clicks on sidebar agent items that would
  // just re-open the panel, and ignore clicks inside the panel itself.
  useEffect(() => {
    if (!activeAgentSlug) return;
    const onDown = (e: MouseEvent) => {
      const target = e.target as Node | null;
      const panel = panelRef.current;
      if (!(panel && target)) return;
      if (panel.contains(target)) return;
      const el = target as HTMLElement;
      if (el.closest?.("[data-agent-slug]")) return;
      close();
    };
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [activeAgentSlug, close]);

  if (!activeAgentSlug) return null;

  const agent = members.find((m) => m.slug === activeAgentSlug);
  if (!agent) return null;

  return (
    <div ref={panelRef} style={{ display: "contents" }}>
      <AgentPanelView agent={agent} onClose={close} />
    </div>
  );
}
