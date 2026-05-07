import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Xmark } from "iconoir-react";

import type { OfficeMember, Skill } from "../../api/client";
import { getChannels, getSkillsList } from "../../api/client";
import {
  getOfficeTasks,
  listAgentLogTasks,
  type Task,
  type TaskLogSummary,
} from "../../api/tasks";
import { useDefaultHarness } from "../../hooks/useConfig";
import type { HarnessKind } from "../../lib/harness";
import { resolveHarness } from "../../lib/harness";
import { router } from "../../lib/router";
import { HarnessBadge } from "../ui/HarnessBadge";
import { PixelAvatar } from "../ui/PixelAvatar";

interface AgentProfilePanelProps {
  agent: OfficeMember;
  onClose: () => void;
}

function SectionTitle({ children }: { children: React.ReactNode }) {
  return <div className="agent-profile-section-title">{children}</div>;
}

function EmptyRow({ label }: { label: string }) {
  return <div className="agent-profile-empty">{label}</div>;
}

function StatusBadge({ status }: { status: string | undefined }) {
  const s = (status || "idle").toLowerCase();
  let cls = "agent-profile-status-badge";
  if (s === "active") cls += " active";
  else if (s === "paused") cls += " paused";
  return (
    <span className={cls}>
      <span className="agent-profile-status-dot" />
      {s}
    </span>
  );
}

