import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { getScheduler, type SchedulerJob } from "../../api/client";
import { router } from "../../lib/router";
import { RoutineCalendarView } from "./routines/RoutineCalendarView";
import { RoutineListView } from "./routines/RoutineListView";
import {
  isCadenceSchedulerJob,
  isSystemRoutine,
} from "./schedulerJobClassification";

type ViewMode = "calendar" | "list";

const STORAGE_KEY = "routines.viewMode";
const SHOW_SYSTEM_KEY = "routines.showSystem";

function loadInitialView(): ViewMode {
  if (typeof window === "undefined") return "list";
  try {
    const stored = window.localStorage.getItem(STORAGE_KEY);
    if (stored === "list" || stored === "calendar") return stored;
  } catch {
    /* localStorage unavailable — fall through to default */
  }
  return "list";
}

function loadInitialShowSystem(): boolean {
  if (typeof window === "undefined") return false;
  try {
    return window.localStorage.getItem(SHOW_SYSTEM_KEY) === "true";
  } catch {
    return false;
  }
}

/**
 * Routines workspace — replaces the legacy Calendar app.
 *
 * Two toggleable views render the same underlying scheduler jobs:
 *   - Calendar: month grid (Google Calendar style) showing routines on
 *     their scheduled fire dates.
 *   - List: flat list (Paperclip style) with per-row controls (run-now,
 *     enable toggle, click-through to detail).
 *
 * Both views drive a shared right-side detail drawer that surfaces the
 * routine's instructions, schedule, owning agent, and previous-run history.
 */
export function RoutinesApp() {
  const [view, setView] = useState<ViewMode>(loadInitialView);
  const [showSystem, setShowSystem] = useState<boolean>(loadInitialShowSystem);

  const scheduler = useQuery({
    queryKey: ["scheduler"],
    queryFn: () => getScheduler(),
    refetchInterval: 15_000,
  });

  // Only show recurring routines here. One-shot follow-ups (task/request
  // recheck jobs) live in their own surfaces and would just be noise.
  const allRoutines = useMemo<SchedulerJob[]>(() => {
    const jobs = scheduler.data?.jobs ?? [];
    return jobs.filter(isCadenceSchedulerJob);
  }, [scheduler.data]);

  // System routines (broker-managed crons like nex-insights) are
  // infrastructure plumbing most users don't care about. Hide them by
  // default; surface a checkbox to opt in.
  const routines = useMemo<SchedulerJob[]>(() => {
    if (showSystem) return allRoutines;
    return allRoutines.filter((r) => !isSystemRoutine(r));
  }, [allRoutines, showSystem]);

  const hiddenSystemCount = useMemo<number>(
    () => allRoutines.filter(isSystemRoutine).length,
    [allRoutines],
  );

  function openRoutine(slug: string): void {
    void router.navigate({
      to: "/routines/$routineSlug",
      params: { routineSlug: slug },
    });
  }

  function changeView(next: ViewMode): void {
    setView(next);
    try {
      window.localStorage.setItem(STORAGE_KEY, next);
    } catch {
      /* localStorage unavailable — silently accept the in-memory toggle */
    }
  }

  function toggleShowSystem(next: boolean): void {
    setShowSystem(next);
    try {
      window.localStorage.setItem(SHOW_SYSTEM_KEY, next ? "true" : "false");
    } catch {
      /* localStorage unavailable — silently accept the in-memory toggle */
    }
  }

  return (
    <div data-testid="routines-app" className="routines-shell">
      <RoutinesHeader
        view={view}
        onChangeView={changeView}
        total={routines.length}
        showSystem={showSystem}
        onToggleShowSystem={toggleShowSystem}
        hiddenSystemCount={hiddenSystemCount}
        onNewRoutine={() => void router.navigate({ to: "/routines/new" })}
      />

      {scheduler.isLoading ? (
        <LoadingState />
      ) : scheduler.error ? (
        <ErrorState />
      ) : routines.length === 0 ? (
        <EmptyState
          hiddenSystemCount={hiddenSystemCount}
          onShowSystem={() => toggleShowSystem(true)}
        />
      ) : view === "calendar" ? (
        <RoutineCalendarView routines={routines} onSelect={openRoutine} />
      ) : (
        <RoutineListView routines={routines} onSelect={openRoutine} />
      )}
    </div>
  );
}

interface RoutinesHeaderProps {
  view: ViewMode;
  onChangeView: (next: ViewMode) => void;
  total: number;
  showSystem: boolean;
  onToggleShowSystem: (next: boolean) => void;
  hiddenSystemCount: number;
  onNewRoutine: () => void;
}

