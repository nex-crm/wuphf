import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";

import {
  getSystemCronSpecs,
  type PatchSchedulerJobResponse,
  patchSchedulerJob,
  runSchedulerJob,
  type SchedulerJob,
} from "../../api/client";
import { formatRelativeTime } from "../../lib/format";
import { showNotice } from "../ui/Toast";
import { isCadenceSchedulerJob } from "./schedulerJobClassification";

const DEFAULT_FLOOR_MINUTES = 5;

/** Read-only system crons (enabled toggle + interval picker disabled). */
const READ_ONLY_SLUGS = new Set(["one-relay-events"]);

interface SystemSchedulesPanelProps {
  jobs: SchedulerJob[];
}

type FloorState =
  | { status: "loading"; values: Record<string, number> }
  | { status: "ready"; values: Record<string, number> }
  | { status: "fallback"; values: Record<string, number> };

/**
 * "System Schedules" section above the timeline-grouped job cards in
 * CalendarApp. Shows every cron-style scheduler entry with its toggle,
 * interval picker, last/next run, and source badge. Inline validation
 * mirrors the backend's MinFloor before allowing a PATCH.
 *
 * Floors are fetched once on mount from GET /scheduler/system-specs so
 * they stay in sync with the broker without any hardcoded mirror.
 *
 * Empty if the broker hasn't surfaced any system or interval crons (test
 * environments before registerSystemCrons runs).
 */
export function SystemSchedulesPanel({ jobs }: SystemSchedulesPanelProps) {
  const rows = useMemo(() => filterSchedulerRows(jobs), [jobs]);
  // floors: slug → min_floor_minutes, populated from the API on mount.
  const [floorState, setFloorState] = useState<FloorState>({
    status: "loading",
    values: {},
  });

  useEffect(() => {
    let aborted = false;
    getSystemCronSpecs()
      .then((specs) => {
        if (aborted) return;
        const map: Record<string, number> = {};
        for (const s of specs) {
          map[s.slug] = s.min_floor_minutes;
        }
        setFloorState({ status: "ready", values: map });
      })
      .catch((err: unknown) => {
        if (aborted) return;
        console.warn(
          "SystemSchedulesPanel: could not fetch system-specs; falling back to default floor",
          err,
        );
        setFloorState({ status: "fallback", values: {} });
      });
    return () => {
      aborted = true;
    };
  }, []);

  if (rows.length === 0) return null;

  return (
    <section style={{ marginBottom: 16 }}>
      <div
        style={{
          fontSize: 11,
          fontWeight: 600,
          textTransform: "uppercase",
          letterSpacing: "0.05em",
          color: "var(--text-tertiary)",
          padding: "8px 0 6px",
        }}
      >
        System Schedules
      </div>
      {rows.map((job) => (
        <ScheduleRow
          key={job.slug ?? job.id}
          job={job}
          floorState={floorState}
        />
      ))}
    </section>
  );
}

/**
 * Return the rows that belong in the System Schedules panel. Any job
 * that exposes `interval_minutes`, `system_managed`, or a cron expression
 * qualifies. Pure-task scheduler entries (one-shot due_at) are excluded —
 * they belong in the timeline view below.
 */
function filterSchedulerRows(jobs: SchedulerJob[]): SchedulerJob[] {
  return jobs.filter(isCadenceSchedulerJob);
}

interface ScheduleRowProps {
  job: SchedulerJob;
  floorState: FloorState;
}

