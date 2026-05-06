import { useCallback, useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Xmark } from "iconoir-react";

import type { OfficeMember } from "../../api/client";
import { createDM, post } from "../../api/client";
import { listAgentLogTasks, type TaskLogSummary } from "../../api/tasks";
import { useAgentStream } from "../../hooks/useAgentStream";
import { useDefaultHarness } from "../../hooks/useConfig";
import { useChannelMembers, useOfficeMembers } from "../../hooks/useMembers";
import { resolveHarness } from "../../lib/harness";
import { router } from "../../lib/router";
import {
  type CurrentRoute,
  useChannelSlug,
  useCurrentRoute,
} from "../../routes/useCurrentRoute";
import { useAppStore } from "../../stores/app";
import { StreamLineView } from "../messages/StreamLineView";
import { confirm } from "../ui/ConfirmDialog";
import { HarnessBadge } from "../ui/HarnessBadge";
import { PixelAvatar } from "../ui/PixelAvatar";
import { showNotice } from "../ui/Toast";

/**
 * Stable identity key for the AgentPanel "close on route change" effect.
 * Uses an explicit per-kind key instead of JSON.stringify(route) so
 * adding a new CurrentRoute kind that includes a non-string field can't
 * silently produce an unstable serialization (and the exhaustiveness
 * check forces the maintainer to update this helper).
 */
function routeIdentityKey(route: CurrentRoute): string {
  switch (route.kind) {
    case "channel":
      return `channel:${route.channelSlug}`;
    case "dm":
      return `dm:${route.agentSlug}`;
    case "app":
      return `app:${route.appId}`;
    case "task-board":
      return "task-board";
    case "task-detail":
      return `task-detail:${route.taskId}`;
    case "wiki":
      return "wiki";
    case "wiki-article":
      return `wiki-article:${route.articlePath}`;
    case "wiki-lookup":
      return `wiki-lookup:${route.query ?? ""}`;
    case "notebook-catalog":
      return "notebook-catalog";
    case "notebook-agent":
      return `notebook-agent:${route.agentSlug}`;
    case "notebook-entry":
      return `notebook-entry:${route.agentSlug}/${route.entrySlug}`;
    case "reviews":
      return "reviews";
    case "unknown":
      return "unknown";
    default: {
      const _exhaustive: never = route;
      void _exhaustive;
      return "unknown";
    }
  }
}

interface AgentPanelViewProps {
  agent: OfficeMember;
  onClose: () => void;
}

