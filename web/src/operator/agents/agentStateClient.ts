// agentStateClient — typed client for the agent's persisted state, split
// across its two REAL homes:
//
//   - ROUTINES live in the BROKER's scheduler registry (cron, enable/disable,
//     revision history = versioning with change notes, per-slug run history)
//     — reached through the /api broker client. The registry is the previous
//     product avatar's proven engine; nothing routine-shaped is stored on the
//     agent service.
//   - TOOLS, SESSIONS (pi SessionManager JSONL), and ARTIFACTS live on the
//     agent service, reached via the /agent vite proxy.
//
// House pattern (see ../tools/toolAgentClient.ts): every agent-service call
// carries an AbortSignal.timeout and throws on !ok; the try* wrappers resolve
// null on ANY failure so callers can fall back to the local seeded state and
// the FE keeps working offline. All agent POST bodies carry schema_version.

import { get as brokerGet, patch as brokerPatch, post as brokerPost } from "../../api/client";

const SCHEMA_VERSION = 1;
const READ_TIMEOUT_MS = 10_000;
const WRITE_TIMEOUT_MS = 30_000;

// ── Wire shapes (mirror the agent service) ──────────────────────────────────

export interface WireToolInput {
  name: string;
  type?: string;
}

export interface WireTool {
  name: string;
  title: string;
  purpose: string;
  inputs: WireToolInput[];
  code: string;
  version: number;
}

/** FE view of a broker scheduler job that IS an operator routine. `id` is the
 * scheduler slug; `version` is the latest revision's version. */
export interface WireRoutine {
  id: string;
  agent: string;
  name: string;
  prompt: string;
  schedule: string;
  enabled: boolean;
  version: number;
  lastRun?: string;
  lastRunStatus?: string;
}

/** One entry of the routine's revision history (broker scheduler revisions). */
export interface WireRoutineRevision {
  version: number;
  created_at: string;
  author?: string;
  change_note?: string;
  label: string;
  schedule_expr?: string;
  payload?: string;
  enabled: boolean;
}

/** One entry of the routine's run history (broker per-slug run ring). */
export interface WireRoutineRun {
  slug: string;
  started_at: string;
  finished_at?: string;
  status: string;
  message?: string;
  triggered_by?: string;
  output_summary?: string;
  events?: string[];
  error?: string;
}

export type WireSessionKind = "routine" | "manual";

export interface WireSession {
  id: string;
  agent: string;
  title: string;
  kind: WireSessionKind;
  at: string;
  /** Broker scheduler slug of the owning routine (routine sessions only). */
  routine?: string;
}

export interface WireSessionMessage {
  from: "you" | "nex";
  body: string;
  at: string;
}

export interface WireArtifact {
  id: string;
  type: "md" | "html" | "pdf";
  title: string;
  producedBy: string;
  at: string;
  content?: string;
  url?: string;
  size?: string;
}

// ── Fetch plumbing ───────────────────────────────────────────────────────────

interface AgentFetchInit {
  method?: "GET" | "POST" | "PATCH";
  body?: Record<string, unknown>;
  timeoutMs: number;
}

async function agentFetch<T>(path: string, init: AgentFetchInit): Promise<T> {
  const res = await fetch(`/agent${path}`, {
    method: init.method ?? (init.body === undefined ? "GET" : "POST"),
    headers:
      init.body === undefined
        ? undefined
        : { "content-type": "application/json" },
    body:
      init.body === undefined
        ? undefined
        : JSON.stringify({ schema_version: SCHEMA_VERSION, ...init.body }),
    signal: AbortSignal.timeout(init.timeoutMs),
  });
  if (!res.ok) throw new Error(`agent ${res.status}`);
  return (await res.json()) as T;
}

function q(agent: string): string {
  return `agent=${encodeURIComponent(agent)}`;
}

// ── Tools ────────────────────────────────────────────────────────────────────

export async function listAgentTools(agent: string): Promise<WireTool[]> {
  const data = await agentFetch<{ tools: WireTool[] }>(`/tools?${q(agent)}`, {
    timeoutMs: READ_TIMEOUT_MS,
  });
  return Array.isArray(data.tools) ? data.tools : [];
}