function ScheduleRow({ job, floorState }: ScheduleRowProps) {
  const queryClient = useQueryClient();
  const slug = job.slug ?? "";
  const isReadOnly = READ_ONLY_SLUGS.has(slug);
  const isCron = typeof job.schedule_expr === "string" && job.schedule_expr;
  const isInterval = typeof job.interval_minutes === "number";

  const floorReady = floorState.status !== "loading";
  const floor = floorState.values[slug] ?? DEFAULT_FLOOR_MINUTES;
  const defaultInterval = job.interval_minutes ?? 0;
  const initialOverride = job.interval_override ?? 0;
  const initialEnabled = job.enabled !== false; // missing → assume enabled

  const [enabled, setEnabled] = useState(initialEnabled);
  const [overrideText, setOverrideText] = useState(
    initialOverride > 0 ? String(initialOverride) : String(defaultInterval),
  );
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);
  const [runPending, setRunPending] = useState(false);
  // Track last server-confirmed values so PATCH failures roll back to the
  // right state rather than the stale mount-time values.
  const committedTextRef = useRef(
    initialOverride > 0 ? String(initialOverride) : String(defaultInterval),
  );
  const committedOverrideRef = useRef(initialOverride);
  const committedEnabledRef = useRef(initialEnabled);

  const sourceLabel = describeSource(job);

  const submitPatch = useCallback(
    (patchBody: { enabled?: boolean; interval_override?: number }) => {
      if (!slug || isReadOnly) return;
      setPending(true);
      setError(null);
      patchSchedulerJob(slug, patchBody)
        .then((res: PatchSchedulerJobResponse) => {
          // Refresh the query so the row reflects server-canonical state
          // (status, next_run, etc.) — the optimistic update we already
          // applied stays visible while the refetch is in flight.
          queryClient.invalidateQueries({ queryKey: ["scheduler"] });
          if (typeof patchBody.enabled === "boolean") {
            showNotice(
              patchBody.enabled
                ? `${labelOf(job)} enabled`
                : `${labelOf(job)} disabled`,
              "success",
            );
          } else if (typeof patchBody.interval_override === "number") {
            showNotice(
              patchBody.interval_override === 0
                ? `${labelOf(job)} reset to default cadence`
                : `${labelOf(job)} now runs every ${patchBody.interval_override} min`,
              "success",
            );
          }
          // Sync local state with the server response in case the broker
          // adjusted it (e.g. clamping to default).
          if (typeof res.job?.enabled === "boolean") {
            setEnabled(res.job.enabled);
            committedEnabledRef.current = res.job.enabled;
          }
          if (typeof res.job?.interval_override === "number") {
            const next = res.job.interval_override;
            committedOverrideRef.current = next;
            const nextText =
              next > 0
                ? String(next)
                : String(res.job.interval_minutes ?? defaultInterval);
            setOverrideText(nextText);
            committedTextRef.current = nextText;
          }
        })
        .catch((e: Error) => {
          // Roll back optimistic state to last server-confirmed values.
          setEnabled(committedEnabledRef.current);
          setOverrideText(committedTextRef.current);
          setError(e.message || "Update failed");
          showNotice(`Couldn't update ${labelOf(job)}: ${e.message}`, "error");
        })
        .finally(() => setPending(false));
    },
    [slug, isReadOnly, job, queryClient, defaultInterval],
  );

  const handleToggle = useCallback(() => {
    if (isReadOnly || pending) return;
    const next = !enabled;
    setEnabled(next);
    submitPatch({ enabled: next });
  }, [isReadOnly, pending, enabled, submitPatch]);

  const handleIntervalCommit = useCallback(() => {
    if (isReadOnly || isCron) return;
    if (!floorReady) {
      setError("Scheduler floors are still loading");
      return;
    }
    const trimmed = overrideText.trim();
    if (trimmed === "") {
      setError("Interval is required");
      return;
    }
    const parsed = Number(trimmed);
    if (!(Number.isFinite(parsed) && Number.isInteger(parsed)) || parsed < 0) {
      setError("Must be a non-negative whole number");
      return;
    }
    if (parsed > 0 && parsed < floor) {
      setError(`Min interval is ${floor} min for this cron`);
      setOverrideText(committedTextRef.current);
      return;
    }
    // No-op: parsed matches what the server already has.
    const committedValue = Number(committedTextRef.current);
    if (Number.isFinite(committedValue) && parsed === committedValue) {
      setError(null);
      return;
    }
    submitPatch({ interval_override: parsed === defaultInterval ? 0 : parsed });
  }, [
    isReadOnly,
    isCron,
    floorReady,
    overrideText,
    floor,
    defaultInterval,
    submitPatch,
  ]);

  const handleRunNow = useCallback(() => {
    if (!slug || runPending) return;
    setRunPending(true);
    runSchedulerJob(slug)
      .then(() => {
        queryClient.invalidateQueries({ queryKey: ["scheduler"] });
        showNotice(`${labelOf(job)} triggered`, "success");
      })
      .catch((e: Error) => {
        showNotice(`Couldn't trigger ${labelOf(job)}: ${e.message}`, "error");
      })
      .finally(() => setRunPending(false));
  }, [slug, runPending, job, queryClient]);

  const lastRunChip = describeLastRun(job);
  const nextRunCountdown = describeNextRun(job);

  return (
    <article
      className="app-card"
      style={{
        marginBottom: 8,
        opacity: enabled ? 1 : 0.7,
      }}
      aria-labelledby={`schedule-${slug || "row"}-label`}
    >
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 8,
          marginBottom: 6,
          flexWrap: "wrap",
        }}
      >
        <span
          id={`schedule-${slug || "row"}-label`}
          className="app-card-title"
          style={{ marginBottom: 0 }}
        >
          {labelOf(job)}
        </span>
        <SourceBadge source={sourceLabel} />
        {!enabled ? <span className="badge badge-muted">disabled</span> : null}
        {lastRunChip ? (
          <span className={`badge ${lastRunChip.cls}`}>{lastRunChip.text}</span>
        ) : null}
      </div>

      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 12,
          flexWrap: "wrap",
          fontSize: 12,
          color: "var(--text-secondary)",
        }}
      >
        {isReadOnly ? (
          <span style={{ fontFamily: "var(--font-mono)" }}>
            Every {defaultInterval}m (read-only)
          </span>
        ) : isCron ? (
          <span style={{ fontFamily: "var(--font-mono)" }}>
            cron: {job.schedule_expr}
          </span>
        ) : isInterval ? (
          <IntervalPicker
            value={overrideText}
            disabled={pending || !floorReady}
            onChange={(v) => {
              setOverrideText(v);
              setError(null);
            }}
            onBlur={handleIntervalCommit}
            ariaLabel={`Interval in minutes for ${labelOf(job)}`}
            floor={floor}
            defaultInterval={defaultInterval}
          />
        ) : (
          <span style={{ color: "var(--text-tertiary)" }}>
            (no cadence reported)
          </span>
        )}

        <ToggleSwitch
          enabled={enabled}
          disabled={isReadOnly || pending}
          onToggle={handleToggle}
          ariaLabel={`${enabled ? "Disable" : "Enable"} ${labelOf(job)}`}
        />

        <button
          type="button"
          disabled={runPending}
          onClick={handleRunNow}
          aria-label={`Run ${labelOf(job)} now`}
          style={{
            padding: "2px 8px",
            fontSize: 11,
            fontWeight: 500,
            background: "transparent",
            border: "1px solid var(--border)",
            borderRadius: 4,
            color: "var(--text-secondary)",
            cursor: runPending ? "not-allowed" : "pointer",
            opacity: runPending ? 0.6 : 1,
            transition: "opacity 0.1s",
          }}
        >
          {runPending ? "…" : "Run now"}
        </button>

        {nextRunCountdown ? (
          <span style={{ marginLeft: "auto" }}>{nextRunCountdown}</span>
        ) : null}
      </div>

      {error ? (
        <div
          role="alert"
          style={{
            marginTop: 6,
            fontSize: 12,
            color: "var(--red, #c43e3e)",
          }}
        >
          {error}
        </div>
      ) : null}

      {!isReadOnly && isInterval && committedOverrideRef.current > 0 ? (
        <div
          style={{
            marginTop: 6,
            fontSize: 11,
            color: "var(--text-tertiary)",
          }}
        >
          Override active. Default: every {defaultInterval}m.
        </div>
      ) : null}
    </article>
  );
}

