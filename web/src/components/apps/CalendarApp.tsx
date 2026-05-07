// biome-ignore-all lint/a11y/useKeyWithClickEvents: Calendar cells and task chips are navigable via explicit link/button children; div click handlers handle supplemental pointer shortcuts.
import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { getScheduler, type SchedulerJob } from "../../api/client";
import type { Task } from "../../api/tasks";
import { useOfficeTasks } from "../../hooks/useOfficeTasks";
import { resolveObjectRoute } from "../../lib/objectRoutes";
import {
  groupTasksByAgent,
  groupTasksByWeek,
  normalizeTaskStatus,
  type TaskStatus,
  UNASSIGNED_BUCKET,
  UNSCHEDULED_BUCKET,
} from "../../lib/taskProjections";
import { SystemSchedulesPanel } from "./SystemSchedulesPanel";
import { isCadenceSchedulerJob } from "./schedulerJobClassification";

// ── Week helpers ──────────────────────────────────────────────────────────────

/** Return the Monday of the ISO week containing `date` (UTC). */
function startOfWeek(date: Date): Date {
  const d = new Date(
    Date.UTC(date.getUTCFullYear(), date.getUTCMonth(), date.getUTCDate()),
  );
  const dow = d.getUTCDay(); // 0=Sun … 6=Sat
  const diff = dow === 0 ? -6 : 1 - dow; // shift to Monday
  d.setUTCDate(d.getUTCDate() + diff);
  return d;
}

function addDays(date: Date, n: number): Date {
  const d = new Date(date.getTime());
  d.setUTCDate(d.getUTCDate() + n);
  return d;
}

function isoDay(date: Date): string {
  return date.toISOString().slice(0, 10);
}

function formatDayHeader(isoDate: string): { weekday: string; day: string } {
  const d = new Date(`${isoDate}T00:00:00Z`);
  return {
    weekday: d.toLocaleDateString(undefined, {
      weekday: "short",
      timeZone: "UTC",
    }),
    day: d.toLocaleDateString(undefined, { day: "numeric", timeZone: "UTC" }),
  };
}

function formatWeekRange(weekStart: Date): string {
  const end = addDays(weekStart, 6);
  return `${weekStart.toLocaleDateString(undefined, { month: "short", day: "numeric", timeZone: "UTC" })} – ${end.toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric", timeZone: "UTC" })}`;
}

// ── Status visuals ────────────────────────────────────────────────────────────

const STATUS_DOT_COLOR: Record<TaskStatus, string> = {
  in_progress: "var(--accent)",
  open: "var(--text-tertiary)",
  review: "var(--yellow)",
  pending: "var(--text-tertiary)",
  blocked: "var(--red)",
  done: "var(--green)",
  canceled: "var(--neutral-300)",
};

const STATUS_LABEL: Record<TaskStatus, string> = {
  in_progress: "in progress",
  open: "open",
  review: "review",
  pending: "pending",
  blocked: "blocked",
  done: "done",
  canceled: "canceled",
};

function statusDotColor(status: string): string {
  return (
    STATUS_DOT_COLOR[normalizeTaskStatus(status)] ?? "var(--text-tertiary)"
  );
}

function statusLabel(status: string): string {
  return STATUS_LABEL[normalizeTaskStatus(status)] ?? status;
}

// ── Scheduler helpers ─────────────────────────────────────────────────────────

/** Group one-shot scheduler jobs by the day their `next_run` falls on. */
function groupSchedulerJobsByDay(
  jobs: SchedulerJob[],
  dayKeys: string[],
): Record<string, SchedulerJob[]> {
  const groups: Record<string, SchedulerJob[]> = {};
  for (const key of dayKeys) groups[key] = [];
  for (const job of jobs) {
    if (!job.next_run) continue;
    const d = new Date(job.next_run);
    if (Number.isNaN(d.getTime())) continue;
    const key = isoDay(d);
    if (groups[key]) groups[key].push(job);
  }
  return groups;
}

// ── Types ─────────────────────────────────────────────────────────────────────

type ViewMode = "week" | "agenda";

