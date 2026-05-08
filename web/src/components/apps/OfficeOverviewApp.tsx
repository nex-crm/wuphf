// biome-ignore-all lint/a11y/noStaticElementInteractions: Intentional wrapper/backdrop or SVG hover target; interactive child controls and keyboard paths are handled nearby.
import { useQuery } from "@tanstack/react-query";

import {
  type AgentRequest,
  getAllRequests,
  getLocalProvidersStatus,
  getOfficeMembers,
  getScheduler,
  getSkillsList,
  type LocalProviderStatus,
  type OfficeMember,
  type SchedulerJob,
  type Skill,
} from "../../api/client";
import { getOfficeTasks, type Task } from "../../api/tasks";
import { formatRelativeTime } from "../../lib/format";
import { isAgentActive, normalizeStatus, taskMeta } from "../../lib/officeStatus";
import { router } from "../../lib/router";
import { ActiveTasksPanel } from "./shared/ActiveTasksPanel";
import { AgentPulsePanel } from "./shared/AgentPulsePanel";

// ── Types ──────────────────────────────────────────────────────────

interface OverviewSectionProps {
  title: string;
  count?: number;
  children: React.ReactNode;
  id?: string;
  action?: React.ReactNode;
}

interface OverviewCardProps {
  label: string;
  body?: string;
  meta?: string;
  badge?: string;
  badgeClass?: string;
  onClick?: () => void;
}

interface OverviewData {
  activeTasks: Task[];
  blockedTasks: Task[];
  recentArtifacts: Task[];
  activeAgents: OfficeMember[];
  pendingRequests: AgentRequest[];
  proposedSkills: Skill[];
  upcomingJobs: SchedulerJob[];
  unhealthyProviders: LocalProviderStatus[];
  taskIsLoading: boolean;
  membersIsLoading: boolean;
  requestsIsLoading: boolean;
  skillsIsLoading: boolean;
  schedulerIsLoading: boolean;
  providersIsFetched: boolean;
}

// ── Navigation helpers ─────────────────────────────────────────────

function goToTasks(): void {
  void router.navigate({ to: "/tasks" });
}

function goToTask(taskId: string): void {
  void router.navigate({ to: "/tasks/$taskId", params: { taskId } });
}

function goToRequests(): void {
  void router.navigate({ to: "/apps/$appId", params: { appId: "requests" } });
}

function goToSkills(): void {
  void router.navigate({ to: "/apps/$appId", params: { appId: "skills" } });
}

function goToCalendar(): void {
  void router.navigate({ to: "/apps/$appId", params: { appId: "calendar" } });
}

function goToSettings(): void {
  void router.navigate({ to: "/apps/$appId", params: { appId: "settings" } });
}

function goToHealthCheck(): void {
  void router.navigate({
    to: "/apps/$appId",
    params: { appId: "health-check" },
  });
}

// ── Data helpers ───────────────────────────────────────────────────

function providerIsUnhealthy(p: LocalProviderStatus): boolean {
  return p.probed && !p.reachable;
}

function taskBadgeClass(status: string): string {
  return status === "done" ? "badge badge-green" : "badge badge-accent";
}

// ── Data hook ─────────────────────────────────────────────────────