interface IntervalPickerProps {
  value: string;
  disabled: boolean;
  onChange: (v: string) => void;
  onBlur: () => void;
  ariaLabel: string;
  floor: number;
  defaultInterval: number;
}

function IntervalPicker({
  value,
  disabled,
  onChange,
  onBlur,
  ariaLabel,
  floor,
  defaultInterval,
}: IntervalPickerProps) {
  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 4 }}>
      <span>Every</span>
      <input
        type="number"
        min={floor}
        step={1}
        inputMode="numeric"
        value={value}
        disabled={disabled}
        onChange={(e) => onChange(e.target.value)}
        onBlur={onBlur}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            (e.target as HTMLInputElement).blur();
          }
        }}
        aria-label={ariaLabel}
        style={{
          width: 60,
          padding: "2px 6px",
          background: "var(--bg-card, #fff)",
          border: "1px solid var(--border)",
          borderRadius: 4,
          fontFamily: "var(--font-mono)",
          fontSize: 12,
          color: "var(--text)",
        }}
      />
      <span>min</span>
      <span
        style={{
          fontSize: 11,
          color: "var(--text-tertiary)",
        }}
      >
        (default: {defaultInterval}m, min: {floor}m)
      </span>
    </span>
  );
}

interface ToggleSwitchProps {
  enabled: boolean;
  disabled: boolean;
  onToggle: () => void;
  ariaLabel: string;
}