// ── CalendarApp ───────────────────────────────────────────────────────────────

/**
 * Calendar workspace — Phase 5 PR 8.
 *
 * Desktop: week grid with agent swimlanes, day columns, task chips, and
 * scheduler jobs in a distinct visual treatment.
 * Mobile: agenda/list mode toggled automatically below 640px via JS state.
 * Unscheduled tasks appear in a bottom tray; unassigned tasks get their own
 * swimlane.
 */
export function CalendarApp() {
  const today = new Date();
  const [weekStart, setWeekStart] = useState<Date>(() => startOfWeek(today));
  const [viewMode, setViewMode] = useState<ViewMode>("week");

  const tasksResult = useOfficeTasks();
  const schedulerResult = useQuery({
    queryKey: ["scheduler"],
    queryFn: () => getScheduler(),
    refetchInterval: 15_000,
  });

  const dayKeys = useMemo<string[]>(() => {
    return Array.from({ length: 7 }, (_, i) => isoDay(addDays(weekStart, i)));
  }, [weekStart]);

  const tasksByAgent = useMemo<Record<string, Task[]>>(() => {
    if (!tasksResult.data) return { [UNASSIGNED_BUCKET]: [] };
    return groupTasksByAgent(tasksResult.data);
  }, [tasksResult.data]);

  const tasksByWeek = useMemo<Record<string, Task[]>>(() => {
    if (!tasksResult.data) {
      const g: Record<string, Task[]> = { [UNSCHEDULED_BUCKET]: [] };
      for (const k of dayKeys) g[k] = [];
      return g;
    }
    return groupTasksByWeek(tasksResult.data, weekStart);
  }, [tasksResult.data, weekStart, dayKeys]);

  const schedulerJobsData = schedulerResult.data?.jobs;
  const { oneShotJobs, cadenceJobs } = useMemo(() => {
    const jobs = schedulerJobsData ?? [];
    return {
      oneShotJobs: jobs.filter((j) => !isCadenceSchedulerJob(j)),
      cadenceJobs: jobs.filter(isCadenceSchedulerJob),
    };
  }, [schedulerJobsData]);
  const schedulerByDay = useMemo(
    () => groupSchedulerJobsByDay(oneShotJobs, dayKeys),
    [oneShotJobs, dayKeys],
  );

  const unscheduledTasks = tasksByWeek[UNSCHEDULED_BUCKET] ?? [];

  const agentSlugs = useMemo<string[]>(() => {
    const agents = Object.keys(tasksByAgent).filter(
      (k) => k !== UNASSIGNED_BUCKET,
    );
    agents.sort();
    // unassigned always last
    return [...agents, UNASSIGNED_BUCKET];
  }, [tasksByAgent]);

  const isLoading = tasksResult.isLoading || schedulerResult.isLoading;
  const hasError = tasksResult.error || schedulerResult.error;

  const goToPrevWeek = () => setWeekStart((w) => addDays(w, -7));
  const goToNextWeek = () => setWeekStart((w) => addDays(w, 7));
  const goToCurrentWeek = () => setWeekStart(startOfWeek(new Date()));

  return (
    <div
      data-testid="calendar-app"
      style={{ display: "flex", flexDirection: "column", gap: 0, minHeight: 0 }}
    >
      {/* Header */}
      <CalendarHeader
        weekStart={weekStart}
        viewMode={viewMode}
        onPrevWeek={goToPrevWeek}
        onNextWeek={goToNextWeek}
        onToday={goToCurrentWeek}
        onToggleView={() =>
          setViewMode((m) => (m === "week" ? "agenda" : "week"))
        }
      />

      {/* System cadence schedules */}
      {cadenceJobs.length > 0 && (
        <div style={{ marginBottom: 12 }}>
          <SystemSchedulesPanel jobs={cadenceJobs} />
        </div>
      )}

      {isLoading ? (
        <LoadingState />
      ) : hasError ? (
        <ErrorState />
      ) : viewMode === "week" ? (
        <WeekGrid
          dayKeys={dayKeys}
          agentSlugs={agentSlugs}
          tasksByAgent={tasksByAgent}
          tasksByWeek={tasksByWeek}
          schedulerByDay={schedulerByDay}
          unscheduledTasks={unscheduledTasks}
          today={isoDay(today)}
        />
      ) : (
        <AgendaView
          dayKeys={dayKeys}
          agentSlugs={agentSlugs}
          tasksByAgent={tasksByAgent}
          tasksByWeek={tasksByWeek}
          schedulerByDay={schedulerByDay}
          unscheduledTasks={unscheduledTasks}
          today={isoDay(today)}
        />
      )}
    </div>
  );
}

