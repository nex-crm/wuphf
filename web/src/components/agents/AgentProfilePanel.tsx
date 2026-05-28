import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Lock, Xmark } from "iconoir-react";

import type {
  LLMRuntimeKind,
  OfficeMember,
  ProviderBinding,
  Skill,
} from "../../api/client";
import {
  getChannels,
  getConfig,
  getSkillsList,
  isGatewayBinding,
  post,
} from "../../api/client";
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

const PROVIDER_LABELS: Record<LLMRuntimeKind, string> = {
  "claude-code": "Claude Code",
  codex: "Codex",
  opencode: "Opencode",
  "mlx-lm": "MLX-LM",
  ollama: "Ollama",
  exo: "Exo",
};

const GATEWAY_LABELS: Record<string, string> = {
  openclaw: "OpenClaw",
  "openclaw-http": "OpenClaw HTTP",
  "hermes-agent": "Hermes",
};

interface AgentProfilePanelProps {
  agent: OfficeMember;
  onClose: () => void;
}

function arrayOrEmpty<T>(value: unknown): T[] {
  if (!Array.isArray(value)) return [];
  return value.filter(
    (item): item is T => item !== null && typeof item === "object",
  );
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
  runs: TaskLogSummary[];
  loading: boolean;
}

function RecentRunsSection({ runs, loading }: RecentRunsSectionProps) {
  const agentRuns = runs;

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
              <div className="agent-profile-run-row">
                <span className="agent-profile-run-id">{r.taskId}</span>
                <span className="agent-profile-run-meta">
                  {r.toolCallCount} tool call{r.toolCallCount === 1 ? "" : "s"}
                  {r.hasError ? " ⚠" : ""}
                </span>
              </div>
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

function bindingFromMember(
  provider: OfficeMember["provider"],
): ProviderBinding {
  if (!provider) return {};
  if (typeof provider === "string") {
    // String form is a legacy shape carrying just the kind. Widen to the
    // union so downstream code can read `binding.kind` uniformly.
    return { kind: provider as ProviderBinding["kind"] };
  }
  return provider;
}

// RuntimeSection is the per-agent runtime picker — the surface the user task
// description calls out as "enable [provider selection] in the agent's
// settings in the UI." Three rendering paths:
//
//  1. Gateway-bound agent (kind ∈ {openclaw, openclaw-http, hermes-agent}):
//     no picker. We render a "Managed by <Gateway>" pill instead because the
//     gateway transport is load-bearing — flipping the kind here would orphan
//     the agent from its imported session. Editing a gateway-bound agent's
//     runtime is the Integrations app's job, not this panel's.
//
//  2. Global runtime unlocked (Settings.LLMProviderUnlocked === true):
//     picker rendered but read-only with a "Locked by global override" banner,
//     because the dispatch resolver will ignore the per-agent binding anyway.
//     We display the value so the user can see what's stored — they just
//     can't write while the global is overriding.
//
//  3. Normal: editable picker with provider + model fields and a Save button.
function RuntimeSection({
  agent,
  defaultHarness,
}: {
  agent: OfficeMember;
  defaultHarness: HarnessKind;
}) {
  const queryClient = useQueryClient();
  const configQuery = useQuery({
    queryKey: ["config"],
    queryFn: getConfig,
    staleTime: 30_000,
  });
  const llmKinds: LLMRuntimeKind[] = (configQuery.data?.llm_provider_kinds ?? [
    "claude-code",
    "codex",
    "opencode",
    "mlx-lm",
    "ollama",
    "exo",
  ]) as LLMRuntimeKind[];
  const globalDefault = configQuery.data?.llm_provider ?? "claude-code";

  const binding = bindingFromMember(agent.provider);
  const isGateway = isGatewayBinding(agent.provider);
  const harness = resolveHarness(agent.provider, defaultHarness);

  const [draftKind, setDraftKind] = useState<"" | LLMRuntimeKind>(
    (binding.kind && llmKinds.includes(binding.kind as LLMRuntimeKind)
      ? (binding.kind as LLMRuntimeKind)
      : "") as "" | LLMRuntimeKind,
  );
  const [draftModel, setDraftModel] = useState<string>(binding.model ?? "");
  const [saveError, setSaveError] = useState<string | null>(null);

  // Lead agents (CEO, other built-ins) can have their runtime changed from
  // this panel too. The broker's built-in gate only fires on remove, not on
  // provider updates — there's no broker-side reason to block here.
  // Gateway-bound agents are the only non-editable case: their gateway
  // transport is load-bearing, so changing the kind through this panel
  // would orphan the imported session. Those go through Integrations.
  const editable = !isGateway;

  const mutation = useMutation({
    mutationFn: async () => {
      const body: Record<string, unknown> = {
        action: "update",
        slug: agent.slug,
      };
      if (draftKind === "") {
        // Empty kind means "clear per-agent binding, inherit default".
        // The broker stores ProviderBinding{} which the resolver treats as
        // "fall back to global default" at dispatch time.
        body.provider = { kind: "", model: "" };
      } else {
        body.provider = { kind: draftKind, model: draftModel.trim() };
      }
      await post("/office-members", body);
    },
    onSuccess: () => {
      setSaveError(null);
      void queryClient.invalidateQueries({ queryKey: ["office-members"] });
    },
    onError: (err: unknown) => {
      setSaveError(err instanceof Error ? err.message : "Failed to save");
    },
  });

  if (isGateway) {
    const gatewayKind =
      (typeof agent.provider === "string"
        ? agent.provider
        : agent.provider?.kind) || "";
    const gatewayLabel = GATEWAY_LABELS[gatewayKind] || gatewayKind;
    return (
      <div className="agent-profile-section op-runtime">
        <SectionTitle>runtime</SectionTitle>
        <div className="op-runtime-grid">
          <span className="op-runtime-label">managed by</span>
          <span className="op-runtime-value">
            <span className="op-runtime-managed">
              <Lock width={11} height={11} />
              {gatewayLabel} gateway
            </span>
          </span>
          {binding.model && (
            <>
              <span className="op-runtime-label">model</span>
              <span
                className="op-runtime-value"
                style={{ fontFamily: "var(--font-mono)", fontSize: 12 }}
              >
                {binding.model}
              </span>
            </>
          )}
        </div>
        <p className="op-runtime-note">
          This agent was imported through the {gatewayLabel} gateway. Change its
          runtime from the Integrations app.
        </p>
      </div>
    );
  }

  const dirty =
    draftKind !== ((binding.kind as LLMRuntimeKind | undefined) ?? "") ||
    draftModel.trim() !== (binding.model ?? "").trim();

  return (
    <div className="agent-profile-section op-runtime">
      <SectionTitle>runtime</SectionTitle>
      <div className="op-runtime-grid">
        <span className="op-runtime-label">harness</span>
        <span
          className="op-runtime-value"
          style={{ fontFamily: "var(--font-mono)", fontSize: 12 }}
        >
          {harness}
        </span>
        <span className="op-runtime-label">provider</span>
        <span className="op-runtime-value">
          <select
            value={draftKind}
            disabled={!editable || mutation.isPending}
            onChange={(e) =>
              setDraftKind(e.target.value as "" | LLMRuntimeKind)
            }
          >
            <option value="">Inherit default ({globalDefault})</option>
            {llmKinds.map((kind) => (
              <option key={kind} value={kind}>
                {PROVIDER_LABELS[kind] ?? kind}
              </option>
            ))}
          </select>
        </span>
        <span className="op-runtime-label">model</span>
        <span className="op-runtime-value">
          <input
            className="input"
            type="text"
            placeholder={
              draftKind === ""
                ? "Runtime default"
                : "e.g. claude-3-5-sonnet-latest"
            }
            value={draftModel}
            disabled={!editable || draftKind === "" || mutation.isPending}
            onChange={(e) => setDraftModel(e.target.value)}
            style={{ fontFamily: "var(--font-mono)", fontSize: 12 }}
          />
        </span>
      </div>
      {draftKind === "" && (
        <p className="op-runtime-note">
          Inheriting the install default ({globalDefault}). Pick a specific
          runtime here to pin this agent.
        </p>
      )}
      {saveError && (
        <div
          className="agent-wizard-error"
          style={{ marginTop: 8 }}
          role="alert"
        >
          {saveError}
        </div>
      )}
      {editable && (
        <div className="op-runtime-actions">
          <button
            type="button"
            className="btn btn-ghost btn-sm"
            disabled={!dirty || mutation.isPending}
            onClick={() => {
              setDraftKind(
                (binding.kind &&
                llmKinds.includes(binding.kind as LLMRuntimeKind)
                  ? (binding.kind as LLMRuntimeKind)
                  : "") as "" | LLMRuntimeKind,
              );
              setDraftModel(binding.model ?? "");
              setSaveError(null);
            }}
          >
            Reset
          </button>
          <button
            type="button"
            className="btn btn-primary btn-sm"
            disabled={!dirty || mutation.isPending}
            onClick={() => mutation.mutate()}
          >
            {mutation.isPending ? "Saving..." : "Save runtime"}
          </button>
        </div>
      )}
    </div>
  );
}

// EditableName replaces the static name display with an inline-edit field.
// Click the name to edit; Enter or blur saves, Escape cancels. Trimmed
// empty values are rejected (the name is required at the broker layer
// anyway). Applies to every agent including the lead — the broker's
// built-in gate fires only on remove, not on update.
function EditableName({ agent }: { agent: OfficeMember }) {
  const queryClient = useQueryClient();
  const initial = agent.name || agent.slug;
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(initial);
  const [saveError, setSaveError] = useState<string | null>(null);

  // Keep the draft in sync when the underlying member name changes from
  // outside (e.g. another browser tab edited it). React re-mounts the
  // component on a slug switch (key prop on AgentPanelView), so we only
  // need to track name changes within the same agent.
  if (!editing && draft !== initial && draft !== agent.name) {
    setDraft(initial);
  }

  const mutation = useMutation({
    mutationFn: async (name: string) => {
      await post("/office-members", {
        action: "update",
        slug: agent.slug,
        name,
      });
    },
    onSuccess: () => {
      setSaveError(null);
      setEditing(false);
      void queryClient.invalidateQueries({ queryKey: ["office-members"] });
    },
    onError: (err: unknown) => {
      setSaveError(err instanceof Error ? err.message : "Failed to rename");
    },
  });

  const commit = () => {
    const next = draft.trim();
    if (!next || next === initial) {
      setDraft(initial);
      setEditing(false);
      setSaveError(null);
      return;
    }
    mutation.mutate(next);
  };

  if (!editing) {
    return (
      <button
        type="button"
        className="agent-panel-name"
        onClick={() => {
          setDraft(initial);
          setEditing(true);
          setSaveError(null);
        }}
        title="Click to rename"
        style={{
          background: "transparent",
          border: 0,
          padding: 0,
          font: "inherit",
          color: "inherit",
          cursor: "text",
        }}
      >
        {initial}
      </button>
    );
  }
  return (
    <>
      <input
        autoFocus={true}
        className="input"
        value={draft}
        disabled={mutation.isPending}
        onChange={(e) => setDraft(e.target.value)}
        onBlur={commit}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            commit();
          } else if (e.key === "Escape") {
            e.preventDefault();
            setDraft(initial);
            setEditing(false);
            setSaveError(null);
          }
        }}
        style={{
          font: "inherit",
          padding: "2px 6px",
          minWidth: 120,
          maxWidth: 220,
        }}
        aria-label="Agent name"
      />
      {saveError && (
        <span
          style={{
            fontSize: 11,
            color: "var(--error-400)",
            marginLeft: 6,
          }}
          role="alert"
        >
          {saveError}
        </span>
      )}
    </>
  );
}