function useOverviewData(): OverviewData {
  const tasks = useQuery({
    queryKey: ["overview-tasks"],
    queryFn: () => getOfficeTasks({ includeDone: false }),
    refetchInterval: 15_000,
  });

  const members = useQuery({
    queryKey: ["overview-members"],
    queryFn: () => getOfficeMembers(),
    refetchInterval: 15_000,
  });

  const requests = useQuery({
    queryKey: ["overview-requests"],
    queryFn: () => getAllRequests(),
    refetchInterval: 10_000,
  });

  const skills = useQuery({
    queryKey: ["overview-skills"],
    queryFn: () => getSkillsList("all"),
    refetchInterval: 30_000,
  });

  const scheduler = useQuery({
    queryKey: ["overview-scheduler"],
    queryFn: () => getScheduler(),
    refetchInterval: 30_000,
  });

  const providers = useQuery({
    queryKey: ["overview-providers"],
    queryFn: () => getLocalProvidersStatus(),
    refetchInterval: 30_000,
  });

  const allTasks: Task[] = tasks.data?.tasks ?? [];
  const allMembers: OfficeMember[] = members.data?.members ?? [];
  const allRequests: AgentRequest[] = requests.data?.requests ?? [];
  const allSkills: Skill[] = skills.data?.skills ?? [];
  const allJobs: SchedulerJob[] = scheduler.data?.jobs ?? [];
  const allProviders: LocalProviderStatus[] = Array.isArray(providers.data)
    ? providers.data
    : [];

  const activeTasks = allTasks.filter((t) => {
    const s = normalizeStatus(t.status);
    return s === "in_progress" || s === "review";
  });

  const blockedTasks = allTasks.filter(
    (t) => normalizeStatus(t.status) === "blocked",
  );

  const activeAgents = allMembers.filter(isAgentActive);

  const pendingRequests = allRequests.filter(
    (r) => !r.status || r.status === "open" || r.status === "pending",
  );

  const proposedSkills = allSkills.filter((s) => s.status === "proposed");
  const unhealthyProviders = allProviders.filter(providerIsUnhealthy);

  const upcomingJobs = allJobs
    .filter((j) => j.next_run || j.due_at)
    .sort((a, b) => {
      const ta = a.next_run ?? a.due_at ?? "";
      const tb = b.next_run ?? b.due_at ?? "";
      return ta.localeCompare(tb);
    })
    .slice(0, 5);

  const recentArtifacts = allTasks
    .filter((t) => t.updated_at)
    .sort((a, b) =>
      String(b.updated_at ?? "").localeCompare(String(a.updated_at ?? "")),
    )
    .slice(0, 6);

  return {
    activeTasks,
    blockedTasks,
    recentArtifacts,
    activeAgents,
    pendingRequests,
    proposedSkills,
    upcomingJobs,
    unhealthyProviders,
    taskIsLoading: tasks.isLoading,
    membersIsLoading: members.isLoading,
    requestsIsLoading: requests.isLoading,
    skillsIsLoading: skills.isLoading,
    schedulerIsLoading: scheduler.isLoading,
    providersIsFetched: providers.isFetched,
  };
}

// ── Section sub-components ────────────────────────────────────────

interface ActiveRunsSectionProps {
  tasks: Task[];
  isLoading: boolean;
}

function ActiveRunsSection({ tasks, isLoading }: ActiveRunsSectionProps) {
  return (
    <OverviewSection
      title="Active runs"
      count={tasks.length}
      id="active-runs"
      action={
        tasks.length > 0 ? (
          <SectionLink onClick={goToTasks}>View board</SectionLink>
        ) : null
      }
    >
      {isLoading ? (
        <SkeletonRows count={3} />
      ) : (
        <ActiveTasksPanel
          tasks={tasks}
          limit={5}
          onTaskClick={goToTask}
          emptyLabel="No tasks are running right now."
        />
      )}
    </OverviewSection>
  );
}

interface BlockedTasksSectionProps {
  tasks: Task[];
  isLoading: boolean;
}

function BlockedTasksSection({ tasks, isLoading }: BlockedTasksSectionProps) {
  return (
    <OverviewSection
      title="Blocked tasks"
      count={tasks.length}
      id="blocked-tasks"
      action={
        tasks.length > 0 ? (
          <SectionLink onClick={goToTasks}>View board</SectionLink>
        ) : null
      }
    >
      {isLoading ? (
        <SkeletonRows count={2} />
      ) : (
        <ActiveTasksPanel
          tasks={tasks}
          badgeClass="badge badge-yellow"
          limit={5}
          onTaskClick={goToTask}
          emptyLabel="Nothing is blocked. Agents are moving freely."
        />
      )}
    </OverviewSection>
  );
}

interface AgentsWorkingSectionProps {
  agents: OfficeMember[];
  isLoading: boolean;
}

function AgentsWorkingSection({
  agents,
  isLoading,
}: AgentsWorkingSectionProps) {
  return (
    <OverviewSection
      title="Agents working now"
      count={agents.length}
      id="agents-working"
    >
      {isLoading ? (
        <SkeletonRows count={3} />
      ) : (
        <AgentPulsePanel agents={agents} limit={6} />
      )}
    </OverviewSection>
  );
}

interface PendingReviewsSectionProps {
  requests: AgentRequest[];
  isLoading: boolean;
}

