// agentStateClient — typed client for the agent service's persistence
// endpoints (tools, routines, sessions, artifacts), reached via the /agent
// vite proxy. House pattern (see ../tools/toolAgentClient.ts): every call
// carries an AbortSignal.timeout and throws on !ok; the try* wrappers resolve
// null on ANY failure so callers can fall back to the local seeded state and
// the FE keeps working offline. All POST/PATCH bodies carry schema_version.

const SCHEMA_VERSION = 1;
const READ_TIMEOUT_MS = 10_000;
const WRITE_TIMEOUT_MS = 30_000;
// Run-now drives a full chat turn through the agent — give it longer.
const RUN_TIMEOUT_MS = 60_000;

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

export interface WireRoutine {
  id: string;
  agent: string;
  name: string;
  prompt: string;
  schedule: string;
  enabled: boolean;
  version: number;
  draft?: boolean;
  lastRun?: string;
  sessionId: string;
}

export type WireSessionKind = "routine" | "manual";

export interface WireSession {
  id: string;
  agent: string;
  title: string;
  kind: WireSessionKind;
  at: string;
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

// ── Routines ─────────────────────────────────────────────────────────────────

export async function listRoutines(agent: string): Promise<WireRoutine[]> {
  const data = await agentFetch<{ routines: WireRoutine[] }>(
    `/routines?${q(agent)}`,
    { timeoutMs: READ_TIMEOUT_MS },
  );
  return Array.isArray(data.routines) ? data.routines : [];
}

export interface CreateRoutineInput {
  agent: string;
  name: string;
  prompt: string;
  schedule: string;
}

export async function createRoutine(
  input: CreateRoutineInput,
): Promise<WireRoutine> {
  const data = await agentFetch<{ routine: WireRoutine }>("/routines", {
    method: "POST",
    body: { ...input },
    timeoutMs: WRITE_TIMEOUT_MS,
  });
  return data.routine;
}

export interface PatchRoutineInput {
  agent: string;
  enabled?: boolean;
  prompt?: string;
  publish?: boolean;
}

export async function patchRoutine(
  id: string,
  input: PatchRoutineInput,
): Promise<WireRoutine> {
  const data = await agentFetch<{ routine: WireRoutine }>(
    `/routines/${encodeURIComponent(id)}`,
    { method: "PATCH", body: { ...input }, timeoutMs: WRITE_TIMEOUT_MS },
  );
  return data.routine;
}

export interface RunRoutineResult {
  routine: WireRoutine;
  session: WireSession;
}

export async function runRoutineNow(
  id: string,
  agent: string,
): Promise<RunRoutineResult> {
  return agentFetch<RunRoutineResult>(
    `/routines/${encodeURIComponent(id)}/run`,
    { method: "POST", body: { agent }, timeoutMs: RUN_TIMEOUT_MS },
  );
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

export function tryRunRoutineNow(
  id: string,
  agent: string,
): Promise<RunRoutineResult | null> {
  return orNull(runRoutineNow(id, agent));
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
