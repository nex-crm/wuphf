/**
 * Typed WuphfAPI client.
 * Mirrors every method from the legacy IIFE in index.legacy.html.
 */

const apiBase = "/api";
let brokerDirect = "http://localhost:7890";
let useProxy = true;
let token: string | null = null;

// ── Init ──

export async function initApi(): Promise<void> {
  try {
    const r = await fetch("/api-token");
    const data = await r.json();
    token = data.token;
    if (data.broker_url) {
      brokerDirect = String(data.broker_url).replace(/\/+$/, "");
    }
    useProxy = true;
  } catch {
    useProxy = false;
    try {
      const r = await fetch(`${brokerDirect}/web-token`);
      const data = await r.json();
      token = data.token;
    } catch {
      // broker unreachable — will fail on first request
    }
  }
}

// ── Internal helpers ──

function baseURL(): string {
  return useProxy ? apiBase : brokerDirect;
}

function authHeaders(): Record<string, string> {
  const h: Record<string, string> = { "Content-Type": "application/json" };
  if (!useProxy && token) h.Authorization = `Bearer ${token}`;
  return h;
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
    const text = (await r.text().catch(() => "")).trim();
    throw new Error(text || `${r.status} ${r.statusText}`);
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
    const text = (await r.text().catch(() => "")).trim();
    throw new Error(text || `${r.status} ${r.statusText}`);
  }
  return r.text();
}

export async function post<T = unknown>(
  path: string,
  body?: unknown,
): Promise<T> {
  const r = await fetch(baseURL() + path, {
    method: "POST",
    headers: authHeaders(),
    body: JSON.stringify(body),
  });
  if (!r.ok) {
    const text = (await r.text().catch(() => "")).trim();
    throw new Error(text || `${r.status} ${r.statusText}`);
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
      const text = (await r.text().catch(() => "")).trim();
      throw new Error(text || `${r.status} ${r.statusText}`);
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
    const text = (await r.text().catch(() => "")).trim();
    throw new Error(text || `${r.status} ${r.statusText}`);
  }
  return r.json();
}

// ── SSE ──

export function sseURL(path: string): string {
  let url = baseURL() + path;
  if (!useProxy && token) url += `?token=${encodeURIComponent(token)}`;
  return url;
}

// ── Messages ──

export interface Message {
  id: string;
  from: string;
  channel: string;
  content: string;
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
  kind?: string;
  model?: string;
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
}

export function getOfficeMembers() {
  return get<{ members: OfficeMember[] }>("/office-members");
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
  return post<Channel>("/channels/generate", { prompt });
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

// ── Health ──

export function getHealth() {
  return get<{
    status: string;
    provider?: string;
    provider_model?: string;
    agents?: Record<string, unknown>;
  }>("/health");
}

// ── Tasks ──

export interface Task {
  id: string;
  title: string;
  description?: string;
  details?: string;
  status: string;
  owner?: string;
  created_by?: string;
  channel?: string;
  thread_id?: string;
  task_type?: string;
  pipeline_id?: string;
  pipeline_stage?: string;
  execution_mode?: string;
  review_state?: string;
  source_signal_id?: string;
  source_decision_id?: string;
  worktree_path?: string;
  worktree_branch?: string;
  depends_on?: string[];
  blocked?: boolean;
  acked_at?: string;
  due_at?: string;
  follow_up_at?: string;
  reminder_at?: string;
  recheck_at?: string;
  created_at?: string;
  updated_at?: string;
}

export function reassignTask(
  taskId: string,
  newOwner: string,
  channel: string,
  actor = "human",
) {
  return post<{ task: Task }>("/tasks", {
    action: "reassign",
    id: taskId,
    owner: newOwner,
    channel: channel || "general",
    created_by: actor,
  });
}

export type TaskStatusAction =
  | "release"
  | "review"
  | "block"
  | "complete"
  | "cancel";

export function updateTaskStatus(
  taskId: string,
  action: TaskStatusAction,
  channel: string,
  actor = "human",
) {
  return post<{ task: Task }>("/tasks", {
    action,
    id: taskId,
    channel: channel || "general",
    created_by: actor,
  });
}

export function getTasks(
  channel: string,
  opts?: { includeDone?: boolean; status?: string; mySlug?: string },
) {
  const params: Record<string, string> = {
    viewer_slug: "human",
    channel: channel || "general",
  };
  if (opts?.includeDone) params.include_done = "true";
  if (opts?.status) params.status = opts.status;
  if (opts?.mySlug) params.my_slug = opts.mySlug;
  return get<{ tasks: Task[] }>("/tasks", params);
}

export function getOfficeTasks(opts?: {
  includeDone?: boolean;
  status?: string;
  mySlug?: string;
}) {
  const params: Record<string, string> = {
    viewer_slug: "human",
    all_channels: "true",
  };
  if (opts?.includeDone) params.include_done = "true";
  if (opts?.status) params.status = opts.status;
  if (opts?.mySlug) params.my_slug = opts.mySlug;
  return get<{ tasks: Task[] }>("/tasks", params);
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

// ── Scheduler ──

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
}

export function getScheduler(opts?: { dueOnly?: boolean }) {
  const params: Record<string, string> = {};
  if (opts?.dueOnly) params.due_only = "true";
  return get<{ jobs: SchedulerJob[] }>("/scheduler", params);
}

// ── Skills ──

export interface Skill {
  name: string;
  description?: string;
  source?: string;
  parameters?: unknown;
}

export function getSkills() {
  return get<{ skills: Skill[] }>("/skills");
}

export function invokeSkill(name: string, params?: Record<string, unknown>) {
  return post(`/skills/${encodeURIComponent(name)}/invoke`, params ?? {});
}

// ── Usage ──

export interface AgentUsage {
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens: number;
  cost_usd: number;
}

export interface UsageData {
  total?: { cost_usd: number; total_tokens?: number };
  session?: { total_tokens: number };
  agents?: Record<string, AgentUsage>;
}

export function getUsage() {
  return get<UsageData>("/usage");
}

// ── Agent Logs ──

export interface AgentLog {
  id: string;
  agent: string;
  task?: string;
  action?: string;
  content?: string;
  timestamp?: string;
  usage?: TokenUsage;
}

export function getAgentLogs(opts?: { limit?: number; task?: string }) {
  if (opts?.task) {
    return get<{ logs: AgentLog[] }>("/agent-logs", { task: opts.task });
  }
  const params: Record<string, string> = {};
  if (opts?.limit) params.limit = String(opts.limit);
  return get<{ logs: AgentLog[] }>("/agent-logs", params);
}

// ── Memory ──

export function getMemory(channel: string) {
  return get("/memory", { channel: channel || "general" });
}

export function setMemory(namespace: string, key: string, value: string) {
  return post("/memory", { namespace, key, value });
}

// ── Config (Settings) ──

export type LLMProvider =
  | "claude-code"
  | "codex"
  | "opencode"
  | "mlx-lm"
  | "ollama"
  | "exo";
export type MemoryBackend = "nex" | "gbrain" | "none";
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
  llm_provider_priority?: string[];
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

export function updateConfig(patch: ConfigUpdate) {
  return post<{ status: string }>("/config", patch);
}

// Doctor endpoint — one entry per registered local OpenAI-compatible
// runtime. Settings page polls this; Onboarding wizard reads it on
// mount. The broker probes loopback endpoints only (see
// internal/team/local_providers_status.go), so calling this never
// triggers outbound traffic.
export function getLocalProvidersStatus() {
  return get<LocalProviderStatus[]>("/status/local-providers");
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