function PendingReviewsSection({
  requests,
  isLoading,
}: PendingReviewsSectionProps) {
  return (
    <OverviewSection
      title="Pending reviews"
      count={requests.length}
      id="pending-reviews"
      action={
        requests.length > 0 ? (
          <SectionLink onClick={goToRequests}>Answer</SectionLink>
        ) : null
      }
    >
      {isLoading ? (
        <SkeletonRows count={2} />
      ) : requests.length === 0 ? (
        <EmptyState action={{ label: "Go to requests", onClick: goToRequests }}>
          No pending requests from agents.
        </EmptyState>
      ) : (
        // biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Pending-reviews map callback builds per-request meta + badge from multiple optional fields; inline for readability, baselined pending extraction.
        requests.slice(0, 4).map((r) => {
          const meta = [
            r.from ? `@${r.from}` : "",
            r.channel ? `#${r.channel}` : "",
            r.blocking ? "blocking" : "",
          ]
            .filter(Boolean)
            .join(" · ");
          return (
            <OverviewCard
              key={r.id}
              label={r.title || r.question?.slice(0, 60) || "Request"}
              body={r.question?.slice(0, 100)}
              meta={meta || undefined}
              badge={r.blocking ? "blocking" : "pending"}
              badgeClass={
                r.blocking ? "badge badge-yellow" : "badge badge-accent"
              }
              onClick={goToRequests}
            />
          );
        })
      )}
    </OverviewSection>
  );
}

interface WikiProposalsSectionProps {
  skills: Skill[];
  isLoading: boolean;
}

function WikiProposalsSection({
  skills,
  isLoading,
}: WikiProposalsSectionProps) {
  return (
    <OverviewSection
      title="Wiki proposals"
      count={skills.length}
      id="wiki-proposals"
      action={
        skills.length > 0 ? (
          <SectionLink onClick={goToSkills}>Review</SectionLink>
        ) : null
      }
    >
      {isLoading ? (
        <SkeletonRows count={2} />
      ) : skills.length === 0 ? (
        <EmptyState action={{ label: "Go to skills", onClick: goToSkills }}>
          No skill proposals awaiting review.
        </EmptyState>
      ) : (
        skills.slice(0, 4).map((s) => {
          const meta = [
            s.created_by ? `by @${s.created_by}` : "",
            s.created_at ? formatRelativeTime(s.created_at) : "",
          ]
            .filter(Boolean)
            .join(" · ");
          return (
            <OverviewCard
              key={s.name}
              label={s.title || s.name}
              body={s.description?.slice(0, 100)}
              meta={meta || undefined}
              badge="proposed"
              badgeClass="badge badge-yellow"
              onClick={goToSkills}
            />
          );
        })
      )}
    </OverviewSection>
  );
}

interface ScheduledJobsSectionProps {
  jobs: SchedulerJob[];
  isLoading: boolean;
}

function ScheduledJobsSection({ jobs, isLoading }: ScheduledJobsSectionProps) {
  return (
    <OverviewSection
      title="Next scheduled jobs"
      count={jobs.length}
      id="scheduled-jobs"
      action={
        jobs.length > 0 ? (
          <SectionLink onClick={goToCalendar}>View calendar</SectionLink>
        ) : null
      }
    >
      {isLoading ? (
        <SkeletonRows count={3} />
      ) : jobs.length === 0 ? (
        <EmptyState action={{ label: "Go to calendar", onClick: goToCalendar }}>
          No upcoming scheduled jobs.
        </EmptyState>
      ) : (
        // biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Scheduled-jobs map callback derives nextRun, badge, and fallback key from multiple optional fields; inline for readability, baselined pending extraction.
        jobs.map((job, idx) => {
          const nextRun = job.next_run ?? job.due_at;
          return (
            <OverviewCard
              key={job.slug ?? job.id ?? `job-${idx}`}
              label={job.label || job.name || job.slug || "Job"}
              body={job.kind || undefined}
              meta={nextRun ? `Next ${formatRelativeTime(nextRun)}` : undefined}
              badge={job.enabled === false ? "disabled" : "scheduled"}
              badgeClass={
                job.enabled === false
                  ? "badge badge-muted"
                  : "badge badge-accent"
              }
              onClick={goToCalendar}
            />
          );
        })
      )}
    </OverviewSection>
  );
}

interface RecentArtifactsSectionProps {
  tasks: Task[];
  isLoading: boolean;
}