// ── CalendarHeader ────────────────────────────────────────────────────────────

interface CalendarHeaderProps {
  weekStart: Date;
  viewMode: ViewMode;
  onPrevWeek: () => void;
  onNextWeek: () => void;
  onToday: () => void;
  onToggleView: () => void;
}

function CalendarHeader({
  weekStart,
  viewMode,
  onPrevWeek,
  onNextWeek,
  onToday,
  onToggleView,
}: CalendarHeaderProps) {
  return (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        gap: 8,
        padding: "0 0 12px",
        borderBottom: "1px solid var(--border)",
        marginBottom: 12,
        flexWrap: "wrap",
      }}
    >
      <h3 style={{ fontSize: 15, fontWeight: 600, flex: 1 }}>
        {formatWeekRange(weekStart)}
      </h3>

      <button
        type="button"
        onClick={onToday}
        aria-label="Go to current week"
        style={navBtnStyle}
      >
        Today
      </button>

      <button
        type="button"
        onClick={onPrevWeek}
        aria-label="Previous week"
        style={navBtnStyle}
      >
        ‹
      </button>

      <button
        type="button"
        onClick={onNextWeek}
        aria-label="Next week"
        style={navBtnStyle}
      >
        ›
      </button>

      <button
        type="button"
        onClick={onToggleView}
        aria-label={
          viewMode === "week" ? "Switch to agenda view" : "Switch to week view"
        }
        style={{ ...navBtnStyle, minWidth: 64 }}
      >
        {viewMode === "week" ? "Agenda" : "Week"}
      </button>
    </div>
  );
}

const navBtnStyle: React.CSSProperties = {
  padding: "3px 10px",
  fontSize: 12,
  fontWeight: 500,
  background: "transparent",
  border: "1px solid var(--border)",
  borderRadius: 6,
  color: "var(--text-secondary)",
  cursor: "pointer",
};

// ── WeekGrid ──────────────────────────────────────────────────────────────────

interface WeekGridProps {
  dayKeys: string[];
  agentSlugs: string[];
  tasksByAgent: Record<string, Task[]>;
  tasksByWeek: Record<string, Task[]>;
  schedulerByDay: Record<string, SchedulerJob[]>;
  unscheduledTasks: Task[];
  today: string;
}

/**
 * Week grid: each row is an agent swimlane, each column is a day.
 * Scheduler one-shot jobs appear in a separate row at the top with a clock
 * badge so they are visually distinct from task due dates.
 */