function RoutinesHeader({
  view,
  onChangeView,
  total,
  showSystem,
  onToggleShowSystem,
  hiddenSystemCount,
  onNewRoutine,
}: RoutinesHeaderProps) {
  return (
    <header className="routines-header">
      <div
        style={{ flex: 1, display: "flex", flexDirection: "column", gap: 2 }}
      >
        <span className="routines-eyebrow">Scheduled work</span>
        <h2 className="routines-title" data-testid="routines-title">
          Scheduled Tasks
          <span className="routines-count">{total}</span>
        </h2>
      </div>
      <ShowSystemToggle
        checked={showSystem}
        onChange={onToggleShowSystem}
        hiddenCount={hiddenSystemCount}
      />
      <ViewToggle view={view} onChange={onChangeView} />
      <button
        type="button"
        onClick={onNewRoutine}
        data-testid="routines-new-button"
        style={{
          padding: "var(--space-1) var(--space-3)",
          fontSize: "var(--text-sm)",
          fontWeight: 500,
          border: "1px solid var(--accent)",
          borderRadius: "var(--radius-sm)",
          background: "var(--accent)",
          color: "white",
          cursor: "pointer",
        }}
      >
        + New scheduled task
      </button>
    </header>
  );
}

interface ShowSystemToggleProps {
  checked: boolean;
  onChange: (next: boolean) => void;
  hiddenCount: number;
}

function ShowSystemToggle({
  checked,
  onChange,
  hiddenCount,
}: ShowSystemToggleProps) {
  // When no system routines exist, hide the control entirely — no opt-in
  // is needed and an always-on checkbox would be noise.
  if (!checked && hiddenCount === 0) return null;
  const label = checked ? "Show system" : `Show system (${hiddenCount} hidden)`;
  return (
    <label
      style={{
        display: "inline-flex",
        alignItems: "center",
        gap: "var(--space-2)",
        fontSize: "var(--text-sm)",
        color: "var(--text-secondary)",
        cursor: "pointer",
        userSelect: "none",
      }}
      data-testid="routines-show-system-toggle"
    >
      <input
        type="checkbox"
        checked={checked}
        onChange={(e) => onChange(e.target.checked)}
        aria-label="Show system-managed scheduled tasks"
        style={{ cursor: "pointer" }}
      />
      {label}
    </label>
  );
}

interface ViewToggleProps {
  view: ViewMode;
  onChange: (next: ViewMode) => void;
}

function ViewToggle({ view, onChange }: ViewToggleProps) {
  return (
    <div
      role="tablist"
      aria-label="Scheduled tasks view"
      className="routines-view-toggle"
    >
      <button
        type="button"
        role="tab"
        aria-selected={view === "list"}
        className="routines-view-tab"
        data-testid="routines-view-list"
        onClick={() => onChange("list")}
      >
        List
      </button>
      <button
        type="button"
        role="tab"
        aria-selected={view === "calendar"}
        className="routines-view-tab"
        data-testid="routines-view-calendar"
        onClick={() => onChange("calendar")}
      >
        Calendar
      </button>
    </div>
  );
}

interface EmptyStateProps {
  hiddenSystemCount: number;
  onShowSystem: () => void;
}

function EmptyState({ hiddenSystemCount, onShowSystem }: EmptyStateProps) {
  const onlySystemHidden = hiddenSystemCount > 0;
  return (
    <div
      data-testid="routines-empty-state"
      style={{
        padding: "var(--space-7) var(--space-5)",
        textAlign: "center",
        color: "var(--text-tertiary)",
        fontSize: "var(--text-md)",
        lineHeight: 1.6,
      }}
    >
      <div
        style={{
          fontFamily: "var(--font-mono)",
          fontSize: "var(--text-2xs)",
          letterSpacing: "0.16em",
          textTransform: "uppercase",
          color: "var(--text-tertiary)",
          marginBottom: "var(--space-2)",
        }}
      >
        {onlySystemHidden ? "Filtered" : "Nothing scheduled"}
      </div>
      <div
        style={{
          fontSize: "var(--text-xl)",
          fontWeight: 600,
          marginBottom: "var(--space-2)",
          color: "var(--text-secondary)",
          letterSpacing: "-0.01em",
        }}
      >
        {onlySystemHidden
          ? `Only system routines (${hiddenSystemCount})`
          : "No scheduled tasks yet"}
      </div>
      <div
        style={{
          fontSize: "var(--text-sm)",
          maxWidth: 360,
          margin: "0 auto",
        }}
      >
        {onlySystemHidden ? (
          <>
            Every routine in this office is broker-managed plumbing.{" "}
            <button
              type="button"
              onClick={onShowSystem}
              style={{
                background: "none",
                border: "none",
                padding: 0,
                color: "var(--accent)",
                cursor: "pointer",
                font: "inherit",
                textDecoration: "underline",
              }}
            >
              Show system routines
            </button>{" "}
            to see them.
          </>
        ) : (
          <>
            Scheduled tasks run on a schedule, assigned to an agent. They appear here
            once an agent registers a cron job, a workflow gets a schedule, or a
            system loop publishes a heartbeat.
          </>
        )}
      </div>
    </div>
  );
}

function LoadingState() {
  return (
    <div
      data-testid="routines-loading"
      style={{
        padding: "var(--space-7) var(--space-5)",
        textAlign: "center",
        color: "var(--text-tertiary)",
        fontSize: "var(--text-sm)",
      }}
    >
      Loading scheduled tasks…
    </div>
  );
}

function ErrorState() {
  return (
    <div
      data-testid="routines-error"
      style={{
        padding: "var(--space-7) var(--space-5)",
        textAlign: "center",
        color: "var(--text-tertiary)",
        fontSize: "var(--text-sm)",
      }}
    >
      Could not load routines. Check your connection and try again.
    </div>
  );
}