function ToggleSwitch({
  enabled,
  disabled,
  onToggle,
  ariaLabel,
}: ToggleSwitchProps) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={enabled}
      aria-label={ariaLabel}
      disabled={disabled}
      onClick={onToggle}
      style={{
        position: "relative",
        width: 36,
        height: 20,
        background: enabled ? "var(--green, #2eb472)" : "var(--neutral-300)",
        border: "none",
        borderRadius: 999,
        cursor: disabled ? "not-allowed" : "pointer",
        opacity: disabled ? 0.6 : 1,
        transition: "background 0.15s",
        padding: 0,
      }}
    >
      <span
        aria-hidden="true"
        style={{
          position: "absolute",
          top: 2,
          left: enabled ? 18 : 2,
          width: 16,
          height: 16,
          background: "#fff",
          borderRadius: "50%",
          transition: "left 0.15s",
          boxShadow: "0 1px 2px rgba(0,0,0,0.2)",
        }}
      />
    </button>
  );
}

interface SourceBadgeProps {
  source: "system" | "agent" | "workflow";
}

function SourceBadge({ source }: SourceBadgeProps) {
  const cls =
    source === "system"
      ? "badge badge-neutral"
      : source === "agent"
        ? "badge badge-accent"
        : "badge badge-yellow";
  return <span className={cls}>{source}</span>;
}

function describeSource(job: SchedulerJob): "system" | "agent" | "workflow" {
  if (job.system_managed) return "system";
  if (
    job.target_type === "workflow" ||
    job.kind === "workflow" ||
    typeof job.schedule_expr === "string"
  ) {
    return "workflow";
  }
  return "agent";
}

function labelOf(job: SchedulerJob): string {
  return job.label || job.name || job.slug || "(unnamed)";
}

function describeLastRun(
  job: SchedulerJob,
): { text: string; cls: string } | null {
  const status = (job.last_run_status || "").toLowerCase();
  if (!status) return null;
  if (status === "ok" || status === "success") {
    return {
      text: `OK · ${job.last_run ? formatRelativeTime(job.last_run) : "—"}`,
      cls: "badge-green",
    };
  }
  if (status === "failed" || status === "error") {
    return {
      text: `Failed · ${job.last_run ? formatRelativeTime(job.last_run) : "—"}`,
      cls: "badge badge-red",
    };
  }
  return { text: status, cls: "badge-neutral" };
}

function describeNextRun(job: SchedulerJob): string | null {
  const target = job.next_run || job.due_at;
  if (!target) return null;
  return `Next ${formatRelativeTime(target)}`;
}