function RecentArtifactsSection({
  tasks,
  isLoading,
}: RecentArtifactsSectionProps) {
  return (
    <OverviewSection
      title="Recent artifacts"
      count={tasks.length}
      id="recent-artifacts"
      action={
        tasks.length > 0 ? (
          <SectionLink onClick={goToTasks}>View tasks</SectionLink>
        ) : null
      }
    >
      {isLoading ? (
        <SkeletonRows count={3} />
      ) : tasks.length === 0 ? (
        <EmptyState>No recent task activity.</EmptyState>
      ) : (
        tasks.map((t) => {
          const meta = [
            t.owner ? `@${t.owner}` : "",
            t.updated_at ? formatRelativeTime(t.updated_at) : "",
          ]
            .filter(Boolean)
            .join(" · ");
          const status = normalizeStatus(t.status);
          return (
            <OverviewCard
              key={t.id}
              label={t.title || t.id}
              meta={meta || undefined}
              badge={status.replace(/_/g, " ")}
              badgeClass={taskBadgeClass(status)}
              onClick={() => goToTask(t.id)}
            />
          );
        })
      )}
    </OverviewSection>
  );
}

// ── Main component ────────────────────────────────────────────────

export function OfficeOverviewApp() {
  const data = useOverviewData();

  return (
    <div
      data-testid="office-overview-app"
      style={{
        display: "flex",
        flexDirection: "column",
        gap: 20,
        padding: "4px 0",
      }}
    >
      {/* Header */}
      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "flex-start",
        }}
      >
        <div>
          <h3 style={{ fontSize: 18, fontWeight: 700 }}>Office overview</h3>
          <div
            style={{
              fontSize: 13,
              color: "var(--text-secondary)",
              marginTop: 4,
            }}
          >
            What is active, blocked, and ready for review right now.
          </div>
        </div>
        <div
          style={{
            fontSize: 12,
            color: "var(--text-tertiary)",
            whiteSpace: "nowrap",
          }}
        >
          {new Date().toLocaleTimeString([], {
            hour: "numeric",
            minute: "2-digit",
          })}
        </div>
      </div>

      {/* Provider warnings — always at top so they are impossible to miss */}
      {data.providersIsFetched && data.unhealthyProviders.length > 0 ? (
        <ProviderWarningsSection providers={data.unhealthyProviders} />
      ) : null}

      {/* Section grid */}
      <div
        style={{
          display: "grid",
          gridTemplateColumns: "repeat(auto-fill, minmax(280px, 1fr))",
          gap: 16,
          alignItems: "start",
        }}
      >
        <ActiveRunsSection
          tasks={data.activeTasks}
          isLoading={data.taskIsLoading}
        />
        <BlockedTasksSection
          tasks={data.blockedTasks}
          isLoading={data.taskIsLoading}
        />
        <AgentsWorkingSection
          agents={data.activeAgents}
          isLoading={data.membersIsLoading}
        />
        <PendingReviewsSection
          requests={data.pendingRequests}
          isLoading={data.requestsIsLoading}
        />
        <WikiProposalsSection
          skills={data.proposedSkills}
          isLoading={data.skillsIsLoading}
        />
        <ScheduledJobsSection
          jobs={data.upcomingJobs}
          isLoading={data.schedulerIsLoading}
        />
        <RecentArtifactsSection
          tasks={data.recentArtifacts}
          isLoading={data.taskIsLoading}
        />
      </div>
    </div>
  );
}

// ── Provider warnings section ──────────────────────────────────────

interface ProviderWarningsSectionProps {
  providers: LocalProviderStatus[];
}

function ProviderWarningsSection({ providers }: ProviderWarningsSectionProps) {
  return (
    <section
      id="provider-warnings"
      aria-label="Provider warnings"
      style={{
        background: "var(--warning-100, #fbf5dc)",
        border: "1px solid var(--warning-300, #ffb647)",
        borderRadius: 8,
        padding: "12px 16px",
      }}
    >
      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "baseline",
          marginBottom: 8,
        }}
      >
        <div
          style={{
            fontSize: 13,
            fontWeight: 600,
            color: "var(--warning-500, #994200)",
          }}
        >
          {providers.length} provider{providers.length !== 1 ? "s" : ""}{" "}
          unreachable
        </div>
        <div style={{ display: "flex", gap: 8 }}>
          <SectionLink onClick={goToSettings}>Settings</SectionLink>
          <SectionLink onClick={goToHealthCheck}>Provider Doctor</SectionLink>
        </div>
      </div>
      {providers.map((p) => (
        <div
          key={p.kind}
          style={{
            fontSize: 13,
            color: "var(--warning-500, #994200)",
            marginBottom: 4,
            display: "flex",
            alignItems: "center",
            gap: 6,
          }}
        >
          <span
            style={{
              width: 6,
              height: 6,
              borderRadius: "50%",
              background: "var(--warning-400, #ce6b09)",
              flexShrink: 0,
            }}
          />
          <strong>{p.kind}</strong>
          {p.binary_installed && !p.reachable
            ? " — installed but not running"
            : " — not reachable"}
          {p.endpoint ? (
            <span
              style={{ color: "var(--warning-400, #ce6b09)", fontSize: 11 }}
            >
              ({p.endpoint})
            </span>
          ) : null}
        </div>
      ))}
      <div
        style={{
          marginTop: 8,
          fontSize: 12,
          color: "var(--warning-400, #ce6b09)",
        }}
      >
        Agents assigned to these providers will stall. Open Settings or Provider
        Doctor to fix the connection.
      </div>
    </section>
  );
}

