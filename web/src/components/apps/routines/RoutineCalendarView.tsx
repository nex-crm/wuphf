import type { CSSProperties } from "react";
import { useMemo, useState } from "react";

import type { SchedulerJob } from "../../../api/client";
import {
  projectFires,
  routineColor,
  routineKey,
  routineLabel,
} from "./routineModel";

interface RoutineCalendarViewProps {
  routines: SchedulerJob[];
  onSelect: (slug: string) => void;
}

/**
 * Month grid — Google Calendar style. Cells are days; each cell stacks
 * coloured chips for every routine scheduled to fire that day. Clicking a
 * chip opens the routine detail drawer. Cells with overflow expose a
 * "+N more" toggle that reveals the rest in-place.
 */
export function RoutineCalendarView({
  routines,
  onSelect,
}: RoutineCalendarViewProps) {
  const today = useMemo(() => startOfDay(new Date()), []);
  const [monthAnchor, setMonthAnchor] = useState<Date>(() =>
    startOfMonth(today),
  );

  const grid = useMemo(() => buildMonthGrid(monthAnchor), [monthAnchor]);
  const firesByDay = useMemo(() => {
    const map = new Map<string, Array<{ job: SchedulerJob; at: Date }>>();
    const [first] = grid;
    const last = grid.at(-1) ?? first;
    const windowEnd = addDays(last, 1);
    for (const routine of routines) {
      const fires = projectFires(routine, first, windowEnd);
      for (const at of fires) {
        const key = isoDay(at);
        const bucket = map.get(key) ?? [];
        bucket.push({ job: routine, at });
        map.set(key, bucket);
      }
    }
    return map;
  }, [routines, grid]);

  const monthLabel = monthAnchor.toLocaleDateString(undefined, {
    month: "long",
    year: "numeric",
    timeZone: "UTC",
  });

  return (
    <div
      data-testid="routine-calendar"
      style={{
        display: "flex",
        flexDirection: "column",
        gap: "var(--space-2)",
      }}
    >
      <div
        style={{
          display: "flex",
          alignItems: "baseline",
          gap: "var(--space-2)",
          padding: "0 var(--space-1) var(--space-1)",
        }}
      >
        <h3
          style={{
            fontSize: "var(--text-md)",
            fontWeight: 600,
            margin: 0,
            flex: 1,
            letterSpacing: "-0.01em",
            color: "var(--text)",
          }}
        >
          {monthLabel}
        </h3>
        <button
          type="button"
          onClick={() => setMonthAnchor(addMonths(monthAnchor, -1))}
          aria-label="Previous month"
          style={navBtnStyle}
        >
          ‹
        </button>
        <button
          type="button"
          onClick={() => setMonthAnchor(startOfMonth(new Date()))}
          aria-label="Go to current month"
          style={navBtnStyle}
        >
          Today
        </button>
        <button
          type="button"
          onClick={() => setMonthAnchor(addMonths(monthAnchor, 1))}
          aria-label="Next month"
          style={navBtnStyle}
        >
          ›
        </button>
      </div>

      <div className="routine-month-grid">
        {WEEKDAY_LABELS.map((label) => (
          <div key={label} className="routine-month-weekday">
            {label}
          </div>
        ))}

        {grid.map((day) => {
          const dayKey = isoDay(day);
          const inMonth = day.getUTCMonth() === monthAnchor.getUTCMonth();
          const isToday = sameDay(day, today);
          const fires = firesByDay.get(dayKey) ?? [];
          return (
            <DayCell
              key={dayKey}
              day={day}
              fires={fires}
              inMonth={inMonth}
              isToday={isToday}
              onSelect={onSelect}
            />
          );
        })}
      </div>
    </div>
  );
}

const navBtnStyle: CSSProperties = {
  padding: "3px var(--space-3)",
  fontSize: "var(--text-sm)",
  fontWeight: 500,
  background: "transparent",
  border: "1px solid var(--border)",
  borderRadius: "var(--radius-sm)",
  color: "var(--text-secondary)",
  cursor: "pointer",
};

const WEEKDAY_LABELS = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];

const MAX_VISIBLE_CHIPS = 3;