// ── Routines (BROKER scheduler registry) ─────────────────────────────────────

/** Raw broker scheduler job — only the fields the routine view reads. */
interface BrokerSchedulerJob {
  slug: string;
  label: string;
  target_type?: string;
  target_id?: string;
  schedule_expr?: string;
  payload?: string;
  enabled: boolean;
  last_run?: string;
  last_run_status?: string;
  status?: string;
}

function jobToRoutine(job: BrokerSchedulerJob, version: number): WireRoutine {
  return {
    id: job.slug,
    agent: job.target_id ?? "",
    name: job.label,
    prompt: job.payload ?? "",
    schedule: job.schedule_expr ?? "",
    enabled: job.enabled,
    version,
    lastRun: job.last_run || undefined,
    lastRunStatus: job.last_run_status || undefined,
  };
}

/** Latest revision version per slug; 1 when no revision recorded yet. */
async function latestRevisionVersion(slug: string): Promise<number> {
  try {
    const data = await brokerGet<{ revisions: WireRoutineRevision[] }>(
      `/scheduler/${encodeURIComponent(slug)}/revisions`,
    );
    const versions = (data.revisions ?? []).map((r) => r.version);
    return versions.length ? Math.max(...versions) : 1;
  } catch {
    return 1;
  }
}

export async function listRoutines(agent: string): Promise<WireRoutine[]> {
  const data = await brokerGet<{ jobs: BrokerSchedulerJob[] }>("/scheduler");
  const jobs = (data.jobs ?? []).filter(
    (j) => j.target_type === "agent" && j.target_id === agent,
  );
  return Promise.all(
    jobs.map(async (j) => jobToRoutine(j, await latestRevisionVersion(j.slug))),
  );
}

export interface CreateRoutineInput {
  agent: string;
  name: string;
  prompt: string;
  /** Cron expression or broker shorthand (daily, hourly, 4h, "0 9 * * 1"). */
  schedule: string;
}

export async function createRoutine(
  input: CreateRoutineInput,
): Promise<WireRoutine> {
  const data = await brokerPost<{ job: BrokerSchedulerJob }>(
    "/scheduler/routines",
    {
      purpose: input.name,
      schedule: input.schedule,
      prompt: input.prompt,
      owner: input.agent,
      created_by: "operator",
    },
  );
  return jobToRoutine(data.job, 1);
}

export interface PatchRoutineInput {
  agent: string;
  enabled?: boolean;
  /** Publishing a new prompt version = a broker revision with a change note. */
  prompt?: string;
  changeNote?: string;
}

export async function patchRoutine(
  id: string,
  input: PatchRoutineInput,
): Promise<WireRoutine> {
  const body: Record<string, unknown> = {};
  if (input.enabled !== undefined) body.enabled = input.enabled;
  if (input.prompt !== undefined) {
    body.payload = input.prompt;
    body.change_note = input.changeNote || "Prompt updated from the Routines tab";
  }
  const data = await brokerPatch<{ job: BrokerSchedulerJob }>(
    `/scheduler/${encodeURIComponent(id)}`,
    body,
  );
  return jobToRoutine(data.job, await latestRevisionVersion(id));
}

/** Run NOW: the broker backdates next_run so the watchdog fires the routine
 * against the agent service within one tick (~20s). The outcome lands in the
 * run history + the routine's chat session — not in this response. */
export async function runRoutineNow(id: string): Promise<void> {
  await brokerPost(`/scheduler/${encodeURIComponent(id)}/run`, {});
}

export async function listRoutineRuns(id: string): Promise<WireRoutineRun[]> {
  const data = await brokerGet<{ runs: WireRoutineRun[] }>(
    `/scheduler/${encodeURIComponent(id)}/runs`,
  );
  return Array.isArray(data.runs) ? data.runs : [];
}

export async function listRoutineRevisions(
  id: string,
): Promise<WireRoutineRevision[]> {
  const data = await brokerGet<{ revisions: WireRoutineRevision[] }>(
    `/scheduler/${encodeURIComponent(id)}/revisions`,
  );
  return Array.isArray(data.revisions) ? data.revisions : [];
}

