/**
 * Typed WuphfAPI client.
 * Mirrors every method from the legacy IIFE in index.legacy.html.
 */

const apiBase = "/api";
let brokerDirect = "http://localhost:7890";
let useProxy = true;
let token: string | null = null;
const brokerHandshakeTimeoutMs = 8000;

// ── Init ──

export async function initApi(): Promise<void> {
  try {
    const r = await fetch("/api-token");
    const data = await r.json();
    const { token: nextToken, broker_url: brokerUrl } = data;
    token = nextToken;
    if (brokerUrl) {
      brokerDirect = String(brokerUrl).replace(/\/+$/, "");
    }
    useProxy = true;
  } catch {
    useProxy = false;
    try {
      const r = await fetch(`${brokerDirect}/web-token`);
      const data = await r.json();
      const { token: nextToken } = data;
      token = nextToken;
    } catch {
      // broker unreachable — will fail on first request
    }
  }
}

export async function connectBroker(
  brokerUrl: string,
  brokerToken?: string,
): Promise<void> {
  const nextBroker = brokerUrl.trim().replace(/\/+$/, "");
  if (!nextBroker) {
    throw new Error("Broker URL is required");
  }
  try {
    const parsed = new URL(nextBroker);
    if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
      throw new Error("unsupported protocol");
    }
  } catch {
    throw new Error("Broker URL must be a valid http:// or https:// URL");
  }
  let nextToken = brokerToken?.trim() || null;
  const r = await fetchWithTimeout(
    `${nextBroker}/health`,
    brokerHandshakeTimeoutMs,
  );
  if (!r.ok) {
    const text = (await r.text().catch(() => "")).trim();
    throw new Error(text || `${r.status} ${r.statusText}`);
  }
  if (!nextToken) {
    const tokenResp = await fetchWithTimeout(
      `${nextBroker}/web-token`,
      brokerHandshakeTimeoutMs,
    );
    if (!tokenResp.ok) {
      const text = (await tokenResp.text().catch(() => "")).trim();
      throw new Error(text || `${tokenResp.status} ${tokenResp.statusText}`);
    }
    const data = await tokenResp.json();
    const candidate = typeof data?.token === "string" ? data.token.trim() : "";
    if (!candidate) {
      throw new Error("Broker /web-token response did not include a token");
    }
    nextToken = candidate;
  }
  brokerDirect = nextBroker;
  token = nextToken;
  useProxy = false;
}

async function fetchWithTimeout(
  url: string,
  timeoutMs: number,
): Promise<Response> {
  const controller = new AbortController();
  const timeout = globalThis.setTimeout(() => controller.abort(), timeoutMs);
  try {
    return await fetch(url, { signal: controller.signal });
  } catch (err) {
    if (err instanceof Error && err.name === "AbortError") {
      throw new Error(`Timed out connecting to broker after ${timeoutMs}ms`);
    }
    throw err;
  } finally {
    globalThis.clearTimeout(timeout);
  }
}

// ── Internal helpers ──

function baseURL(): string {
  return useProxy ? apiBase : brokerDirect;
}

function authHeaders(): Record<string, string> {
  const h: Record<string, string> = { "Content-Type": "application/json" };
  if (token) h.Authorization = `Bearer ${token}`;
  return h;
}

interface RequestOptions {
  signal?: AbortSignal;
}

export class ApiError extends Error {
  readonly status: number;
  readonly statusText: string;
  readonly bodyText: string;
  readonly errorCode: string | null;
  readonly retryAfter: string | null;

  constructor(args: {
    readonly status: number;
    readonly statusText: string;
    readonly bodyText: string;
    readonly errorCode?: string | null;
    readonly retryAfter?: string | null;
  }) {
    super(args.bodyText || `${args.status} ${args.statusText}`);
    this.name = "ApiError";
    this.status = args.status;
    this.statusText = args.statusText;
    this.bodyText = args.bodyText;
    this.errorCode = args.errorCode ?? null;
    this.retryAfter = args.retryAfter ?? null;
  }
}