interface DayCellProps {
  day: Date;
  fires: Array<{ job: SchedulerJob; at: Date }>;
  inMonth: boolean;
  isToday: boolean;
  onSelect: (slug: string) => void;
}

function DayCell({ day, fires, inMonth, isToday, onSelect }: DayCellProps) {
  const [expanded, setExpanded] = useState(false);
  const sorted = useMemo(
    () => [...fires].sort((a, b) => a.at.getTime() - b.at.getTime()),
    [fires],
  );
  const visible = expanded ? sorted : sorted.slice(0, MAX_VISIBLE_CHIPS);
  const overflow = sorted.length - visible.length;

  return (
    <div
      data-testid={`routine-day-${isoDay(day)}`}
      data-today={isToday || undefined}
      data-out-of-month={!inMonth || undefined}
      className="routine-day"
    >
      <div className="routine-day-number">{day.getUTCDate()}</div>

      {visible.map((fire) => (
        <RoutineChip
          key={`${routineKey(fire.job)}-${fire.at.toISOString()}`}
          job={fire.job}
          at={fire.at}
          onSelect={() => onSelect(routineKey(fire.job))}
        />
      ))}

      {overflow > 0 && (
        <button
          type="button"
          onClick={() => setExpanded(true)}
          style={{
            border: "none",
            background: "transparent",
            color: "var(--text-tertiary)",
            fontSize: "var(--text-2xs)",
            padding: "2px 4px",
            cursor: "pointer",
            textAlign: "left",
          }}
          aria-label={`Show ${overflow} more routines on ${day.toDateString()}`}
        >
          +{overflow} more
        </button>
      )}
    </div>
  );
}

interface RoutineChipProps {
  job: SchedulerJob;
  at: Date;
  onSelect: () => void;
}

function RoutineChip({ job, at, onSelect }: RoutineChipProps) {
  const slug = job.slug ?? job.id ?? "";
  const color = routineColor(slug);
  const enabled = job.enabled !== false;
  const time = at.toLocaleTimeString(undefined, {
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  });

  const style: CSSProperties = {
    ["--routine-color" as keyof CSSProperties]: color,
  };

  return (
    <button
      type="button"
      onClick={onSelect}
      data-testid={`routine-chip-${slug}`}
      data-enabled={enabled}
      title={`${routineLabel(job)} · ${time}`}
      className="routine-chip"
      style={style}
    >
      <span className="routine-chip-time">{time}</span>
      <span className="routine-chip-label">{routineLabel(job)}</span>
    </button>
  );
}

// ── Date helpers (UTC-anchored to match the existing CalendarApp helpers) ──

function startOfDay(date: Date): Date {
  return new Date(
    Date.UTC(date.getUTCFullYear(), date.getUTCMonth(), date.getUTCDate()),
  );
}

function startOfMonth(date: Date): Date {
  return new Date(Date.UTC(date.getUTCFullYear(), date.getUTCMonth(), 1));
}

function addDays(date: Date, n: number): Date {
  const d = new Date(date.getTime());
  d.setUTCDate(d.getUTCDate() + n);
  return d;
}

function addMonths(date: Date, n: number): Date {
  const d = new Date(date.getTime());
  d.setUTCMonth(d.getUTCMonth() + n);
  return d;
}

function isoDay(date: Date): string {
  return date.toISOString().slice(0, 10);
}

function sameDay(a: Date, b: Date): boolean {
  return (
    a.getUTCFullYear() === b.getUTCFullYear() &&
    a.getUTCMonth() === b.getUTCMonth() &&
    a.getUTCDate() === b.getUTCDate()
  );
}

/**
 * Build a 6-row × 7-col grid anchored on the first Monday on/before the
 * 1st of the given month. The grid always spans 42 cells so the calendar
 * doesn't visually jump between months with different week counts.
 */
function buildMonthGrid(monthAnchor: Date): Date[] {
  const first = startOfMonth(monthAnchor);
  const dow = first.getUTCDay(); // 0=Sun … 6=Sat
  const diff = dow === 0 ? -6 : 1 - dow;
  const gridStart = addDays(first, diff);
  const out: Date[] = [];
  for (let i = 0; i < 42; i++) out.push(addDays(gridStart, i));
  return out;
}
