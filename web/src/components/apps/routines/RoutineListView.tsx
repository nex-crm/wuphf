import type { CSSProperties } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";

import {
  patchSchedulerJob,
  runSchedulerJob,
  type SchedulerJob,
} from "../../../api/client";
import { formatRelativeTime } from "../../../lib/format";
import { resolveObjectRoute } from "../../../lib/objectRoutes";
import {
  lastRunBadge,
  routineColor,
  routineKey,
  routineLabel,
  routineOwner,
  routineSchedule,
} from "./routineModel";

interface RoutineListViewProps {
  routines: SchedulerJob[];
  onSelect: (slug: string) => void;
}

/**
 * Paperclip-style flat list of routines. Each row is one routine, with a
 * coloured leading edge, label, schedule, owning agent, last-run badge,
 * next run, run-now button, enable toggle. Clicking anywhere outside the
 * action cluster opens the detail drawer.
 */
export function RoutineListView({ routines, onSelect }: RoutineListViewProps) {
  return (
    <div
      data-testid="routine-list"
      style={{
        display: "flex",
        flexDirection: "column",
        gap: "var(--space-2)",
        paddingBottom: "var(--space-4)",
      }}
    >
      <div className="routine-header-row">
        <span>Scheduled task</span>
        <span>Schedule</span>
        <span>Owner</span>
        <span>Last run</span>
        <span>Next run</span>
        <span style={{ textAlign: "right" }}>Actions</span>
      </div>

      {routines.map((routine) => (
        <RoutineListRow
          key={routineKey(routine)}
          routine={routine}
          onSelect={() => onSelect(routineKey(routine))}
        />
      ))}
    </div>
  );
}

interface RoutineListRowProps {
  routine: SchedulerJob;
  onSelect: () => void;
}

function RoutineListRow({ routine, onSelect }: RoutineListRowProps) {
  const queryClient = useQueryClient();
  const slug = routine.slug ?? routine.id ?? "";
  const color = routineColor(slug);
  const owner = routineOwner(routine);
  const schedule = routineSchedule(routine);
  const lastRun = lastRunBadge(routine);
  const nextRun = routine.next_run || routine.due_at;
  const enabled = routine.enabled !== false;

  const toggleMutation = useMutation({
    mutationFn: () => patchSchedulerJob(slug, { enabled: !enabled }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["scheduler"] });
    },
  });

  const runMutation = useMutation({
    mutationFn: () => runSchedulerJob(slug),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["scheduler"] });
    },
  });

  const rowStyle: CSSProperties = {
    // CSS custom property bound to this row's color so the border-left
    // can read it from the stylesheet instead of being set inline.
    ["--routine-color" as keyof CSSProperties]: color,
  };

  return (
    <div
      data-testid={`routine-row-${slug}`}
      data-enabled={enabled}
      onClick={onSelect}
      onKeyDown={(e) => {
        // Only activate the row on Enter/Space when the keypress
        // originates on the row itself. Without this check, Enter on a
        // nested control (owner link, Run-now button, enable toggle)
        // would hijack into navigation, swallowing the button's own
        // default action.
        if (e.target !== e.currentTarget) return;
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onSelect();
        }
      }}
      role="button"
      tabIndex={0}
      aria-label={`Open ${routineLabel(routine)}`}
      className="routine-row"
      style={rowStyle}
    >
      <div style={{ minWidth: 0 }}>
        <div className="routine-row-label">{routineLabel(routine)}</div>
        <div className="routine-row-slug" title={slug}>
          {slug}
        </div>
      </div>

      <div className="routine-row-meta">
        <span
          style={{
            fontFamily:
              schedule.kind === "cron" ? "var(--font-mono)" : undefined,
            fontSize:
              schedule.kind === "cron" ? "var(--text-xs)" : "var(--text-sm)",
          }}
        >
          {schedule.text}
        </span>
      </div>

      <OwnerCell owner={owner} />

      <div style={{ display: "flex", flexDirection: "column", gap: 2 }}>
        {lastRun && routine.last_run ? (
          <>
            <span
              style={{ fontSize: "var(--text-sm)", color: "var(--text)" }}
              title={new Date(routine.last_run).toLocaleString()}
            >
              {formatRelativeTime(routine.last_run)}
            </span>
            <span
              className={`badge badge-${
                lastRun.tone === "ok"
                  ? "green"
                  : lastRun.tone === "fail"
                    ? "red"
                    : "neutral"
              }`}
              style={{ alignSelf: "flex-start" }}
            >
              {lastRun.text}
            </span>
          </>
        ) : (
          <span
            style={{
              fontSize: "var(--text-xs)",
              color: "var(--text-tertiary)",
            }}
          >
            Never
          </span>
        )}
      </div>

      <div style={{ display: "flex", flexDirection: "column", gap: 2 }}>
        {nextRun && enabled ? (
          <>
            <span
              style={{ fontSize: "var(--text-sm)", color: "var(--text)" }}
              title={new Date(nextRun).toLocaleString()}
            >
              {formatRelativeTime(nextRun)}
            </span>
            <span
              style={{
                fontSize: "var(--text-xs)",
                color: "var(--text-tertiary)",
              }}
            >
              {new Date(nextRun).toLocaleTimeString(undefined, {
                hour: "2-digit",
                minute: "2-digit",
              })}
            </span>
          </>
        ) : (
          <span
            style={{
              fontSize: "var(--text-xs)",
              color: "var(--text-tertiary)",
            }}
          >
            {enabled ? "—" : "Paused"}
          </span>
        )}
      </div>

      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: "var(--space-2)",
        }}
        onClick={(e) => e.stopPropagation()}
        onKeyDown={(e) => e.stopPropagation()}
        role="presentation"
      >
        <button
          type="button"
          onClick={() => runMutation.mutate()}
          disabled={runMutation.isPending || !enabled}
          aria-label={`Run ${routineLabel(routine)} now`}
          style={actionButtonStyle}
        >
          {runMutation.isPending ? "…" : "Run now"}
        </button>

        <EnableToggle
          enabled={enabled}
          pending={toggleMutation.isPending}
          onToggle={() => toggleMutation.mutate()}
          label={routineLabel(routine)}
        />
      </div>
    </div>
  );
}