export async function get<T = unknown>(
  path: string,
  params?: Record<string, string | number | boolean | null | undefined>,
): Promise<T> {
  let url = baseURL() + path;
  if (params) {
    const qs = Object.entries(params)
      .filter(([, v]) => v !== null)
      .map(
        ([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(String(v))}`,
      )
      .join("&");
    if (qs) url += `?${qs}`;
  }
  const r = await fetch(url, { headers: authHeaders() });
  if (!r.ok) {
    throw await apiErrorFromResponse(r);
  }
  return r.json();
}

export async function getText(
  path: string,
  params?: Record<string, string | number | boolean | null | undefined>,
): Promise<string> {
  let url = baseURL() + path;
  if (params) {
    const qs = Object.entries(params)
      .filter(([, v]) => v !== null)
      .map(
        ([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(String(v))}`,
      )
      .join("&");
    if (qs) url += `?${qs}`;
  }
  const r = await fetch(url, { headers: authHeaders() });
  if (!r.ok) {
    throw await apiErrorFromResponse(r);
  }
  return r.text();
}

export async function post<T = unknown>(
  path: string,
  body?: unknown,
  options: RequestOptions = {},
): Promise<T> {
  const r = await fetch(baseURL() + path, {
    method: "POST",
    headers: authHeaders(),
    body: JSON.stringify(body),
    signal: options.signal,
  });
  if (!r.ok) {
    throw await apiErrorFromResponse(r);
  }
  return r.json();
}

export async function put<T = unknown>(
  path: string,
  body?: unknown,
): Promise<T> {
  const r = await fetch(baseURL() + path, {
    method: "PUT",
    headers: authHeaders(),
    body: JSON.stringify(body),
  });
  if (!r.ok) {
    throw await apiErrorFromResponse(r);
  }
  return r.json();
}

export async function postWithTimeout<T = unknown>(
  path: string,
  body: unknown,
  timeoutMs: number,
): Promise<T> {
  const controller = new AbortController();
  const timeout = globalThis.setTimeout(() => controller.abort(), timeoutMs);
  try {
    const r = await fetch(baseURL() + path, {
      method: "POST",
      headers: authHeaders(),
      body: JSON.stringify(body),
      signal: controller.signal,
    });
    if (!r.ok) {
      throw await apiErrorFromResponse(r);
    }
    return r.json();
  } catch (err) {
    if (err instanceof Error && err.name === "AbortError") {
      throw new Error("Request timed out");
    }
    throw err;
  } finally {
    globalThis.clearTimeout(timeout);
  }
}

export async function patch<T = unknown>(
  path: string,
  body?: unknown,
): Promise<T> {
  const r = await fetch(baseURL() + path, {
    method: "PATCH",
    headers: authHeaders(),
    body: JSON.stringify(body),
  });
  if (!r.ok) {
    throw await apiErrorFromResponse(r);
  }
  return r.json();
}

export async function del<T = unknown>(
  path: string,
  body?: unknown,
): Promise<T> {
  const r = await fetch(baseURL() + path, {
    method: "DELETE",
    headers: authHeaders(),
    body: JSON.stringify(body),
  });
  if (!r.ok) {
    throw await apiErrorFromResponse(r);
  }
  return r.json();
}

async function apiErrorFromResponse(response: Response): Promise<ApiError> {
  const bodyText = (await response.text().catch(() => "")).trim();
  return new ApiError({
    status: response.status,
    statusText: response.statusText,
    bodyText,
    errorCode: errorCodeFromBodyText(bodyText),
    retryAfter: response.headers.get("Retry-After"),
  });
}

function errorCodeFromBodyText(bodyText: string): string | null {
  if (bodyText.length === 0) return null;
  try {
    const parsed = JSON.parse(bodyText) as unknown;
    if (
      typeof parsed !== "object" ||
      parsed === null ||
      Array.isArray(parsed)
    ) {
      return null;
    }
    const { error } = parsed as Readonly<Record<string, unknown>>;
    return typeof error === "string" ? error : null;
  } catch {
    return null;
  }
}

// ── SSE ──

export function sseURL(path: string): string {
  let url = baseURL() + path;
  if (!useProxy && token) url += `?token=${encodeURIComponent(token)}`;
  return url;
}

export function websocketURL(path: string): string {
  const base =
    useProxy && brokerDirect
      ? brokerDirect
      : typeof window === "undefined"
        ? baseURL()
        : new URL(baseURL(), window.location.href)
            .toString()
            .replace(/\/$/, "");
  const url = new URL(path, base.endsWith("/") ? base : `${base}/`);
  url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
  if (token) url.searchParams.set("token", token);
  return url.toString();
}

// ── Messages ──

export interface Message {
  id: string;
  from: string;
  channel: string;
  content: string;
  /**
   * Server-assigned message kind. Empty/absent for plain chat. Known kinds:
   *  - "agent_issue"        legacy agent-authored issue banner
   *  - "system_auth_error"  system-authored provider-auth failure card (#933)
   *  - "issue_draft_section" CEO-authored issue draft section
   *  - "ceo_*"              onboarding cards (form_field, chip_row, etc.)
   * The SPA's MessageBubble dispatches on this field to pick a renderer.
   */
  kind?: string;
  /**
   * Structured card payload for kinds that carry one. The broker marshals
   * this from a Go json.RawMessage so consumers receive an inline JSON
   * object (or array) — not a string. Consumers must treat every string
   * field inside as plain text (defense in depth on top of the broker-side
   * sanitizeContextValue).
   */
  payload?: unknown;
  redacted?: boolean;
  redaction_count?: number;
  redaction_reasons?: string[];
  timestamp: string;
  reply_to?: string;
  thread_id?: string;
  thread_count?: number;
  reactions?: Record<string, string[]>;
  tagged?: string[];
  usage?: TokenUsage;
}

export interface TokenUsage {
  input_tokens?: number;
  output_tokens?: number;
  cache_read_tokens?: number;
  cache_creation_tokens?: number;
  total_tokens?: number;
  cost_usd?: number;
}

export function getMessages(
  channel: string,
  sinceId?: string | null,
  limit = 50,
) {
  return get<{ messages: Message[] }>("/messages", {
    channel: channel || "general",
    viewer_slug: "human",
    since_id: sinceId ?? null,
    limit,
  });
}

export async function searchMessages(
  query: string,
  limit = 8,
  signal?: AbortSignal,
): Promise<Message[]> {
  const trimmed = query.trim();
  if (!trimmed) return [];
  const url =
    baseURL() +
    `/messages/search?q=${encodeURIComponent(trimmed)}&limit=${limit}&viewer_slug=human`;
  try {
    const r = await fetch(url, { headers: authHeaders(), signal });
    if (!r.ok) return [];
    const res = await r.json();
    return Array.isArray(res?.messages) ? res.messages : [];
  } catch {
    return [];
  }
}

export function postMessage(
  content: string,
  channel: string,
  replyTo?: string,
  tagged?: string[],
) {
  const body: Record<string, string | string[]> = {
    from: "you",
    channel: channel || "general",
    content,
  };
  if (replyTo) body.reply_to = replyTo;
  if (tagged && tagged.length > 0) body.tagged = tagged;
  return post<Message>("/messages", body);
}

export function getThreadMessages(channel: string, threadId: string) {
  return get<{ messages: Message[] }>("/messages", {
    channel: channel || "general",
    thread_id: threadId,
    viewer_slug: "human",
    limit: 50,
  });
}

export function toggleReaction(msgId: string, emoji: string, channel: string) {
  return post("/messages/react", {
    message_id: msgId,
    emoji,
    channel: channel || "general",
  });
}

// ── Slash-command registry ──

/**
 * One entry from GET /commands. Mirrors the broker's `commandDescriptor`
 * shape in internal/team/broker_commands.go. Sorted alphabetically by the
 * broker — callers do not need to re-sort.
 */
export interface SlashCommandDescriptor {
  name: string;
  description: string;
  /** True when the web composer has a real handler for this command. */
  webSupported: boolean;
}

/**
 * Fetch the canonical slash-command registry from the broker. The web
 * autocomplete filters to webSupported=true; other callers may want the
 * full set for discovery.
 */
export function fetchCommands() {
  return get<SlashCommandDescriptor[]>("/commands");
}

// ── Members ──

export interface ProviderBinding {
  // kind tags the runtime or gateway for this agent. Empty string means
  // "inherit from global default". Use IsGatewayKind on a Kind to decide
  // whether to render the runtime picker (LLM kinds) or a "Managed by
  // <Gateway>" badge (gateway kinds) in the agent profile.
  kind?: LLMProvider | "";
  // model is the runtime-specific model identifier. Free-form on the wire —
  // validated by each provider implementation, not at the schema layer.
  // Common shapes: "claude-3-5-sonnet-latest", "gpt-4o", "llama3.1:8b".
  model?: string;
  // openclaw is populated only when kind === "openclaw" — it carries the
  // gateway-side session key + agent id. Set by the OpenClaw bridge bootstrap
  // path, not by the per-agent runtime picker.
  openclaw?: {
    session_key?: string;
    agent_id?: string;
  };
}

// Helper for UI code: returns true when binding.kind is a gateway-controlled
// tag. Per-agent runtime pickers and the AgentWizard should swap their UI to
// a read-only "Managed by <Gateway>" pill when this returns true.
export function isGatewayBinding(
  binding: ProviderBinding | string | undefined,
): boolean {
  if (!binding) return false;
  const kind = typeof binding === "string" ? binding : binding.kind;
  return (
    kind === "openclaw" || kind === "openclaw-http" || kind === "hermes-agent"
  );
}

export interface OfficeMember {
  slug: string;
  name: string;
  role: string;
  emoji?: string;
  status?: string;
  activity?: string;
  detail?: string;
  liveActivity?: string;
  lastTime?: string;
  task?: string;
  channel?: string;
  provider?: ProviderBinding | string;
  /** Broker-provided: serialized as `built_in`. Built-ins cannot be removed. (CEO is guarded by a separate slug check.) */
  built_in?: boolean;
  /** Per-channel disabled state when the list is sourced from `/members?channel=…`. */
  disabled?: boolean;
  /**
   * Transport-presence flag: true when an adapter session is currently live for
   * this member. Distinct from `status`/`activity` (which reflect "is the
   * agent processing right now") — `online` reflects "is the adapter
   * reachable at all". Always present (no omitempty on the Go side) so
   * "false" and "missing field" cannot be confused.
   */
  online?: boolean;
  /**
   * RFC3339 timestamp of the most recent UpsertParticipant for this slug.
   * Empty when no adapter has ever upserted (e.g. built-in members without an
   * openclaw provider) — the consumer should treat empty as "never observed"
   * and not render a "last seen" line.
   */
  last_seen_at?: string;
}

/**
 * Lane A piggybacks `humanHasPosted` onto the existing `/office-members`
 * payload (eng decision A5/P1) — additive `meta` field. When the backend
 * has not yet shipped Lane A, `meta` is absent and consumers default
 * `humanHasPosted` to `false` to avoid flashing the first-run nudge.
 */
export interface OfficeMembersMeta {
  humanHasPosted?: boolean;
}

export interface OfficeMembersResponse {
  members: OfficeMember[];
  meta?: OfficeMembersMeta;
}

export function getOfficeMembers() {
  return get<OfficeMembersResponse>("/office-members");
}

export interface GeneratedAgentTemplate {
  slug?: string;
  name?: string;
  role?: string;
  emoji?: string;
  expertise?: string[];
  personality?: string;
  provider?: string;
  model?: string;
}

export function generateAgent(prompt: string) {
  return post<GeneratedAgentTemplate>("/office-members/generate", { prompt });
}

export function getMembers(channel: string) {
  return get<{ members: OfficeMember[] }>("/members", {
    channel: channel || "general",
    viewer_slug: "human",
  });
}

// ── Channels ──

export interface Channel {
  slug: string;
  name: string;
  description?: string;
  type?: string;
  created_by?: string;
  members?: string[];
}

export interface DMChannelResponse extends Channel {
  id?: string;
  created?: boolean;
}

export function getChannels() {
  return get<{ channels: Channel[] }>("/channels");
}

export function createChannel(slug: string, name: string, description: string) {
  return post("/channels", {
    action: "create",
    slug,
    name: name || slug,
    description,
    created_by: "you",
  });
}

export function generateChannel(prompt: string) {
  return postWithTimeout<Channel>("/channels/generate", { prompt }, 65_000);
}

export function createDM(agentSlug: string) {
  return post<DMChannelResponse>("/channels/dm", {
    members: ["human", agentSlug],
    type: "direct",
  });
}

// ── Requests ──

export interface InterviewOption {
  id: string;
  label: string;
  description?: string;
  requires_text?: boolean;
  text_hint?: string;
}

export interface SkillSimilarRef {
  slug: string;
  score: number;
  method?: string;
}

export interface InterviewMetadata {
  /** Set on enhance_skill_proposal interviews (PR 7 task #15). */
  enhances_slug?: string;
  /** Set on ambiguous-band skill_proposal interviews (PR 7 task #15). */
  similar_to_existing?: SkillSimilarRef;
  [key: string]: unknown;
}

export interface AgentRequest {
  id: string;
  from: string;
  question: string;
  /** Legacy field name; broker now returns `options`. Kept for compatibility. */
  choices?: InterviewOption[];
  options?: InterviewOption[];
  channel?: string;
  title?: string;
  context?: string;
  kind?: string;
  timestamp?: string;
  status?: string;
  blocking?: boolean;
  required?: boolean;
  recommended_id?: string;
  created_at?: string;
  updated_at?: string;
  /** Echoes the entity slug the request is about (e.g. a skill name). */
  reply_to?: string;
  /** Structured metadata. Used by the enhance-existing UX (PR 7 task #14). */
  metadata?: InterviewMetadata;
  /** Full candidate spec on enhance_skill_proposal interviews. */
  enhance_candidate?: Skill;
  redacted?: boolean;
  redaction_count?: number;
  redaction_reasons?: string[];
  /** Issue/task id this request belongs to, when the owner agent
   * filed the request from inside an owned Issue. The Inbox card
   * renders a breadcrumb when set so the human sees the parent
   * Issue at a glance. */
  issue_id?: string;
}

export function getRequests(channel: string) {
  return get<{ requests: AgentRequest[] }>("/requests", {
    channel: channel || "general",
    viewer_slug: "human",
  });
}

// Cross-channel view. The broker's blocking check is global, so the web UI's
// global overlay + inline interview bar need every blocking request the human
// can answer, not just the ones in the current channel.
export function getAllRequests() {
  return get<{ requests: AgentRequest[] }>("/requests", {
    scope: "all",
    viewer_slug: "human",
  });
}

export function answerRequest(
  id: string,
  choiceId: string,
  customText?: string,
) {
  const body: Record<string, string> = { id, choice_id: choiceId };
  if (customText) body.custom_text = customText;
  return post("/requests/answer", body);
}

export function cancelRequest(id: string) {
  return post("/requests", { action: "cancel", id });
}

// ── Signals / Decisions / Watchdogs / Actions ──

export function getSignals() {
  return get("/signals");
}
export function getDecisions() {
  return get("/decisions");
}
export function getWatchdogs() {
  return get("/watchdogs");
}
export function getActions() {
  return get("/actions");
}

// ── Policies ──

export interface Policy {
  id: string;
  source: string;
  rule: string;
  active?: boolean;
}

export function getPolicies() {
  return get<{ policies: Policy[] }>("/policies");
}

export function createPolicy(source: string, rule: string) {
  return post("/policies", { source, rule });
}

export function deletePolicy(id: string) {
  return del("/policies", { id });
}

// ── Scheduler / Routines ──
// Moved to ./scheduler.ts; re-exported here for back-compat with existing
// imports from "./api/client" or "../api/client".
export type {
  CreateSchedulerJobBody,
  PatchSchedulerJobBody,
  PatchSchedulerJobResponse,
  SchedulerActivity,
  SchedulerJob,
  SchedulerRevision,
  SchedulerRun,
  SystemCronSpec,
} from "./scheduler";
export {
  createSchedulerJob,
  getScheduler,
  getSchedulerActivity,
  getSchedulerRevisions,
  getSchedulerRuns,
  getSystemCronSpecs,
  patchSchedulerJob,
  restoreSchedulerRevision,
  runSchedulerJob,
} from "./scheduler";

// ── Skills ──

export type SkillStatus = "active" | "proposed" | "archived" | "disabled";

export interface SkillMetadata {
  wuphf?: {
    source_articles?: string[];
  };
}

export type OwnerAgents = string[];

export interface Skill {
  name: string;
  title?: string;
  description?: string;
  source?: string;
  content?: string;
  trigger?: string;
  parameters?: unknown;
  status?: SkillStatus;
  created_by?: string;
  created_at?: string;
  updated_at?: string;
  /** Per-agent scoping (PR 7). Empty/missing = lead-routable shared skill. */
  owner_agents?: OwnerAgents;
  /** Set on ambiguous-band proposals by the similarity gate (PR 7 task #15). */
  similar_to_existing?: SkillSimilarRef;
  metadata?: SkillMetadata;
}

export type SkillsListScope = "active" | "all";

export function getSkills() {
  return get<{ skills: Skill[] }>("/skills");
}

/**
 * Fetch the skill catalog. With scope="all" the legacy /skills endpoint
 * accepts include_archived + include_disabled flags (PR 7 task #18) so the
 * Skills app can render every section (Pending / Active / Disabled /
 * Archived) from a single query — keeping body content intact for the
 * SidePanel preview and the enhance-existing patchSkill flow.
 *
 * scope="active" returns the legacy default (active + proposed + disabled,
 * archived hidden) for callers that don't need the archived bucket.
 */
export function getSkillsList(scope: SkillsListScope = "all") {
  const params: Record<string, string> = {};
  if (scope === "all") {
    params.include_archived = "true";
    params.include_disabled = "true";
  }
  return get<{ skills: Skill[] }>("/skills", params);
}

export interface DisableSkillResponse {
  skill?: Skill;
}

export function disableSkill(name: string): Promise<DisableSkillResponse> {
  return post<DisableSkillResponse>(
    `/skills/${encodeURIComponent(name)}/disable`,
    {},
  );
}

export interface EnableSkillResponse {
  skill?: Skill;
}

export function enableSkill(name: string): Promise<EnableSkillResponse> {
  return post<EnableSkillResponse>(
    `/skills/${encodeURIComponent(name)}/enable`,
    {},
  );
}

export interface SkillOwnerToggleResponse {
  skill?: Skill;
}

/**
 * Enable a specific skill for a specific agent. Adds the agent slug to
 * the skill's owner_agents list (idempotent). Only OwnerAgents members
 * can invoke a skill via team_skill_run — see the AVAILABLE SKILLS /
 * DISCOVERABLE SKILLS split in the agent prompt.
 */
export function enableSkillForAgent(
  name: string,
  agent: string,
): Promise<SkillOwnerToggleResponse> {
  return post<SkillOwnerToggleResponse>(
    `/skills/${encodeURIComponent(name)}/enable-for`,
    { agent },
  );
}

/** Remove an agent from the skill's owner_agents list (idempotent). */
export function disableSkillForAgent(
  name: string,
  agent: string,
): Promise<SkillOwnerToggleResponse> {
  return post<SkillOwnerToggleResponse>(
    `/skills/${encodeURIComponent(name)}/disable-for`,
    { agent },
  );
}

export interface RestoreArchivedSkillResponse {
  skill?: Skill;
}

export function restoreArchivedSkill(
  name: string,
): Promise<RestoreArchivedSkillResponse> {
  return post<RestoreArchivedSkillResponse>(
    `/skills/${encodeURIComponent(name)}/restore`,
    {},
  );
}

export interface ArchiveSkillResponse {
  ok?: boolean;
  skill?: Skill;
}

export function archiveSkill(name: string): Promise<ArchiveSkillResponse> {
  return post<ArchiveSkillResponse>(
    `/skills/${encodeURIComponent(name)}/archive`,
    {},
  );
}

export interface InvokeSkillResult {
  channel?: string;
  skill?: Skill;
  task_id?: string;
}

export function invokeSkill(
  name: string,
  params?: Record<string, unknown>,
): Promise<InvokeSkillResult> {
  return post<InvokeSkillResult>(
    `/skills/${encodeURIComponent(name)}/invoke`,
    params ?? {},
  );
}

// ── Skill compile (PR 1a wiki-skill-compile) ──

export interface CompileError {
  slug: string;
  reason: string;
}

export interface CompileResult {
  scanned: number;
  matched: number;
  proposed: number;
  deduped: number;
  rejected_by_guard: number;
  errors: CompileError[];
  duration_ms: number;
  trigger: string;
}

export interface CompileQueued {
  queued: true;
}

export interface CompileSkipped {
  skipped: string;
}

export type CompileResponse = CompileResult | CompileQueued | CompileSkipped;

export function compileSkills(opts?: {
  dry_run?: boolean;
  scope_path?: string;
}) {
  return post<CompileResponse>("/skills/compile", opts ?? {});
}

export interface SkillCompileStats {
  last_run_at?: string;
  total_runs?: number;
  total_proposed?: number;
  total_deduped?: number;
  total_rejected_by_guard?: number;
  [key: string]: unknown;
}

export function getSkillCompileStats() {
  return get<SkillCompileStats>("/skills/compile/stats");
}

export interface ApproveSkillResponse {
  skill?: Skill;
}

export function approveSkill(name: string): Promise<ApproveSkillResponse> {
  return post<ApproveSkillResponse>(
    `/skills/${encodeURIComponent(name)}/approve`,
    {},
  );
}

export interface RejectSkillResponse {
  ok: boolean;
  undo_token: string;
  skill_name: string;
  expires_in: number;
}

export function rejectSkill(
  name: string,
  reason?: string,
): Promise<RejectSkillResponse> {
  return post<RejectSkillResponse>(
    `/skills/${encodeURIComponent(name)}/reject`,
    reason ? { reason } : {},
  );
}

export interface UndoRejectSkillResponse {
  skill?: Skill;
}

export function undoRejectSkill(
  undoToken: string,
): Promise<UndoRejectSkillResponse> {
  return post<UndoRejectSkillResponse>(`/skills/reject/undo`, {
    undo_token: undoToken,
  });
}

export interface PatchSkillRequest {
  old_string: string;
  new_string: string;
  replace_all?: boolean;
}

export interface PatchSkillResponse {
  skill?: Skill;
}

/**
 * Edit-tool style find/replace patch against a skill's body.
 * Used by the enhance-existing flow (PR 7 task #14) to fold a candidate
 * proposal into an existing skill without losing provenance.
 */
export function patchSkill(
  name: string,
  body: PatchSkillRequest,
): Promise<PatchSkillResponse> {
  return post<PatchSkillResponse>(
    `/skills/${encodeURIComponent(name)}/patch`,
    body,
  );
}

export interface EditSkillContentResponse {
  skill?: Skill;
}

/**
 * Full SKILL.md body replacement. Caller passes the entire rendered
 * SKILL.md (frontmatter + body). The broker re-parses, re-runs the
 * safety scan with the original creator's trust level, and rewrites the
 * wiki article. Used by the full-screen skill detail editor.
 */
export function editSkillContent(
  name: string,
  content: string,
): Promise<EditSkillContentResponse> {
  return put<EditSkillContentResponse>(`/skills/${encodeURIComponent(name)}`, {
    content,
  });
}

// ── Memory ──

export function getMemory(channel: string) {
  return get("/memory", { channel: channel || "general" });
}

export function setMemory(namespace: string, key: string, value: string) {
  return post("/memory", { namespace, key, value });
}

// ── Config (Settings) ──

// LLMRuntimeKind names a directly-dispatchable LLM runtime — the kinds that
// belong in any runtime picker (Settings default-runtime, AgentProfilePanel
// Runtime section, AgentWizard provider field). Mirrors the non-gateway
// subset returned by provider.LLMProviderKinds in the Go layer.
export type LLMRuntimeKind =
  | "claude-code"
  | "ollama"
  | "codex"
  | "opencode"
  | "mlx-lm"
  | "exo";

// GatewayKind names a runtime that is reached through an integration gateway
// rather than dispatched directly. Gateway-bound agents are imported via the
// Integrations app (OpenClaw / Hermes) and never appear in runtime pickers;
// they receive a "Managed by <Gateway>" badge on the agent profile.
export type GatewayKind = "openclaw" | "openclaw-http" | "hermes-agent";

// LLMProvider is the union of both — used wherever a value carries either an
// LLM runtime or a gateway tag (per-agent ProviderBinding.Kind on the wire,
// ConfigSnapshot.llm_provider for backward compatibility). New UI code should
// prefer LLMRuntimeKind / GatewayKind and only widen to LLMProvider at the
// raw-wire boundary.
export type LLMProvider = LLMRuntimeKind | GatewayKind;
export type MemoryBackend = "markdown" | "nex" | "gbrain" | "none";
export type ActionProvider = "auto" | "one" | "composio" | "";

export interface ProviderEndpoint {
  base_url?: string;
  model?: string;
}

// LocalProviderStatus mirrors internal/team/local_providers_status.go.
// One entry per registered local OpenAI-compatible kind. Test
// `TestComputeLocalProviderStatuses_DocumentedSurface` keeps the JSON
// field names in lockstep with this type.
export interface LocalProviderStatus {
  kind: string;
  binary_installed: boolean;
  binary_path?: string;
  binary_version?: string;
  endpoint: string;
  model: string;
  reachable: boolean;
  loaded_model?: string;
  probed: boolean;
  probe_skipped_note?: string;
  platform_supported: boolean;
  windows_note?: string;
  install?: Record<string, string>;
  start?: Record<string, string>;
  notes?: string[];
}

export interface ConfigSnapshot {
  // Runtime
  llm_provider?: LLMProvider;
  llm_provider_configured?: boolean;
  llm_provider_priority?: string[];
  // llm_provider_kinds is the non-gateway subset of registered runtimes —
  // the safe list to render in any runtime picker. Read this off the wire
  // instead of hardcoding the union so a future provider registered on the
  // Go side appears in the UI without a frontend change.
  llm_provider_kinds?: LLMRuntimeKind[];
  // gateway_kinds is the inverse — the registered gateway runtimes. The
  // Integrations app enumerates these to know which gateway cards (OpenClaw,
  // Hermes) are compiled in and connectable.
  gateway_kinds?: GatewayKind[];
  provider_endpoints?: Record<string, ProviderEndpoint>;
  memory_backend?: MemoryBackend;
  action_provider?: ActionProvider;
  team_lead_slug?: string;
  max_concurrent_agents?: number;
  default_format?: string;
  default_timeout?: number;
  blueprint?: string;
  // Workspace
  email?: string;
  workspace_id?: string;
  workspace_slug?: string;
  dev_url?: string;
  // Company
  company_name?: string;
  company_description?: string;
  company_goals?: string;
  company_size?: string;
  company_priority?: string;
  // Polling
  insights_poll_minutes?: number;
  task_follow_up_minutes?: number;
  task_reminder_minutes?: number;
  task_recheck_minutes?: number;
  // Secret flags
  api_key_set?: boolean;
  openai_key_set?: boolean;
  anthropic_key_set?: boolean;
  gemini_key_set?: boolean;
  minimax_key_set?: boolean;
  one_key_set?: boolean;
  composio_key_set?: boolean;
  telegram_token_set?: boolean;
  openclaw_token_set?: boolean;
  openclaw_gateway_url?: string;
  config_path?: string;
}

export type ConfigUpdate = Partial<{
  llm_provider: LLMProvider;
  provider_endpoints: Record<string, ProviderEndpoint>;
  memory_backend: MemoryBackend;
  action_provider: ActionProvider;
  team_lead_slug: string;
  max_concurrent_agents: number;
  default_format: string;
  default_timeout: number;
  blueprint: string;
  email: string;
  dev_url: string;
  company_name: string;
  company_description: string;
  company_goals: string;
  company_size: string;
  company_priority: string;
  insights_poll_minutes: number;
  task_follow_up_minutes: number;
  task_reminder_minutes: number;
  task_recheck_minutes: number;
  // Secret-write fields — sent as plaintext on write, never returned on read
  api_key: string;
  openai_api_key: string;
  anthropic_api_key: string;
  gemini_api_key: string;
  minimax_api_key: string;
  one_api_key: string;
  composio_api_key: string;
  telegram_bot_token: string;
  openclaw_token: string;
  openclaw_gateway_url: string;
}>;

export function getConfig() {
  return get<ConfigSnapshot>("/config");
}

export function updateConfig(configPatch: ConfigUpdate) {
  return post<{ status: string }>("/config", configPatch);
}

// Doctor endpoint — one entry per registered local OpenAI-compatible
// runtime. Settings page polls this; Onboarding wizard reads it on
// mount. The broker probes loopback endpoints only (see
// internal/team/local_providers_status.go), so calling this never
// triggers outbound traffic.
export function getLocalProvidersStatus() {
  return get<LocalProviderStatus[]>("/status/local-providers");
}

// ── Image generation ──

export interface ImageProviderStatus {
  kind: string;
  label: string;
  blurb: string;
  reachable: boolean;
  configured: boolean;
  base_url?: string;
  default_model?: string;
  supported_models?: string[];
  supports_image: boolean;
  supports_video: boolean;
  needs_api_key: boolean;
  api_key_set: boolean;
  implementation_ok: boolean;
  setup_hint?: string;
}

export function getImageProviders() {
  return get<{ providers: ImageProviderStatus[] }>("/image-providers");
}

export function setImageProviderConfig(opts: {
  kind: string;
  api_key?: string;
  base_url?: string;
  model?: string;
}) {
  return put<ImageProviderStatus[]>("/image-providers", opts);
}

// ── Workspace wipes (Danger Zone) ──

// WorkspaceWipeResult shape mirrors internal/workspace.Result plus the flags
// the HTTP handler adds (restart_required, redirect). The UI just needs ok +
// a reason to reload, but we surface `removed` so users can see what went.
export interface WorkspaceWipeResult {
  ok: boolean;
  restart_required?: boolean;
  redirect?: string;
  removed?: string[];
  errors?: string[];
  error?: string;
}

// resetWorkspace is the narrow wipe: clears broker runtime state only.
// Team roster, company identity, tasks, and workflows all survive. Call
// window.location.reload() after success so the UI picks up the empty
// broker state.
export function resetWorkspace() {
  return postWithTimeout<WorkspaceWipeResult>("/workspace/reset", {}, 20_000);
}

// shredWorkspace is the full wipe: broker runtime + team + company + office,
// workflows, logs, sessions, provider state, and local markdown memory.
// The broker resets in place after success so onboarding can reopen immediately.
export function shredWorkspace() {
  return postWithTimeout<WorkspaceWipeResult>("/workspace/shred", {}, 20_000);
}

// restartBroker asks the host web UI server to restart the broker listener.
// In same-origin web mode, ServeWebUI handles /api/broker/restart before the
// generic proxy, so the action still works when the broker HTTP listener is
// unreachable. The browser's SSE EventSource reconnects automatically once the
// listener is ready; useBrokerEvents refreshes auth before marking connected.
export interface BrokerRestartStatus {
  ok: boolean;
  url?: string;
}

export function restartBroker() {
  return post<BrokerRestartStatus>("/broker/restart");
}

// ── Telegram /connect wizard ──
// These mirror the TUI's `/connect telegram` flow but drive it from the web.
// Pass an explicit `token` to override what the broker has on disk; pass an
// empty string to use the saved token from config / WUPHF_TELEGRAM_BOT_TOKEN.

export interface TelegramVerifyResponse {
  ok: boolean;
  bot_name?: string;
  error?: string;
}

export interface TelegramGroup {
  // chat_id comes from Go's int64 on the wire. Telegram's API docs say chat
  // IDs may have at most 52 significant bits — exactly inside JS's
  // Number.MAX_SAFE_INTEGER (53 bits). Today's supergroup IDs (~13 digits)
  // are well below that, so a plain `number` is safe. If Telegram ever
  // widens past 52 bits this needs to become a string (or bigint with an
  // explicit serialiser) to avoid silent precision loss on the round-trip.
  chat_id: number;
  title: string;
  type: string;
}

export interface TelegramDiscoverResponse {
  groups: TelegramGroup[];
}

export interface TelegramConnectResponse {
  channel_slug: string;
  group_title: string;
}

export function verifyTelegramBot(telegramToken: string, signal?: AbortSignal) {
  return post<TelegramVerifyResponse>(
    "/telegram/verify",
    { token: telegramToken },
    { signal },
  );
}

export function discoverTelegramChats(
  telegramToken: string,
  signal?: AbortSignal,
) {
  return post<TelegramDiscoverResponse>(
    "/telegram/discover",
    { token: telegramToken },
    { signal },
  );
}

export function connectTelegramChannel(
  opts: {
    token?: string;
    chat_id: number;
    title?: string;
    type?: string;
  },
  signal?: AbortSignal,
) {
  return post<TelegramConnectResponse>("/telegram/connect", opts, { signal });
}