function StreamSection({ slug }: { slug: string }) {
  const { lines, connected } = useAgentStream(slug);
  const scrollRef = useRef<HTMLDivElement>(null);

  // biome-ignore lint/correctness/useExhaustiveDependencies: re-run on every new line so the log auto-scrolls.
  useEffect(() => {
    const el = scrollRef.current;
    if (el) {
      el.scrollTop = el.scrollHeight;
    }
  }, [lines.length]);

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
  const [tasks, setTasks] = useState<TaskLogSummary[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);

    listAgentLogTasks({ limit: 50 })
      .then((data) => {
        if (!cancelled) {
          const mine = (data.tasks ?? []).filter((t) => t.agentSlug === slug);
          setTasks(mine.slice(0, 10));
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

  function formatTime(ms: number | undefined): string {
    if (!ms) return "";
    try {
      return new Date(ms).toLocaleTimeString(undefined, {
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
      ) : tasks.length === 0 ? (
        <div className="agent-log-empty">No recent activity</div>
      ) : (
        tasks.map((t) => (
          <div key={t.taskId} className="agent-log-item">
            <div className="agent-log-action">
              {t.taskId} {t.hasError ? "\u26a0" : ""}
            </div>
            <div className="agent-log-content">
              {t.toolCallCount} tool call{t.toolCallCount === 1 ? "" : "s"}
            </div>
            <div className="agent-log-time">{formatTime(t.lastToolAt)}</div>
          </div>
        ))
      )}
    </div>
  );
}

// biome-ignore lint/complexity/noExcessiveCognitiveComplexity: AgentPanelView — off-conversation toggle/channel guard added in PR #634; baselined pending the follow-up panel-extraction refactor.
function AgentPanelView({ agent, onClose }: AgentPanelViewProps) {
  const setActiveAgentSlug = useAppStore((s) => s.setActiveAgentSlug);
  // Read the URL channel directly — no fallback to "general" or last-visited
  // here. The Enable/disable toggle below would otherwise silently flip the
  // agent's membership in a channel the user isn't actually looking at,
  // which is destructive. Off-conversation routes hide the toggle entirely.
  const currentChannel = useChannelSlug();
  const queryClient = useQueryClient();
  const [dmLoading, setDmLoading] = useState(false);
  const [view, setView] = useState<"stream" | "logs">("stream");
  const [toggling, setToggling] = useState(false);
  const [removing, setRemoving] = useState(false);
  const defaultHarness = useDefaultHarness();

  // Derive the per-channel enabled state. An agent is "enabled" in the
  // current channel when it appears in /members and is not flagged
  // disabled. useChannelMembers stays disabled when `currentChannel` is
  // null, so this query and the toggle UI below are hidden in lockstep
  // off conversation routes.
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
  // The toggle is per-channel; off conversation routes there is no channel
  // to scope the action to, so we hide the toggle entirely rather than
  // dispatch against a stale fallback channel.
  const canToggle = !isLead && currentChannel !== null;

  async function handleOpenDM() {
    setDmLoading(true);
    // Optimistic navigation: take the user to the DM immediately. If
    // createDM fails we surface a toast and let the user act — we do
    // not auto-revert. An earlier href-equality guard couldn't tell
    // whether the user had since back-navigated to the same DM
    // intentionally, and stomped legit navigation in long-tail timing.
    void router.navigate({
      to: "/dm/$agentSlug",
      params: { agentSlug: agent.slug },
    });
    setActiveAgentSlug(null);
    try {
      await createDM(agent.slug);
      void queryClient.invalidateQueries({ queryKey: ["channels"] });
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : "Failed to open DM";
      showNotice(message, "error");
    } finally {
      setDmLoading(false);
    }
  }

  // biome-ignore lint/complexity/noExcessiveCognitiveComplexity: handleToggleEnabled — existing cognitive complexity is baselined for a focused follow-up refactor.
  async function handleToggleEnabled(next: boolean) {
    // canToggle already gates currentChannel, but re-check here so the
    // post body is provably non-null and TypeScript narrows. The toggle
    // UI is unmounted off conversation routes, so this branch only
    // protects against a future caller wiring this handler somewhere
    // else.
    if (!canToggle || toggling || currentChannel === null) return;
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
          // Removing from /office-members affects every channel-members
          // list. Invalidate the whole `channel-members` key so each cached
          // channel refreshes — narrowing this to `currentChannel` would
          // skip refetching when the panel is open off a conversation
          // route, leaving the sidebar showing the removed agent.
          await queryClient.invalidateQueries({
            queryKey: ["channel-members"],
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
            {agent.role ? (
              <span className="agent-panel-role">{agent.role}</span>
            ) : null}
          </div>
        </div>
        <button
          type="button"
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
          {agent.status ? (
            <div className="agent-panel-info-row">
              <span className="agent-panel-info-label">status</span>
              <span className="agent-panel-info-value">{agent.status}</span>
            </div>
          ) : null}
          {agent.task ? (
            <div className="agent-panel-info-row">
              <span className="agent-panel-info-label">task</span>
              <span className="agent-panel-info-value">{agent.task}</span>
            </div>
          ) : null}
        </div>
      </div>

      {/* Enable/disable — controls whether this agent participates in
          the current conversation channel. Off conversation routes (apps,
          wiki, notebooks, …) `currentChannel` is null so this whole
          section is hidden, since the toggle would otherwise hit the
          broker against a stale fallback channel. */}
      {canToggle && currentChannel ? (
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
      ) : null}

      {/* Primary actions */}
      <div className="agent-panel-actions">
        <button
          type="button"
          className="btn btn-primary btn-sm"
          onClick={handleOpenDM}
          disabled={dmLoading}
        >
          {dmLoading ? "Opening..." : "Open DM"}
        </button>
        <button
          type="button"
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
            type="button"
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
  const route = useCurrentRoute();
  const { data: members = [] } = useOfficeMembers();
  const panelRef = useRef<HTMLDivElement>(null);

  const close = useCallback(
    () => setActiveAgentSlug(null),
    [setActiveAgentSlug],
  );

  // Close when the user navigates to a different surface. The intent is
  // "nav away from the agent panel" — driven by route changes, NOT by
  // activeAgentSlug itself (which would close on every open). The
  // identity key is per-kind explicit (not JSON-stringified) so adding
  // a non-string field to CurrentRoute can't silently produce a churning
  // serialization that closes the panel mid-interaction.
  const routeKey = routeIdentityKey(route);
  useEffect(() => {
    // routeKey is referenced via the `void` so biome's
    // useExhaustiveDependencies sees it used in-body and accepts the dep.
    // The dep IS the trigger for this effect — re-firing only when the
    // matched route identity changes — so dropping it would break the
    // close-on-navigation contract.
    void routeKey;
    close();
  }, [routeKey, close]);

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
