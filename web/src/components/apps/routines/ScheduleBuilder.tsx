import type { CSSProperties } from "react";

/**
 * Human-readable schedule editor. The user thinks in
 * "weekdays at 9 AM" or "every 30 minutes"; we compile that down to the
 * cron / interval shape the broker wants. Cron syntax is never typed —
 * an "Expression" footnote shows the resolved cron so power users can
 * verify what they're saving without us forcing them to author it.
 */

export type ScheduleFrequency =
  | "interval"
  | "hourly"
  | "daily"
  | "weekdays"
  | "weekly"
  | "monthly";

export type IntervalUnit = "minutes" | "hours";

export interface ScheduleValue {
  frequency: ScheduleFrequency;
  /** "minutes" or "hours" — only used when frequency is "interval". */
  intervalUnit: IntervalUnit;
  intervalAmount: number;
  /** 0-23. Used by daily / weekdays / weekly / monthly. */
  hour: number;
  /** 0-59. Used by all timed frequencies, plus "hourly" for "at minute X". */
  minute: number;
  /** Sun=0, Mon=1, …, Sat=6. Used by "weekly". */
  weekdays: number[];
  /** 1-31. Used by "monthly". */
  dayOfMonth: number;
}

/**
 * The broker rejects routines that fire more often than every 15 minutes
 * (see `validateRoutineCadence` in scheduler_lifecycle.go). The composer
 * mirrors that floor so the user can't author a schedule the server will
 * refuse — keeping the rule in one place means tightening or relaxing it
 * requires editing both sides of the wire, by design.
 */
export const MIN_ROUTINE_INTERVAL_MINUTES = 15;

export const DEFAULT_SCHEDULE: ScheduleValue = {
  frequency: "daily",
  intervalUnit: "minutes",
  intervalAmount: 30,
  hour: 9,
  minute: 0,
  weekdays: [1, 3, 5], // Mon, Wed, Fri — sensible "a few times a week" default
  dayOfMonth: 1,
};

const WEEKDAY_LABELS: { id: number; label: string }[] = [
  { id: 1, label: "Mon" },
  { id: 2, label: "Tue" },
  { id: 3, label: "Wed" },
  { id: 4, label: "Thu" },
  { id: 5, label: "Fri" },
  { id: 6, label: "Sat" },
  { id: 0, label: "Sun" },
];

interface ScheduleBuilderProps {
  value: ScheduleValue;
  onChange: (next: ScheduleValue) => void;
}

export function ScheduleBuilder({ value, onChange }: ScheduleBuilderProps) {
  return (
    <div
      data-testid="schedule-builder"
      style={{
        display: "flex",
        flexDirection: "column",
        gap: "var(--space-3)",
      }}
    >
      <FrequencyChips
        value={value.frequency}
        onChange={(f) => onChange({ ...value, frequency: f })}
      />

      {value.frequency === "interval" && (
        <IntervalEditor value={value} onChange={onChange} />
      )}

      {value.frequency === "hourly" && (
        <Row label="At minute">
          <MinuteInput
            value={value.minute}
            onChange={(m) => onChange({ ...value, minute: m })}
          />
          <span style={hintStyle}>past the hour</span>
        </Row>
      )}

      {(value.frequency === "daily" ||
        value.frequency === "weekdays" ||
        value.frequency === "weekly" ||
        value.frequency === "monthly") && (
        <Row label="At">
          <TimeInput value={value} onChange={onChange} />
        </Row>
      )}

      {value.frequency === "weekly" && (
        <Row label="On">
          <WeekdaySelector
            value={value.weekdays}
            onChange={(days) => onChange({ ...value, weekdays: days })}
          />
        </Row>
      )}

      {value.frequency === "monthly" && (
        <Row label="On day">
          <select
            className="input"
            value={value.dayOfMonth}
            onChange={(e) =>
              onChange({ ...value, dayOfMonth: parseInt(e.target.value, 10) })
            }
            style={{ width: 80 }}
            data-testid="schedule-day-of-month"
          >
            {Array.from({ length: 31 }, (_, i) => i + 1).map((d) => (
              <option key={d} value={d}>
                {ordinal(d)}
              </option>
            ))}
          </select>
          <span style={hintStyle}>of each month</span>
        </Row>
      )}

      <SchedulePreview value={value} />
    </div>
  );
}

interface FrequencyChipsProps {
  value: ScheduleFrequency;
  onChange: (next: ScheduleFrequency) => void;
}