function WeekGrid({
  dayKeys,
  agentSlugs,
  tasksByAgent,
  tasksByWeek,
  schedulerByDay,
  unscheduledTasks,
  today,
}: WeekGridProps) {
  const hasTasks = agentSlugs.some(
    (slug) => (tasksByAgent[slug]?.length ?? 0) > 0,
  );
  const hasSchedulerJobs = dayKeys.some(
    (k) => (schedulerByDay[k]?.length ?? 0) > 0,
  );

  if (!(hasTasks || hasSchedulerJobs) && unscheduledTasks.length === 0) {
    return <EmptyState />;
  }

  return (
    <div style={{ overflowX: "auto" }}>
      <table
        data-testid="week-grid"
        style={{
          width: "100%",
          borderCollapse: "collapse",
          tableLayout: "fixed",
          minWidth: 560,
        }}
      >
        <colgroup>
          {/* Agent label column */}
          <col style={{ width: 120 }} />
          {dayKeys.map((k) => (
            <col key={k} />
          ))}
        </colgroup>

        <thead>
          <tr>
            <th style={theadCellStyle} aria-label="Agent" />
            {dayKeys.map((k) => {
              const { weekday, day } = formatDayHeader(k);
              const isToday = k === today;
              return (
                <th
                  key={k}
                  style={{ ...theadCellStyle, fontWeight: isToday ? 700 : 500 }}
                >
                  <span
                    style={{
                      color: isToday
                        ? "var(--accent)"
                        : "var(--text-secondary)",
                    }}
                  >
                    {weekday}
                  </span>
                  <br />
                  <span
                    style={{
                      fontSize: 16,
                      fontWeight: isToday ? 700 : 400,
                      color: isToday ? "var(--accent)" : "var(--text)",
                    }}
                  >
                    {day}
                  </span>
                </th>
              );
            })}
          </tr>
        </thead>

        <tbody>
          {/* Scheduler row */}
          {hasSchedulerJobs && (
            <tr>
              <td style={rowLabelCellStyle}>
                <span style={{ fontSize: 10, color: "var(--text-tertiary)" }}>
                  SCHEDULES
                </span>
              </td>
              {dayKeys.map((k) => (
                <td key={k} style={dayCellStyle(k === today)}>
                  {(schedulerByDay[k] ?? []).map((job, idx) => (
                    <SchedulerJobChip
                      key={job.slug ?? job.id ?? idx}
                      job={job}
                    />
                  ))}
                </td>
              ))}
            </tr>
          )}

          {/* Agent swimlanes */}
          {agentSlugs.map((slug) => {
            const agentTasks = tasksByAgent[slug] ?? [];
            if (agentTasks.length === 0) return null;
            return (
              <AgentWeekRow
                key={slug}
                agentSlug={slug}
                agentTasks={agentTasks}
                dayKeys={dayKeys}
                tasksByWeek={tasksByWeek}
                today={today}
              />
            );
          })}
        </tbody>
      </table>

      {/* Unscheduled tray */}
      <UnscheduledTray tasks={unscheduledTasks} />
    </div>
  );
}

const theadCellStyle: React.CSSProperties = {
  padding: "6px 8px",
  fontSize: 11,
  textAlign: "center",
  borderBottom: "1px solid var(--border)",
  color: "var(--text-secondary)",
  fontWeight: 500,
};

const rowLabelCellStyle: React.CSSProperties = {
  padding: "6px 8px",
  fontSize: 11,
  fontWeight: 600,
  color: "var(--text-secondary)",
  verticalAlign: "top",
  borderBottom: "1px solid var(--border-light)",
  whiteSpace: "nowrap",
  overflow: "hidden",
  textOverflow: "ellipsis",
  maxWidth: 120,
};

function dayCellStyle(isToday: boolean): React.CSSProperties {
  return {
    padding: "4px 4px",
    verticalAlign: "top",
    borderBottom: "1px solid var(--border-light)",
    borderLeft: "1px solid var(--border-light)",
    background: isToday ? "var(--accent-bg)" : undefined,
    minHeight: 40,
  };
}

// ── AgentWeekRow ──────────────────────────────────────────────────────────────

interface AgentWeekRowProps {
  agentSlug: string;
  agentTasks: Task[];
  dayKeys: string[];
  tasksByWeek: Record<string, Task[]>;
  today: string;
}