// ── Section wrapper ────────────────────────────────────────────────

function OverviewSection({
  title,
  count,
  children,
  id,
  action,
}: OverviewSectionProps) {
  return (
    <section id={id} style={{ scrollMarginTop: 16 }}>
      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "baseline",
          marginBottom: 8,
        }}
      >
        <div style={{ display: "flex", alignItems: "baseline", gap: 6 }}>
          <span style={{ fontSize: 13, fontWeight: 600 }}>{title}</span>
          {count !== undefined ? (
            <span
              style={{
                fontSize: 11,
                color: "var(--text-tertiary)",
                fontVariantNumeric: "tabular-nums",
              }}
            >
              {count}
            </span>
          ) : null}
        </div>
        {action ?? null}
      </div>
      {children}
    </section>
  );
}

// ── Card ───────────────────────────────────────────────────────────

function OverviewCard({
  label,
  body,
  meta,
  badge,
  badgeClass,
  onClick,
}: OverviewCardProps) {
  function handleKeyDown(event: React.KeyboardEvent<HTMLDivElement>) {
    if (!onClick) return;
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      onClick();
    }
  }

  return (
    <div
      className="app-card"
      style={{
        marginBottom: 6,
        cursor: onClick ? "pointer" : "default",
      }}
      onClick={onClick}
      onKeyDown={onClick ? handleKeyDown : undefined}
      role={onClick ? "button" : undefined}
      tabIndex={onClick ? 0 : undefined}
    >
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 6,
          marginBottom: body || meta ? 4 : 0,
        }}
      >
        {badge ? (
          <span className={badgeClass ?? "badge badge-accent"}>{badge}</span>
        ) : null}
        <span
          style={{
            fontWeight: 600,
            fontSize: 13,
            overflow: "hidden",
            textOverflow: "ellipsis",
            whiteSpace: "nowrap",
          }}
        >
          {label}
        </span>
      </div>
      {body ? (
        <div
          style={{
            fontSize: 12,
            color: "var(--text-secondary)",
            marginBottom: meta ? 4 : 0,
            lineHeight: 1.45,
          }}
        >
          {body}
        </div>
      ) : null}
      {meta ? <div className="app-card-meta">{meta}</div> : null}
    </div>
  );
}

// ── Empty state ────────────────────────────────────────────────────

interface EmptyStateProps {
  children: React.ReactNode;
  action?: { label: string; onClick: () => void };
}

function EmptyState({ children, action }: EmptyStateProps) {
  return (
    <div
      style={{
        padding: "20px 0",
        textAlign: "center",
        color: "var(--text-tertiary)",
        fontSize: 13,
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        gap: 8,
      }}
    >
      <span>{children}</span>
      {action ? (
        <button
          type="button"
          className="btn btn-sm btn-ghost"
          onClick={action.onClick}
        >
          {action.label}
        </button>
      ) : null}
    </div>
  );
}

// ── Section link ───────────────────────────────────────────────────

function SectionLink({
  children,
  onClick,
}: {
  children: React.ReactNode;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      style={{
        background: "none",
        border: "none",
        padding: 0,
        fontSize: 12,
        color: "var(--accent)",
        cursor: "pointer",
        fontWeight: 500,
      }}
    >
      {children}
    </button>
  );
}

// ── Skeleton loading rows ─────────────────────────────────────────

function SkeletonRows({ count }: { count: number }) {
  return (
    <>
      {Array.from({ length: count }, (_, i) => (
        <div
          // biome-ignore lint/suspicious/noArrayIndexKey: Static skeleton rows have no identity; order never changes so index key is safe.
          key={i}
          className="app-card"
          style={{
            marginBottom: 6,
            height: 52,
            background: "var(--neutral-50, #f2f2f3)",
          }}
        />
      ))}
    </>
  );
}