function FrequencyChips({ value, onChange }: FrequencyChipsProps) {
  const items: { id: ScheduleFrequency; label: string }[] = [
    { id: "interval", label: "Every few minutes" },
    { id: "hourly", label: "Hourly" },
    { id: "daily", label: "Daily" },
    { id: "weekdays", label: "Weekdays" },
    { id: "weekly", label: "Weekly" },
    { id: "monthly", label: "Monthly" },
  ];
  return (
    <div
      role="radiogroup"
      aria-label="Schedule frequency"
      style={{ display: "flex", flexWrap: "wrap", gap: "var(--space-1)" }}
    >
      {items.map((item) => {
        const active = item.id === value;
        return (
          <button
            key={item.id}
            type="button"
            role="radio"
            aria-checked={active}
            onClick={() => onChange(item.id)}
            data-testid={`schedule-freq-${item.id}`}
            style={{
              padding: "5px var(--space-3)",
              fontSize: "var(--text-sm)",
              fontWeight: 500,
              border: `1px solid ${active ? "var(--accent)" : "var(--border)"}`,
              borderRadius: "var(--radius-full)",
              background: active ? "var(--accent-bg)" : "var(--bg-card)",
              color: active ? "var(--accent)" : "var(--text-secondary)",
              cursor: "pointer",
              transition: "background 120ms ease, border-color 120ms ease",
            }}
          >
            {item.label}
          </button>
        );
      })}
    </div>
  );
}

interface IntervalEditorProps {
  value: ScheduleValue;
  onChange: (next: ScheduleValue) => void;
}

function IntervalEditor({ value, onChange }: IntervalEditorProps) {
  // Floor depends on unit. In minutes mode the broker rejects anything
  // below MIN_ROUTINE_INTERVAL_MINUTES; in hours mode 1h is already
  // well above the floor.
  const minAmount = value.intervalUnit === "hours" ? 1 : MIN_ROUTINE_INTERVAL_MINUTES;
  return (
    <Row label="Every">
      <input
        type="number"
        className="input"
        min={minAmount}
        value={value.intervalAmount}
        onChange={(e) => {
          const raw = parseInt(e.target.value, 10) || minAmount;
          onChange({
            ...value,
            intervalAmount: Math.max(minAmount, raw),
          });
        }}
        data-testid="schedule-interval-amount"
        style={{ width: 96 }}
      />
      <select
        className="input"
        value={value.intervalUnit}
        onChange={(e) => {
          const nextUnit = e.target.value as IntervalUnit;
          const nextFloor = nextUnit === "hours" ? 1 : MIN_ROUTINE_INTERVAL_MINUTES;
          onChange({
            ...value,
            intervalUnit: nextUnit,
            intervalAmount: Math.max(nextFloor, value.intervalAmount),
          });
        }}
        data-testid="schedule-interval-unit"
        style={{ width: 110 }}
      >
        <option value="minutes">minutes</option>
        <option value="hours">hours</option>
      </select>
      {value.intervalUnit === "minutes" && (
        <span style={hintStyle}>
          (minimum {MIN_ROUTINE_INTERVAL_MINUTES})
        </span>
      )}
    </Row>
  );
}

interface TimeInputProps {
  value: ScheduleValue;
  onChange: (next: ScheduleValue) => void;
}

function TimeInput({ value, onChange }: TimeInputProps) {
  const formatted = `${pad2(value.hour)}:${pad2(value.minute)}`;
  return (
    <input
      type="time"
      className="input"
      value={formatted}
      onChange={(e) => {
        const [h, m] = e.target.value.split(":").map((s) => parseInt(s, 10));
        if (Number.isNaN(h) || Number.isNaN(m)) return;
        onChange({ ...value, hour: h, minute: m });
      }}
      data-testid="schedule-time"
      style={{ width: 120, fontFamily: "var(--font-mono)" }}
    />
  );
}

interface MinuteInputProps {
  value: number;
  onChange: (next: number) => void;
}

function MinuteInput({ value, onChange }: MinuteInputProps) {
  return (
    <select
      className="input"
      value={value}
      onChange={(e) => onChange(parseInt(e.target.value, 10))}
      style={{ width: 80 }}
      data-testid="schedule-minute"
    >
      {[0, 5, 10, 15, 20, 25, 30, 35, 40, 45, 50, 55].map((m) => (
        <option key={m} value={m}>
          {pad2(m)}
        </option>
      ))}
    </select>
  );
}

interface WeekdaySelectorProps {
  value: number[];
  onChange: (next: number[]) => void;
}