function AgentWeekRow({
  agentSlug,
  agentTasks,
  dayKeys,
  tasksByWeek,
  today,
}: AgentWeekRowProps) {
  const agentTaskIds = useMemo(
    () => new Set(agentTasks.map((t) => t.id)),
    [agentTasks],
  );

  const agentRoute = resolveObjectRoute({ kind: "agent", slug: agentSlug });
  const isUnassigned = agentSlug === UNASSIGNED_BUCKET;

  return (
    <tr data-testid={`agent-row-${agentSlug}`}>
      <td style={rowLabelCellStyle}>
        {isUnassigned ? (
          <span style={{ color: "var(--text-tertiary)" }}>unassigned</span>
        ) : (
          <a
            href={agentRoute.href}
            style={{
              color: "var(--text-secondary)",
              textDecoration: "none",
              fontSize: 11,
              fontWeight: 600,
            }}
            title={`Open ${agentSlug}`}
          >
            {agentSlug}
          </a>
        )}
      </td>

      {dayKeys.map((dayKey) => {
        const dayTasks = (tasksByWeek[dayKey] ?? []).filter((t) =>
          agentTaskIds.has(t.id),
        );
        return (
          <td key={dayKey} style={dayCellStyle(dayKey === today)}>
            {dayTasks.map((task) => (
              <TaskChip key={task.id} task={task} />
            ))}
          </td>
        );
      })}
    </tr>
  );
}

// ── AgendaView ────────────────────────────────────────────────────────────────

interface AgendaViewProps {
  dayKeys: string[];
  agentSlugs: string[];
  tasksByAgent: Record<string, Task[]>;
  tasksByWeek: Record<string, Task[]>;
  schedulerByDay: Record<string, SchedulerJob[]>;
  unscheduledTasks: Task[];
  today: string;
}

/**
 * Agenda view — linear day-by-day list. Used automatically on mobile (toggle)
 * and available on desktop via the view switcher.
 */
function AgendaView({
  dayKeys,
  tasksByWeek,
  schedulerByDay,
  unscheduledTasks,
  today,
}: AgendaViewProps) {
  const hasSomething =
    dayKeys.some(
      (k) =>
        (tasksByWeek[k]?.length ?? 0) > 0 ||
        (schedulerByDay[k]?.length ?? 0) > 0,
    ) || unscheduledTasks.length > 0;

  if (!hasSomething) return <EmptyState />;

  return (
    <div
      data-testid="agenda-view"
      style={{ display: "flex", flexDirection: "column", gap: 0 }}
    >
      {dayKeys.map((dayKey) => {
        const dayTasks = tasksByWeek[dayKey] ?? [];
        const dayJobs = schedulerByDay[dayKey] ?? [];
        if (dayTasks.length === 0 && dayJobs.length === 0) return null;

        const { weekday, day } = formatDayHeader(dayKey);
        const isToday = dayKey === today;
        return (
          <div key={dayKey} style={{ marginBottom: 12 }}>
            <div
              style={{
                fontSize: 11,
                fontWeight: 600,
                textTransform: "uppercase",
                letterSpacing: "0.05em",
                color: isToday ? "var(--accent)" : "var(--text-tertiary)",
                padding: "6px 0 4px",
                borderBottom: "1px solid var(--border-light)",
                marginBottom: 6,
              }}
            >
              {weekday} {day}
              {isToday && (
                <span style={{ marginLeft: 6, fontSize: 10 }}>— today</span>
              )}
            </div>
            {dayJobs.map((job, idx) => (
              <SchedulerJobChip
                key={job.slug ?? job.id ?? idx}
                job={job}
                compact={true}
              />
            ))}
            {dayTasks.map((task) => (
              <TaskChip key={task.id} task={task} compact={true} />
            ))}
          </div>
        );
      })}

      <UnscheduledTray tasks={unscheduledTasks} />
    </div>
  );
}

// ── TaskChip ──────────────────────────────────────────────────────────────────

interface TaskChipProps {
  task: Task;
  compact?: boolean;
}

function TaskChip({ task, compact = false }: TaskChipProps) {
  const route = resolveObjectRoute({ kind: "task", id: task.id });
  const dotColor = statusDotColor(task.status);
  const label = statusLabel(task.status);

  return (
    <a
      href={route.href}
      title={`${task.title} — ${label}`}
      data-testid={`task-chip-${task.id}`}
      style={{
        display: "block",
        padding: compact ? "3px 6px" : "4px 6px",
        marginBottom: 2,
        background: "var(--bg-card)",
        border: "1px solid var(--border)",
        borderRadius: 5,
        textDecoration: "none",
        color: "var(--text)",
        fontSize: 11,
        lineHeight: 1.35,
        overflow: "hidden",
        whiteSpace: "nowrap",
        textOverflow: "ellipsis",
        maxWidth: "100%",
      }}
    >
      <span
        aria-hidden="true"
        style={{
          display: "inline-block",
          width: 6,
          height: 6,
          borderRadius: "50%",
          background: dotColor,
          marginRight: 4,
          flexShrink: 0,
          verticalAlign: "middle",
        }}
      />
      {task.title}
    </a>
  );
}