// ── Sessions ─────────────────────────────────────────────────────────────────

export async function listSessions(agent: string): Promise<WireSession[]> {
  const data = await agentFetch<{ sessions: WireSession[] }>(
    `/sessions?${q(agent)}`,
    { timeoutMs: READ_TIMEOUT_MS },
  );
  return Array.isArray(data.sessions) ? data.sessions : [];
}

export interface SessionDetail {
  session: WireSession;
  messages: WireSessionMessage[];
}

export async function getSession(
  id: string,
  agent: string,
): Promise<SessionDetail> {
  const data = await agentFetch<SessionDetail>(
    `/sessions/${encodeURIComponent(id)}?${q(agent)}`,
    { timeoutMs: READ_TIMEOUT_MS },
  );
  return {
    session: data.session,
    messages: Array.isArray(data.messages) ? data.messages : [],
  };
}

export async function createSession(
  agent: string,
  title?: string,
): Promise<WireSession> {
  const body: Record<string, unknown> = { agent };
  if (title) body.title = title;
  const data = await agentFetch<{ session: WireSession }>("/sessions", {
    method: "POST",
    body,
    timeoutMs: WRITE_TIMEOUT_MS,
  });
  return data.session;
}

export interface SessionMessageInput {
  agent: string;
  from: "you" | "nex";
  body: string;
}

export async function postSessionMessage(
  id: string,
  input: SessionMessageInput,
): Promise<void> {
  await agentFetch<{ ok: boolean }>(
    `/sessions/${encodeURIComponent(id)}/message`,
    { method: "POST", body: { ...input }, timeoutMs: WRITE_TIMEOUT_MS },
  );
}

// ── Artifacts ────────────────────────────────────────────────────────────────

export async function listArtifacts(agent: string): Promise<WireArtifact[]> {
  const data = await agentFetch<{ artifacts: WireArtifact[] }>(
    `/artifacts?${q(agent)}`,
    { timeoutMs: READ_TIMEOUT_MS },
  );
  return Array.isArray(data.artifacts) ? data.artifacts : [];
}

// ── try* wrappers: null on failure, so callers fall back to seeds ────────────

async function orNull<T>(work: Promise<T>): Promise<T | null> {
  try {
    return await work;
  } catch {
    return null;
  }
}

export function tryListAgentTools(agent: string): Promise<WireTool[] | null> {
  return orNull(listAgentTools(agent));
}

export function tryListRoutines(agent: string): Promise<WireRoutine[] | null> {
  return orNull(listRoutines(agent));
}

export function tryCreateRoutine(
  input: CreateRoutineInput,
): Promise<WireRoutine | null> {
  return orNull(createRoutine(input));
}

export function tryPatchRoutine(
  id: string,
  input: PatchRoutineInput,
): Promise<WireRoutine | null> {
  return orNull(patchRoutine(id, input));
}

/** True when the run-now was accepted (queued at the broker), null on failure. */
export function tryRunRoutineNow(id: string): Promise<true | null> {
  return orNull(runRoutineNow(id).then(() => true as const));
}

export function tryListRoutineRuns(
  id: string,
): Promise<WireRoutineRun[] | null> {
  return orNull(listRoutineRuns(id));
}

export function tryListRoutineRevisions(
  id: string,
): Promise<WireRoutineRevision[] | null> {
  return orNull(listRoutineRevisions(id));
}

export function tryListSessions(agent: string): Promise<WireSession[] | null> {
  return orNull(listSessions(agent));
}

export function tryGetSession(
  id: string,
  agent: string,
): Promise<SessionDetail | null> {
  return orNull(getSession(id, agent));
}

export function tryCreateSession(
  agent: string,
  title?: string,
): Promise<WireSession | null> {
  return orNull(createSession(agent, title));
}

/** Fire-and-forget mirror of a chat turn; a failure never surfaces. */
export function trySendSessionMessage(
  id: string,
  input: SessionMessageInput,
): void {
  void postSessionMessage(id, input).catch(() => {});
}

export function tryListArtifacts(
  agent: string,
): Promise<WireArtifact[] | null> {
  return orNull(listArtifacts(agent));
}