export function AgentProfilePanel({ agent, onClose }: AgentProfilePanelProps) {
  const defaultHarness = useDefaultHarness();

  const { data: skills = [] } = useQuery({
    queryKey: ["skills-list"],
    queryFn: () =>
      getSkillsList("all").then((r) => arrayOrEmpty<Skill>(r?.skills)),
    refetchInterval: 30_000,
  });

  type ChannelLite = { slug: string; name: string; members?: string[] };
  // The "channels" cache slot is shared with useChannels() which stores
  // the full {channels: [...]} envelope, not the array. Normalize on
  // read so this surface tolerates either shape without crashing — and
  // without renaming the cache key (which would invite a stale-data
  // race during the migration).
  const { data: channels = [] as ChannelLite[] } = useQuery<
    ChannelLite[] | { channels?: ChannelLite[] },
    Error,
    ChannelLite[]
  >({
    queryKey: ["channels"],
    queryFn: () =>
      getChannels().then((r) =>
        arrayOrEmpty<ChannelLite>(r?.channels),
      ),
    refetchInterval: 30_000,
    select: (data): ChannelLite[] => {
      if (Array.isArray(data)) return data as ChannelLite[];
      const envelope = data as { channels?: ChannelLite[] };
      return Array.isArray(envelope.channels) ? envelope.channels : [];
    },
  });

  const { data: allTasks = [] } = useQuery({
    queryKey: ["office-tasks-profile"],
    queryFn: () =>
      getOfficeTasks({ includeDone: true }).then((r) =>
        arrayOrEmpty<Task>(r?.tasks),
      ),
    refetchInterval: 30_000,
  });

  const { data: runs = [], isLoading: runsLoading } = useQuery({
    queryKey: ["agent-log-tasks", agent.slug],
    queryFn: () =>
      listAgentLogTasks({ limit: 8, agentSlug: agent.slug }).then((r) =>
        arrayOrEmpty<TaskLogSummary>(r?.tasks),
      ),
    refetchInterval: 30_000,
  });

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
              <EditableName agent={agent} />
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
        {agent.task && agent.status === "active" ? (
          <div className="agent-profile-section">
            <SectionTitle>current task</SectionTitle>
            <p className="agent-profile-current-task">{agent.task}</p>
          </div>
        ) : null}

        {/* Per-agent runtime picker */}
        <RuntimeSection agent={agent} defaultHarness={defaultHarness} />

        {/* Skills */}
        <SkillsSection agentSlug={agent.slug} skills={skills} />

        {/* Channels */}
        <ChannelsSection agentSlug={agent.slug} channels={channels} />

        {/* Recent runs */}
        <RecentRunsSection runs={runs} loading={runsLoading} />

        {/* Recent tasks (artifacts) */}
        <RecentArtifactsSection agentSlug={agent.slug} tasks={allTasks} />

        {/* Permissions */}
        <PermissionsSection agent={agent} />
      </div>
    </div>
  );
}