// ── SchedulerJobChip ──────────────────────────────────────────────────────────

interface SchedulerJobChipProps {
  job: SchedulerJob;
  compact?: boolean;
}

function SchedulerJobChip({ job, compact = false }: SchedulerJobChipProps) {
  const label = job.label || job.name || job.slug || "Job";
  return (
    <div
      data-testid={`scheduler-chip-${job.slug ?? job.id ?? label}`}
      title={`Scheduler: ${label}`}
      style={{
        display: "block",
        padding: compact ? "3px 6px" : "4px 6px",
        marginBottom: 2,
        background: "var(--accent-bg)",
        border: "1px solid var(--border)",
        borderRadius: 5,
        fontSize: 11,
        lineHeight: 1.35,
        overflow: "hidden",
        whiteSpace: "nowrap",
        textOverflow: "ellipsis",
        maxWidth: "100%",
        color: "var(--accent-warm)",
      }}
    >
      {/* Clock icon distinguishes scheduler jobs from task chips */}
      <span aria-hidden="true" style={{ marginRight: 4, fontSize: 10 }}>
        ⏱
      </span>
      {label}
    </div>
  );
}

// ── UnscheduledTray ───────────────────────────────────────────────────────────

interface UnscheduledTrayProps {
  tasks: Task[];
}

/** Bottom tray for tasks without a `due_at`. Always rendered when non-empty. */
function UnscheduledTray({ tasks }: UnscheduledTrayProps) {
  if (tasks.length === 0) return null;

  return (
    <div data-testid="unscheduled-tray" style={{ marginTop: 16 }}>
      <div
        style={{
          fontSize: 11,
          fontWeight: 600,
          textTransform: "uppercase",
          letterSpacing: "0.05em",
          color: "var(--text-tertiary)",
          padding: "6px 0 4px",
          borderBottom: "1px solid var(--border-light)",
          marginBottom: 6,
        }}
      >
        Unscheduled ({tasks.length})
      </div>
      <div
        style={{
          display: "flex",
          flexWrap: "wrap",
          gap: 4,
        }}
      >
        {tasks.map((task) => (
          <TaskChip key={task.id} task={task} compact={true} />
        ))}
      </div>
    </div>
  );
}

// ── Empty / Loading / Error states ────────────────────────────────────────────

function EmptyState() {
  return (
    <div
      data-testid="calendar-empty-state"
      style={{
        padding: "48px 24px",
        textAlign: "center",
        color: "var(--text-tertiary)",
        fontSize: 14,
        lineHeight: 1.6,
      }}
    >
      <div style={{ fontSize: 28, marginBottom: 12 }}>📅</div>
      <div
        style={{
          fontWeight: 600,
          marginBottom: 6,
          color: "var(--text-secondary)",
        }}
      >
        Nothing scheduled this week
      </div>
      <div style={{ fontSize: 12, maxWidth: 320, margin: "0 auto" }}>
        Tasks appear here when an agent is assigned a due date. Unassigned or
        undated tasks show up in the Unscheduled tray below.
      </div>
    </div>
  );
}

function LoadingState() {
  return (
    <div
      data-testid="calendar-loading"
      style={{
        padding: "40px 20px",
        textAlign: "center",
        color: "var(--text-tertiary)",
        fontSize: 14,
      }}
    >
      Loading calendar…
    </div>
  );
}

function ErrorState() {
  return (
    <div
      data-testid="calendar-error"
      style={{
        padding: "40px 20px",
        textAlign: "center",
        color: "var(--text-tertiary)",
        fontSize: 14,
      }}
    >
      Could not load calendar. Check your connection and try again.
    </div>
  );
}