function WeekdaySelector({ value, onChange }: WeekdaySelectorProps) {
  const set = new Set(value);
  function toggle(day: number) {
    const next = new Set(set);
    if (next.has(day)) next.delete(day);
    else next.add(day);
    // Always preserve at least one selected day so we never emit an
    // impossible cron expression.
    if (next.size === 0) return;
    onChange(Array.from(next).sort((a, b) => a - b));
  }
  return (
    <div style={{ display: "inline-flex", gap: 4 }}>
      {WEEKDAY_LABELS.map((day) => {
        const active = set.has(day.id);
        return (
          <button
            key={day.id}
            type="button"
            role="switch"
            aria-checked={active}
            onClick={() => toggle(day.id)}
            data-testid={`schedule-weekday-${day.id}`}
            style={{
              padding: "5px 10px",
              fontSize: "var(--text-xs)",
              fontWeight: 600,
              border: `1px solid ${active ? "var(--accent)" : "var(--border)"}`,
              borderRadius: "var(--radius-sm)",
              background: active ? "var(--accent-bg)" : "var(--bg-card)",
              color: active ? "var(--accent)" : "var(--text-secondary)",
              cursor: "pointer",
              fontFamily: "var(--font-mono)",
              letterSpacing: "0.04em",
              textTransform: "uppercase",
            }}
          >
            {day.label}
          </button>
        );
      })}
    </div>
  );
}

interface RowProps {
  label: string;
  children: React.ReactNode;
}

function Row({ label, children }: RowProps) {
  return (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        gap: "var(--space-2)",
        flexWrap: "wrap",
      }}
    >
      <span
        style={{
          fontSize: "var(--text-xs)",
          color: "var(--text-tertiary)",
          fontFamily: "var(--font-mono)",
          letterSpacing: "0.04em",
          minWidth: 48,
        }}
      >
        {label}
      </span>
      {children}
    </div>
  );
}

function SchedulePreview({ value }: { value: ScheduleValue }) {
  return (
    <div
      style={{
        marginTop: "var(--space-2)",
        padding: "var(--space-3)",
        background: "var(--bg-card)",
        border: "1px dashed var(--border)",
        borderRadius: "var(--radius-sm)",
        fontSize: "var(--text-sm)",
        display: "flex",
        flexDirection: "column",
        gap: 4,
      }}
      data-testid="schedule-preview"
    >
      <div style={{ color: "var(--text)" }}>
        <strong style={{ fontWeight: 600 }}>Fires:</strong>{" "}
        {humanizeSchedule(value)}
      </div>
      <div
        style={{
          fontSize: "var(--text-xs)",
          color: "var(--text-tertiary)",
          fontFamily: "var(--font-mono)",
        }}
      >
        {previewLine(value)}
      </div>
    </div>
  );
}

const hintStyle: CSSProperties = {
  fontSize: "var(--text-sm)",
  color: "var(--text-tertiary)",
};

// ── Pure helpers ──────────────────────────────────────────────────

export interface CompiledSchedule {
  schedule_expr?: string;
  interval_minutes?: number;
}

/**
 * Reverse `compileSchedule` — turn a persisted job's schedule fields
 * back into a ScheduleValue the builder can edit. Falls back to the
 * default when the cron string doesn't match any builder-emitted shape,
 * so editing always lands the user on a valid starting point.
 */
export function parseSchedule(input: {
  schedule_expr?: string;
  cron?: string;
  interval_minutes?: number;
  interval_override?: number;
}): ScheduleValue {
  const interval = input.interval_override || input.interval_minutes || 0;
  const cron = (input.schedule_expr || input.cron || "").trim();
  if (!cron && interval > 0) {
    return intervalScheduleValue(interval);
  }
  if (cron) {
    const parts = cron.split(/\s+/);
    if (parts.length === 5) {
      const [minStr, hourStr, dom, month, dow] = parts;
      // Validate against standard cron ranges so legacy/manual input
      // like "99 25 * * *" doesn't round-trip a nonsense ScheduleValue
      // back to the broker. Out-of-range values fall through to the
      // interval / default safety net at the bottom.
      const minute = numericInRange(minStr, 0, 59);
      const hour = numericInRange(hourStr, 0, 23);
      // Hourly: "M * * * *"
      if (
        minute !== null &&
        hourStr === "*" &&
        dom === "*" &&
        month === "*" &&
        dow === "*"
      ) {
        return { ...DEFAULT_SCHEDULE, frequency: "hourly", minute };
      }
      // Daily / weekdays / weekly: "M H * * X"
      if (minute !== null && hour !== null && dom === "*" && month === "*") {
        if (dow === "*") {
          return { ...DEFAULT_SCHEDULE, frequency: "daily", hour, minute };
        }
        if (dow === "1-5") {
          return { ...DEFAULT_SCHEDULE, frequency: "weekdays", hour, minute };
        }
        const days = dow
          .split(",")
          .map((d) => numericInRange(d, 0, 6))
          .filter((d): d is number => d !== null);
        if (days.length > 0) {
          return {
            ...DEFAULT_SCHEDULE,
            frequency: "weekly",
            hour,
            minute,
            weekdays: days,
          };
        }
      }
      // Monthly: "M H D * *"
      const day = numericInRange(dom, 1, 31);
      if (
        minute !== null &&
        hour !== null &&
        day !== null &&
        month === "*" &&
        dow === "*"
      ) {
        return {
          ...DEFAULT_SCHEDULE,
          frequency: "monthly",
          hour,
          minute,
          dayOfMonth: day,
        };
      }
    }
  }
  return interval > 0 ? intervalScheduleValue(interval) : DEFAULT_SCHEDULE;
}