const actionButtonStyle: CSSProperties = {
  padding: "3px var(--space-3)",
  fontSize: "var(--text-xs)",
  fontWeight: 500,
  border: "1px solid var(--border)",
  borderRadius: "var(--radius-sm)",
  background: "transparent",
  color: "var(--text-secondary)",
  cursor: "pointer",
};

interface OwnerCellProps {
  owner: ReturnType<typeof routineOwner>;
}

function OwnerCell({ owner }: OwnerCellProps) {
  if (owner.kind === "system") {
    return <span className="badge badge-neutral">system</span>;
  }
  if (owner.kind === "workflow") {
    return <span className="badge badge-yellow">workflow</span>;
  }
  if (owner.kind === "unassigned" || !owner.slug) {
    return (
      <span
        style={{ fontSize: "var(--text-xs)", color: "var(--text-tertiary)" }}
      >
        Unassigned
      </span>
    );
  }
  const route = resolveObjectRoute({ kind: "agent", slug: owner.slug });
  return (
    <a
      href={route.href}
      onClick={(e) => e.stopPropagation()}
      style={{
        fontSize: "var(--text-sm)",
        color: "var(--accent)",
        textDecoration: "none",
        fontWeight: 500,
      }}
      title={`Open ${owner.slug}`}
    >
      {owner.slug}
    </a>
  );
}

interface EnableToggleProps {
  enabled: boolean;
  pending: boolean;
  onToggle: () => void;
  label: string;
}

function EnableToggle({
  enabled,
  pending,
  onToggle,
  label,
}: EnableToggleProps) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={enabled}
      aria-label={`${enabled ? "Disable" : "Enable"} ${label}`}
      disabled={pending}
      onClick={onToggle}
      style={{
        width: 34,
        height: 18,
        borderRadius: "var(--radius-full)",
        border: "1px solid var(--border)",
        background: enabled ? "var(--accent)" : "var(--bg-card)",
        position: "relative",
        cursor: pending ? "wait" : "pointer",
        padding: 0,
        transition: "background 120ms ease",
      }}
    >
      <span
        aria-hidden="true"
        style={{
          position: "absolute",
          top: 1,
          left: enabled ? 17 : 1,
          width: 14,
          height: 14,
          borderRadius: "50%",
          background: enabled ? "white" : "var(--text-tertiary)",
          transition: "left 120ms ease",
        }}
      />
    </button>
  );
}