function SkillsSection({
  agentSlug,
  skills,
}: {
  agentSlug: string;
  skills: Skill[];
}) {
  const owned = skills.filter(
    (sk) =>
      Array.isArray(sk.owner_agents) && sk.owner_agents.includes(agentSlug),
  );
  const active = owned.filter(
    (sk) => sk.status === "active" || sk.status === "proposed",
  );

  return (
    <div className="agent-profile-section">
      <SectionTitle>skills</SectionTitle>
      {active.length === 0 ? (
        <EmptyRow label="No skills yet" />
      ) : (
        <ul className="agent-profile-list">
          {active.map((sk) => (
            <li key={sk.name} className="agent-profile-list-item">
              <span className="agent-profile-skill-name">
                {sk.title || sk.name}
              </span>
              {sk.status === "proposed" && (
                <span className="badge badge-yellow agent-profile-badge">
                  pending
                </span>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function ChannelsSection({
  agentSlug,
  channels,
}: {
  agentSlug: string;
  channels: { slug: string; name: string; members?: string[] }[];
}) {
  const memberOf = channels.filter(
    (ch) => Array.isArray(ch.members) && ch.members.includes(agentSlug),
  );

  return (
    <div className="agent-profile-section">
      <SectionTitle>channels</SectionTitle>
      {memberOf.length === 0 ? (
        <EmptyRow label="No channels" />
      ) : (
        <div className="agent-profile-chips">
          {memberOf.map((ch) => (
            <button
              key={ch.slug}
              type="button"
              className="agent-profile-chip"
              onClick={() =>
                void router.navigate({
                  to: "/channels/$channelSlug",
                  params: { channelSlug: ch.slug },
                })
              }
            >
              #{ch.name || ch.slug}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

interface RecentRunsSectionProps {
  agentSlug: string;
  runs: TaskLogSummary[];
  loading: boolean;
}

function RecentRunsSection({
  agentSlug,
  runs,
  loading,
}: RecentRunsSectionProps) {
  const agentRuns = runs.filter((r) => r.agentSlug === agentSlug).slice(0, 8);

  function handleRunClick(taskId: string) {
    // Navigate to the activity app; task detail opens from there.
    void router.navigate({ to: "/apps/$appId", params: { appId: "activity" } });
    // Shallow timeout so panel can close before navigation
    void taskId;
  }

  if (loading) {
    return (
      <div className="agent-profile-section">
        <SectionTitle>recent runs</SectionTitle>
        <EmptyRow label="Loading..." />
      </div>
    );
  }

  return (
    <div className="agent-profile-section">
      <SectionTitle>recent runs</SectionTitle>
      {agentRuns.length === 0 ? (
        <EmptyRow label="No runs yet" />
      ) : (
        <ul className="agent-profile-list">
          {agentRuns.map((r) => (
            <li
              key={r.taskId}
              className="agent-profile-list-item agent-profile-run-item"
            >
              <button
                type="button"
                className="agent-profile-run-btn"
                onClick={() => handleRunClick(r.taskId)}
              >
                <span className="agent-profile-run-id">{r.taskId}</span>
                <span className="agent-profile-run-meta">
                  {r.toolCallCount} tool call{r.toolCallCount === 1 ? "" : "s"}
                  {r.hasError ? " ⚠" : ""}
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
      {agentRuns.length > 0 && (
        <button
          type="button"
          className="agent-profile-see-all"
          onClick={() =>
            void router.navigate({
              to: "/apps/$appId",
              params: { appId: "activity" },
            })
          }
        >
          See all activity
        </button>
      )}
    </div>
  );
}

interface RecentTasksSectionProps {
  agentSlug: string;
  tasks: Task[];
}

function RecentArtifactsSection({ agentSlug, tasks }: RecentTasksSectionProps) {
  const agentTasks = tasks
    .filter((t) => t.owner === agentSlug)
    .sort((a, b) => {
      const ta = a.updated_at ?? a.created_at ?? "";
      const tb = b.updated_at ?? b.created_at ?? "";
      return tb.localeCompare(ta);
    })
    .slice(0, 5);

  return (
    <div className="agent-profile-section">
      <SectionTitle>recent tasks</SectionTitle>
      {agentTasks.length === 0 ? (
        <EmptyRow label="No recent tasks" />
      ) : (
        <ul className="agent-profile-list">
          {agentTasks.map((t) => (
            <li key={t.id} className="agent-profile-list-item">
              <button
                type="button"
                className="agent-profile-task-btn"
                onClick={() =>
                  void router.navigate({
                    to: "/tasks/$taskId",
                    params: { taskId: t.id },
                  })
                }
              >
                <span className="agent-profile-task-title">{t.title}</span>
                <span
                  className={`badge ${taskStatusBadgeClass(t.status)} agent-profile-badge`}
                >
                  {normalizeTaskStatus(t.status)}
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function normalizeTaskStatus(raw: string): string {
  const s = raw.toLowerCase().replace(/[\s-]+/g, "_");
  if (s === "completed") return "done";
  if (s === "in_progress") return "in progress";
  return s;
}

function taskStatusBadgeClass(raw: string): string {
  const s = raw.toLowerCase();
  if (s === "done" || s === "completed") return "badge-green";
  if (s === "blocked") return "badge-yellow";
  if (s === "canceled" || s === "cancelled") return "badge-muted";
  return "badge-accent";
}

function PermissionsSection({ agent }: { agent: OfficeMember }) {
  const isLead = agent.built_in === true || agent.slug === "ceo";

  return (
    <div className="agent-profile-section">
      <SectionTitle>permissions</SectionTitle>
      <div className="agent-profile-permissions">
        <div className="agent-profile-perm-row">
          <span className="agent-profile-perm-label">role</span>
          <span className="agent-profile-perm-value">
            {isLead ? "lead agent" : "team member"}
          </span>
        </div>
        <div className="agent-profile-perm-row">
          <span className="agent-profile-perm-label">removable</span>
          <span className="agent-profile-perm-value">
            {isLead ? "no" : "yes"}
          </span>
        </div>
        <div className="agent-profile-perm-row">
          <span className="agent-profile-perm-label">built-in</span>
          <span className="agent-profile-perm-value">
            {isLead ? "yes" : "no"}
          </span>
        </div>
      </div>
    </div>
  );
}

function ProviderSection({
  agent,
  defaultHarness,
}: {
  agent: OfficeMember;
  defaultHarness: HarnessKind;
}) {
  const harness = resolveHarness(agent.provider, defaultHarness);
  const providerLabel =
    typeof agent.provider === "string"
      ? agent.provider
      : agent.provider?.model
        ? `${agent.provider.kind ?? harness} / ${agent.provider.model}`
        : agent.provider?.kind || harness;

  return (
    <div className="agent-profile-section">
      <SectionTitle>runtime</SectionTitle>
      <div className="agent-profile-permissions">
        <div className="agent-profile-perm-row">
          <span className="agent-profile-perm-label">harness</span>
          <span className="agent-profile-perm-value">{harness}</span>
        </div>
        {providerLabel && providerLabel !== harness && (
          <div className="agent-profile-perm-row">
            <span className="agent-profile-perm-label">provider</span>
            <span className="agent-profile-perm-value">{providerLabel}</span>
          </div>
        )}
      </div>
    </div>
  );
}

export function AgentProfilePanel({ agent, onClose }: AgentProfilePanelProps) {
  const defaultHarness = useDefaultHarness();

  const { data: skills = [] } = useQuery({
    queryKey: ["skills-list"],
    queryFn: () => getSkillsList("all").then((r) => r.skills ?? []),
    refetchInterval: 30_000,
  });

  const { data: channels = [] } = useQuery({
    queryKey: ["channels"],
    queryFn: () => getChannels().then((r) => r.channels ?? []),
    refetchInterval: 30_000,
  });

  const { data: allTasks = [] } = useQuery({
    queryKey: ["office-tasks-profile"],
    queryFn: () =>
      getOfficeTasks({ includeDone: true }).then((r) => r.tasks ?? []),
    refetchInterval: 30_000,
  });

  const [runs, setRuns] = useState<TaskLogSummary[]>([]);
  const [runsLoading, setRunsLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    setRunsLoading(true);
    listAgentLogTasks({ limit: 100 })
      .then((data) => {
        if (!cancelled) {
          setRuns(data.tasks ?? []);
          setRunsLoading(false);
        }
      })
      .catch(() => {
        if (!cancelled) setRunsLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const statusClass = agent.status === "active" ? "active pulse" : "lurking";

  return (
    <div className="agent-panel agent-profile-panel">
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
            <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
              <StatusBadge status={agent.status} />
            </div>
          </div>
        </div>
        <button
          type="button"
          className="agent-panel-close"
          onClick={onClose}
          aria-label="Close agent profile"
        >
          <Xmark width={20} height={20} />
        </button>
      </div>

      {/* Scrollable body */}
      <div className="agent-profile-body">
        {/* Role / description */}
        {agent.role ? (
          <div className="agent-profile-section">
            <SectionTitle>role</SectionTitle>
            <p className="agent-profile-role-text">{agent.role}</p>
          </div>
        ) : null}

        {/* Current task */}
        {agent.task ? (
          <div className="agent-profile-section">
            <SectionTitle>current task</SectionTitle>
            <p className="agent-profile-current-task">{agent.task}</p>
          </div>
        ) : null}

        {/* Provider / runtime */}
        <ProviderSection agent={agent} defaultHarness={defaultHarness} />

        {/* Skills */}
        <SkillsSection agentSlug={agent.slug} skills={skills} />

        {/* Channels */}
        <ChannelsSection agentSlug={agent.slug} channels={channels} />

        {/* Recent runs */}
        <RecentRunsSection
          agentSlug={agent.slug}
          runs={runs}
          loading={runsLoading}
        />

        {/* Recent tasks (artifacts) */}
        <RecentArtifactsSection agentSlug={agent.slug} tasks={allTasks} />

        {/* Permissions */}
        <PermissionsSection agent={agent} />
      </div>
    </div>
  );
}
