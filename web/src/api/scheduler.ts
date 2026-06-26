// Scheduler / Routines API surface — extracted from client.ts to keep
// that file under the file-size cap. Wire shapes mirror the Go broker
// in internal/team/scheduler_*.go.

import { trackOn } from "../lib/analytics";
import { get, patch, post } from "./client";

export interface SchedulerJob {
  id?: string;
  slug?: string;
  name?: string;
  label?: string;
  kind?: string;
  cron?: string;
  next_run?: string;
  last_run?: string;
  due_at?: string;
  status?: string;
  /** Interval-driven cadence in minutes (system crons + interval workflows). */
  interval_minutes?: number;
  /** Cron expression for cron-driven workflow jobs. */
  schedule_expr?: string;
  /** Office agent slug that owns this job (the agent that scheduled it). For
   * workflow jobs this is the owning agent — distinct from `provider`, which is
   * the integration vendor and must never be shown as an agent. */
  agent?: string;
  /** Integration vendor for workflow jobs ("composio" | "one" | "system"). NOT
   * an agent slug — use `agent` / `target_id` for the owning agent. */
  provider?: string;
  /** Target type ("workflow" | "skill" | …) when surfaced by the runtime. */
  target_type?: string;
  target_id?: string;
  // PR 8 Lane G/H — cron registry surface fields.
  /** Whether the cron is currently enabled. */
  enabled?: boolean;
  /** Human override for the cadence in minutes. 0/missing = use default. */
  interval_override?: number;
  /** "ok" | "failed" — chip on the row. */
  last_run_status?: string;
  /** True for crons that self-register at broker startup. */
  system_managed?: boolean;
  /** Free-form payload attached to the job (instructions, templated input). */
  payload?: string;
  /** Channel slug where the routine posts when it fires. */
  channel?: string;
}

export function getScheduler(opts?: { dueOnly?: boolean }) {
  const params: Record<string, string> = {};
  if (opts?.dueOnly) params.due_only = "true";
  return get<{ jobs: SchedulerJob[] }>("/scheduler", params);
}

export interface PatchSchedulerJobBody {
  enabled?: boolean;
  /** Minutes; 0 clears the override. Must be >= the cron's MinFloor. */
  interval_override?: number;
  /** Content edits — any non-empty field triggers a revision snapshot. */
  label?: string;
  schedule_expr?: string;
  interval_minutes?: number;
  payload?: string;
  target_type?: string;
  target_id?: string;
  /** Channel slug the routine posts into; empty string means owner DM. */
  channel?: string;
  /** Short human note attached to the new revision. Optional. */
  change_note?: string;
}

export interface PatchSchedulerJobResponse {
  job: SchedulerJob;
}

/**
 * Update the enabled flag and / or interval_override for a scheduler job.
 * Backed by PATCH /scheduler/{slug} (PR 8 Lane G).
 */
export function patchSchedulerJob(
  slug: string,
  body: PatchSchedulerJobBody,
): Promise<PatchSchedulerJobResponse> {
  return patch<PatchSchedulerJobResponse>(
    `/scheduler/${encodeURIComponent(slug)}`,
    body,
  );
}

/**
 * Wire shape for one entry from GET /scheduler/system-specs.
 * Mirrors systemCronSpecJSON in internal/team/broker_scheduler.go.
 */
export interface SystemCronSpec {
  slug: string;
  min_floor_minutes: number;
  default_interval_minutes: number;
  description: string;
}

/**
 * Fetch the system-cron spec registry from the broker so the UI can
 * derive per-slug MinFloor values at runtime instead of maintaining a
 * hardcoded mirror constant.
 */
export async function getSystemCronSpecs(): Promise<SystemCronSpec[]> {
  const res = await get<{ specs: SystemCronSpec[] }>("/scheduler/system-specs");
  return res.specs ?? [];
}

/**
 * Force-trigger a scheduler job once, immediately. Does not affect the
 * recurring schedule or next_run. Backed by POST /scheduler/{slug}/run (PR 9).
 */
export async function runSchedulerJob(
  slug: string,
): Promise<{ triggered: boolean; slug: string; at: string }> {
  return post<{ triggered: boolean; slug: string; at: string }>(
    `/scheduler/${encodeURIComponent(slug)}/run`,
  );
}

/**
 * Single fire record for a scheduler job. Persisted by the broker as a
 * bounded ring buffer per slug — see `recordSchedulerRunLocked` in
 * `internal/team/scheduler_runs.go`.
 */
export interface SchedulerRun {
  slug: string;
  started_at: string;
  finished_at?: string;
  status: string;
  message?: string;
  triggered_by?: string;
  /** Optional short summary the runner emits (e.g. "Notified 3 users"). */
  output_summary?: string;
  /** Optional per-step trace lines for the "what happened" detail panel. */
  events?: string[];
  /** Optional detailed error block (multi-line, monospace-friendly). */
  error?: string;
  /** Target type the job was pointed at (workflow / skill / agent / …). */
  target_type?: string;
  /** Target id (workflow key, agent slug, task id, etc.). */
  target_id?: string;
}

/**
 * Fetch the persisted run history for a single scheduler job. Used by the
 * Routine detail drawer to show "previous runs and their outcomes".
 */
export async function getSchedulerRuns(slug: string): Promise<SchedulerRun[]> {
  const res = await get<{ runs: SchedulerRun[] }>(
    `/scheduler/${encodeURIComponent(slug)}/runs`,
  );
  return res.runs ?? [];
}

/** Lifecycle event on a routine (created, edited, paused, restored, …). */
export interface SchedulerActivity {
  at: string;
  kind: string;
  actor?: string;
  summary: string;
  detail?: string;
}

export async function getSchedulerActivity(
  slug: string,
): Promise<SchedulerActivity[]> {
  const res = await get<{ events: SchedulerActivity[] }>(
    `/scheduler/${encodeURIComponent(slug)}/activity`,
  );
  return res.events ?? [];
}

/** A snapshot of a routine taken at save time. */
export interface SchedulerRevision {
  version: number;
  created_at: string;
  author?: string;
  change_note?: string;
  label: string;
  schedule_expr?: string;
  interval_minutes?: number;
  interval_override?: number;
  target_type?: string;
  target_id?: string;
  payload?: string;
  enabled: boolean;
  channel?: string;
  kind?: string;
}

export async function getSchedulerRevisions(
  slug: string,
): Promise<SchedulerRevision[]> {
  const res = await get<{ revisions: SchedulerRevision[] }>(
    `/scheduler/${encodeURIComponent(slug)}/revisions`,
  );
  return res.revisions ?? [];
}

export async function restoreSchedulerRevision(
  slug: string,
  version: number,
): Promise<{ restored: boolean; current_revision: number }> {
  return post<{ restored: boolean; current_revision: number }>(
    `/scheduler/${encodeURIComponent(slug)}/revisions/${version}/restore`,
  );
}

/** Wire shape for POST /scheduler — creates a routine from scratch. */
export interface CreateSchedulerJobBody {
  slug?: string;
  label: string;
  kind?: string;
  target_type?: string;
  target_id?: string;
  channel?: string;
  schedule_expr?: string;
  interval_minutes?: number;
  payload?: string;
  enabled?: boolean;
}

export async function createSchedulerJob(
  body: CreateSchedulerJobBody,
): Promise<{ job: SchedulerJob }> {
  return trackOn(
    post<{ job: SchedulerJob }>("/scheduler", body),
    "routine_created",
    {
      schedule_type: body.schedule_expr ? "cron" : "interval",
    },
  );
}