function intervalScheduleValue(minutes: number): ScheduleValue {
  if (minutes >= 60 && minutes % 60 === 0) {
    return {
      ...DEFAULT_SCHEDULE,
      frequency: "interval",
      intervalUnit: "hours",
      intervalAmount: minutes / 60,
    };
  }
  return {
    ...DEFAULT_SCHEDULE,
    frequency: "interval",
    intervalUnit: "minutes",
    intervalAmount: Math.max(1, minutes),
  };
}

function numericOrNull(s: string): number | null {
  if (!/^\d+$/.test(s)) return null;
  const n = Number.parseInt(s, 10);
  return Number.isNaN(n) ? null : n;
}

function numericInRange(s: string, min: number, max: number): number | null {
  const n = numericOrNull(s);
  if (n === null) return null;
  if (n < min || n > max) return null;
  return n;
}

/** Compile a builder value into the wire shape POST /scheduler expects. */
export function compileSchedule(value: ScheduleValue): CompiledSchedule {
  switch (value.frequency) {
    case "interval": {
      const minutes =
        value.intervalUnit === "hours"
          ? value.intervalAmount * 60
          : value.intervalAmount;
      return { interval_minutes: Math.max(1, minutes) };
    }
    case "hourly":
      return { schedule_expr: `${value.minute} * * * *` };
    case "daily":
      return { schedule_expr: `${value.minute} ${value.hour} * * *` };
    case "weekdays":
      return { schedule_expr: `${value.minute} ${value.hour} * * 1-5` };
    case "weekly": {
      const days = value.weekdays.length > 0 ? value.weekdays : [1];
      return {
        schedule_expr: `${value.minute} ${value.hour} * * ${days
          .slice()
          .sort((a, b) => a - b)
          .join(",")}`,
      };
    }
    case "monthly":
      return {
        schedule_expr: `${value.minute} ${value.hour} ${value.dayOfMonth} * *`,
      };
  }
}

/** Plain-English description of the schedule. */
export function humanizeSchedule(value: ScheduleValue): string {
  const time = `${pad12(value.hour)}:${pad2(value.minute)} ${value.hour >= 12 ? "PM" : "AM"}`;
  switch (value.frequency) {
    case "interval": {
      const unit = value.intervalUnit === "hours" ? "hour" : "minute";
      const plural = value.intervalAmount === 1 ? unit : `${unit}s`;
      return `Every ${value.intervalAmount} ${plural}`;
    }
    case "hourly":
      return value.minute === 0
        ? "Every hour, on the hour"
        : `Every hour, at :${pad2(value.minute)}`;
    case "daily":
      return `Every day at ${time}`;
    case "weekdays":
      return `Monday through Friday at ${time}`;
    case "weekly": {
      const days =
        value.weekdays.length > 0
          ? value.weekdays
              .slice()
              .sort((a, b) => weekdaySortKey(a) - weekdaySortKey(b))
              .map(weekdayName)
              .join(", ")
          : "Monday";
      return `${days} at ${time}`;
    }
    case "monthly":
      return `On the ${ordinal(value.dayOfMonth)} of each month at ${time}`;
  }
}

function previewLine(value: ScheduleValue): string {
  const compiled = compileSchedule(value);
  if (compiled.interval_minutes) {
    return `interval_minutes: ${compiled.interval_minutes}`;
  }
  return `cron: ${compiled.schedule_expr ?? ""}`;
}

function pad2(n: number): string {
  return n.toString().padStart(2, "0");
}

function pad12(n: number): string {
  const mod = n % 12;
  return mod === 0 ? "12" : mod.toString();
}

function ordinal(n: number): string {
  const v = n % 100;
  if (v >= 11 && v <= 13) return `${n}th`;
  switch (n % 10) {
    case 1:
      return `${n}st`;
    case 2:
      return `${n}nd`;
    case 3:
      return `${n}rd`;
    default:
      return `${n}th`;
  }
}

function weekdayName(day: number): string {
  return (
    {
      0: "Sunday",
      1: "Monday",
      2: "Tuesday",
      3: "Wednesday",
      4: "Thursday",
      5: "Friday",
      6: "Saturday",
    }[day] ?? "—"
  );
}

function weekdaySortKey(day: number): number {
  // Sort Mon→Sun for display, matching the chip order above.
  return day === 0 ? 7 : day;
}
