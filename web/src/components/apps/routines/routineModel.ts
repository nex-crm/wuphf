import type { SchedulerJob } from "../../../api/client";

/**
 * Display label for a routine — the canonical human-readable string for
 * any surface that renders a SchedulerJob.
 */
export function routineLabel(job: SchedulerJob): string {
  return job.label || job.name || job.slug || "(unnamed routine)";
}

/**
 * Resolve the owning agent for a routine.
 *
 * Returns the agent slug when the routine targets an agent (or runs as an
 * agent loop). System-managed crons that don't bind to a single agent are
 * reported as "system" so the UI can render a neutral badge instead of
 * leaving the owner column blank.
 */
export function routineOwner(job: SchedulerJob): {
  slug: string | null;
  kind: "agent" | "system" | "workflow" | "unassigned";
} {
  if (job.system_managed) return { slug: null, kind: "system" };
  if (job.target_type === "agent" && job.target_id) {
    return { slug: job.target_id, kind: "agent" };
  }
  // The owning agent is the office agent that scheduled the job (e.g. "ceo").
  // This is NOT job.provider, which is the integration vendor ("composio" /
  // "one"). A workflow job carries both, so resolve the agent first and never
  // fall back to the vendor as if it were an agent.
  if (job.agent) {
    return { slug: job.agent, kind: "agent" };
  }
  if (
    job.target_type === "workflow" ||
    job.kind === "workflow" ||
    job.kind?.endsWith("_workflow")
  ) {
    return { slug: null, kind: "workflow" };
  }
  // Legacy fallback: some older jobs stored an agent slug in `provider`.
  // Known integration vendors are NOT agents, so exclude them.
  if (job.provider && !isVendorProvider(job.provider)) {
    return { slug: job.provider, kind: "agent" };
  }
  return { slug: null, kind: "unassigned" };
}

/** Integration vendors that may appear in `job.provider` but are never agents. */
const VENDOR_PROVIDERS = new Set(["composio", "one", "system"]);

function isVendorProvider(provider: string): boolean {
  return VENDOR_PROVIDERS.has(provider.trim().toLowerCase());
}

/**
 * Human-readable schedule summary. Prefers cron when present, falls back to
 * interval-in-minutes. Returns null when the routine has no recurring
 * trigger (one-shot follow-ups).
 */
export function routineSchedule(job: SchedulerJob): {
  text: string;
  kind: "cron" | "interval" | "once";
} {
  const cron = job.schedule_expr || job.cron;
  if (cron && cron.trim() !== "") {
    return { text: prettyCron(cron), kind: "cron" };
  }
  const interval = job.interval_override || job.interval_minutes;
  if (interval && interval > 0) {
    return { text: prettyInterval(interval), kind: "interval" };
  }
  return { text: "One-shot", kind: "once" };
}

/**
 * Map a 5-field cron expression to plain English. Handles every shape
 * the ScheduleBuilder can emit (hourly / daily / weekdays / weekly /
 * monthly), plus a few common "every N minutes" patterns. Falls back to
 * the raw expression only when we genuinely can't read it.
 */
function prettyCron(expr: string): string {
  const normalized = expr.trim().replace(/\s+/g, " ");
  const literal: Record<string, string> = {
    "* * * * *": "Every minute",
    "*/5 * * * *": "Every 5 minutes",
    "*/10 * * * *": "Every 10 minutes",
    "*/15 * * * *": "Every 15 minutes",
    "*/30 * * * *": "Every 30 minutes",
  };
  if (literal[normalized]) return literal[normalized];

  const parts = normalized.split(" ");
  if (parts.length !== 5) return `cron: ${normalized}`;
  const [minute, hour, dom, month, dow] = parts;

  // Hourly: minute fixed, every hour.
  if (
    isNumeric(minute) &&
    hour === "*" &&
    dom === "*" &&
    month === "*" &&
    dow === "*"
  ) {
    const m = Number.parseInt(minute, 10);
    return m === 0 ? "Every hour, on the hour" : `Every hour at :${pad2(m)}`;
  }

  // Daily / weekdays / weekly variants share "minute hour * * X".
  if (isNumeric(minute) && isNumeric(hour) && dom === "*" && month === "*") {
    const m = Number.parseInt(minute, 10);
    const h = Number.parseInt(hour, 10);
    const time = formatClock(h, m);
    if (dow === "*") return `Every day at ${time}`;
    if (dow === "1-5") return `Weekdays at ${time}`;
    if (dow === "0,6" || dow === "6,0") return `Weekends at ${time}`;
    const days = dow
      .split(",")
      .map((d) => Number.parseInt(d, 10))
      .filter((d) => !Number.isNaN(d));
    if (days.length > 0) {
      const labels = days
        .sort((a, b) => weekdaySortKey(a) - weekdaySortKey(b))
        .map(weekdayName)
        .join(", ");
      return `${labels} at ${time}`;
    }
  }

  // Monthly: "minute hour D * *".
  if (
    isNumeric(minute) &&
    isNumeric(hour) &&
    isNumeric(dom) &&
    month === "*" &&
    dow === "*"
  ) {
    const m = Number.parseInt(minute, 10);
    const h = Number.parseInt(hour, 10);
    const day = Number.parseInt(dom, 10);
    return `Day ${ordinal(day)} of each month at ${formatClock(h, m)}`;
  }

  return `cron: ${normalized}`;
}

function isNumeric(s: string): boolean {
  return /^\d+$/.test(s);
}

function pad2(n: number): string {
  return n.toString().padStart(2, "0");
}

function formatClock(h: number, m: number): string {
  const period = h >= 12 ? "PM" : "AM";
  const hour12 = h % 12 === 0 ? 12 : h % 12;
  return `${hour12}:${pad2(m)} ${period}`;
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
      0: "Sun",
      1: "Mon",
      2: "Tue",
      3: "Wed",
      4: "Thu",
      5: "Fri",
      6: "Sat",
    }[day] ?? "—"
  );
}

function weekdaySortKey(day: number): number {
  return day === 0 ? 7 : day;
}

function prettyInterval(minutes: number): string {
  if (minutes < 60) return `Every ${minutes} min`;
  if (minutes % 60 === 0) {
    const hours = minutes / 60;
    if (hours === 24) return "Every day";
    if (hours === 1) return "Every hour";
    return `Every ${hours} hours`;
  }
  return `Every ${minutes} min`;
}

/**
 * Project the next fire times for a routine within [from, to). For cron
 * routines we don't have a JS cron parser available so we surface only the
 * stored `next_run` (one occurrence). For interval routines we walk forward
 * by `interval` from `next_run` until we exit the window or exceed maxFires.
 */
export function projectFires(
  job: SchedulerJob,
  from: Date,
  to: Date,
  maxFires = 200,
): Date[] {
  const first = parseAnchor(job);
  if (!first) return [];

  const intervalMs = projectionStepMs(job);
  if (intervalMs === null) {
    return inWindow(first, from, to) ? [first] : [];
  }
  return walkFires(first, intervalMs, from, to, maxFires);
}

function parseAnchor(job: SchedulerJob): Date | null {
  const anchor = job.next_run || job.due_at;
  if (!anchor) return null;
  const d = new Date(anchor);
  return Number.isNaN(d.getTime()) ? null : d;
}

/**
 * Step in milliseconds for projecting future fires, or null when the
 * routine has no projectable cadence (cron-driven or one-shot). Cron
 * routines are intentionally treated as single-occurrence here because
 * the JS bundle does not ship a cron parser.
 */
function projectionStepMs(job: SchedulerJob): number | null {
  const cron = job.schedule_expr || job.cron;
  if (cron && cron.trim() !== "") return null;
  const interval = job.interval_override || job.interval_minutes;
  if (!interval || interval <= 0) return null;
  return interval * 60_000;
}

function inWindow(t: Date, from: Date, to: Date): boolean {
  return t >= from && t < to;
}

function walkFires(
  first: Date,
  stepMs: number,
  from: Date,
  to: Date,
  maxFires: number,
): Date[] {
  const out: Date[] = [];
  // Fast-forward stale anchors into the visible window. A 5-minute
  // interval routine with an anchor from last month would otherwise
  // step ~8500 times before reaching `from`; arithmetic skips it in
  // one shot.
  let startMs = first.getTime();
  const fromMs = from.getTime();
  if (startMs < fromMs && stepMs > 0) {
    const gap = fromMs - startMs;
    const skip = Math.ceil(gap / stepMs);
    startMs += skip * stepMs;
  }
  for (let t = startMs; t < to.getTime(); t += stepMs) {
    if (t >= fromMs) out.push(new Date(t));
    if (out.length >= maxFires) break;
  }
  return out;
}

/**
 * Stable color assignment per routine slug so calendar chips stay the same
 * color as the user navigates between months.
 */
export function routineColor(slug: string): string {
  const palette = [
    "var(--accent)",
    "var(--purple, #cf72d9)",
    "var(--green, #03a04c)",
    "var(--yellow, #EAB308)",
    "var(--orange, #df750c)",
    "var(--red, #e23428)",
    "var(--cyan, #60A5FA)",
    "var(--magenta, #df3aa8)",
  ];
  let hash = 0;
  for (let i = 0; i < slug.length; i++) {
    hash = (hash * 31 + slug.charCodeAt(i)) >>> 0;
  }
  return palette[hash % palette.length];
}

export function lastRunBadge(
  job: SchedulerJob,
): { text: string; tone: "ok" | "fail" | "muted" } | null {
  const status = (job.last_run_status || "").toLowerCase();
  if (!(status || job.last_run)) return null;
  if (status === "ok" || status === "success") {
    return { text: "ok", tone: "ok" };
  }
  if (status === "failed" || status === "error") {
    return { text: "failed", tone: "fail" };
  }
  return { text: status || "ran", tone: "muted" };
}

export function routineKey(job: SchedulerJob): string {
  return job.slug ?? job.id ?? routineLabel(job);
}
