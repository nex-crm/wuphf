/**
 * Typed WuphfAPI client.
 * Mirrors every method from the legacy IIFE in index.legacy.html.
 */

let apiBase = '/api'
let brokerDirect = 'http://localhost:7890'
let useProxy = true
let token: string | null = null

// ── Init ──

export async function initApi(): Promise<void> {
  try {
    const r = await fetch('/api-token')
    const data = await r.json()
    token = data.token
    if (data.broker_url) {
      brokerDirect = String(data.broker_url).replace(/\/+$/, '')
    }
    useProxy = true
  } catch {
    useProxy = false
    try {
      const r = await fetch(brokerDirect + '/web-token')
      const data = await r.json()
      token = data.token
    } catch {
      // broker unreachable — will fail on first request
    }
  }
}

// ── Internal helpers ──

function baseURL(): string {
  return useProxy ? apiBase : brokerDirect
}

function authHeaders(): Record<string, string> {
  const h: Record<string, string> = { 'Content-Type': 'application/json' }
  if (!useProxy && token) h['Authorization'] = `Bearer ${token}`
  return h
}

export async function get<T = unknown>(
  path: string,
  params?: Record<string, string | number | boolean | null | undefined>,
): Promise<T> {
  let url = baseURL() + path
  if (params) {
    const qs = Object.entries(params)
      .filter(([, v]) => v != null)
      .map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(String(v))}`)
      .join('&')
    if (qs) url += '?' + qs
  }
  const r = await fetch(url, { headers: authHeaders() })
  if (!r.ok) throw new Error(`${r.status} ${r.statusText}`)
  return r.json()
}

export async function post<T = unknown>(
  path: string,
  body?: unknown,
): Promise<T> {
  const r = await fetch(baseURL() + path, {
    method: 'POST',
    headers: authHeaders(),
    body: JSON.stringify(body),
  })
  if (!r.ok) throw new Error(`${r.status} ${r.statusText}`)
  return r.json()
}

export async function del<T = unknown>(
  path: string,
  body?: unknown,
): Promise<T> {
  const r = await fetch(baseURL() + path, {
    method: 'DELETE',
    headers: authHeaders(),
    body: JSON.stringify(body),
  })
  return r.json()
}

// ── SSE ──

export function sseURL(path: string): string {
  let url = baseURL() + path
  if (!useProxy && token) url += '?token=' + encodeURIComponent(token)
  return url
}

// ── Messages ──

export interface Message {
  id: string
  from: string
  channel: string
  content: string
  timestamp: string
  reply_to?: string
  thread_id?: string
  thread_count?: number
  reactions?: Record<string, string[]>
  tagged?: string[]
  usage?: TokenUsage
}

export interface TokenUsage {
  input_tokens?: number
  output_tokens?: number
  cache_read_tokens?: number
  cache_creation_tokens?: number
  total_tokens?: number
  cost_usd?: number
}

export function getMessages(channel: string, sinceId?: string | null, limit = 50) {
  return get<{ messages: Message[] }>('/messages', {
    channel: channel || 'general',
    viewer_slug: 'human',
    since_id: sinceId ?? null,
    limit,
  })
}

export function postMessage(content: string, channel: string, replyTo?: string) {
  const body: Record<string, string> = {
    from: 'you',
    channel: channel || 'general',
    content,
  }
  if (replyTo) body.reply_to = replyTo
  return post<Message>('/messages', body)
}

export function getThreadMessages(channel: string, threadId: string) {
  return get<{ messages: Message[] }>('/messages', {
    channel: channel || 'general',
    thread_id: threadId,
    viewer_slug: 'human',
    limit: 50,
  })
}

export function toggleReaction(msgId: string, emoji: string, channel: string) {
  return post('/messages/react', {
    message_id: msgId,
    emoji,
    channel: channel || 'general',
  })
}

// ── Members ──

export interface ProviderBinding {
  kind?: string
  model?: string
}

export interface OfficeMember {
  slug: string
  name: string
  role: string
  emoji?: string
  status?: string
  task?: string
  channel?: string
  provider?: ProviderBinding | string
}

export function getOfficeMembers() {
  return get<{ members: OfficeMember[] }>('/office-members')
}

export function getMembers(channel: string) {
  return get<{ members: OfficeMember[] }>('/members', {
    channel: channel || 'general',
    viewer_slug: 'human',
  })
}

// ── Channels ──

export interface Channel {
  slug: string
  name: string
  description?: string
  type?: string
  created_by?: string
  members?: string[]
}

export function getChannels() {
  return get<{ channels: Channel[] }>('/channels')
}

export function createChannel(slug: string, name: string, description: string) {
  return post('/channels', {
    action: 'create',
    slug,
    name: name || slug,
    description,
    created_by: 'you',
  })
}

export function generateChannel(prompt: string) {
  return post<Channel>('/channels/generate', { prompt })
}

export function createDM(agentSlug: string) {
  return post('/channels/dm', {
    members: ['human', agentSlug],
    type: 'direct',
  })
}

// ── Requests ──

export interface AgentRequest {
  id: string
  from: string
  question: string
  choices?: { id: string; label: string }[]
  channel?: string
  timestamp?: string
  status?: string
}

export function getRequests(channel: string) {
  return get<{ requests: AgentRequest[] }>('/requests', {
    channel: channel || 'general',
    viewer_slug: 'human',
  })
}

export function answerRequest(id: string, choiceId: string) {
  return post('/requests/answer', { id, choice_id: choiceId })
}

// ── Health ──

export function getHealth() {
  return get<{ status: string; agents?: Record<string, unknown> }>('/health')
}

// ── Tasks ──

export interface Task {
  id: string
  title: string
  description?: string
  status: string
  assigned_to?: string
  channel?: string
  created_at?: string
  updated_at?: string
}

export function getTasks(channel: string, opts?: { includeDone?: boolean; status?: string; mySlug?: string }) {
  const params: Record<string, string> = { viewer_slug: 'human', channel: channel || 'general' }
  if (opts?.includeDone) params.include_done = 'true'
  if (opts?.status) params.status = opts.status
  if (opts?.mySlug) params.my_slug = opts.mySlug
  return get<{ tasks: Task[] }>('/tasks', params)
}

export function getOfficeTasks(opts?: { includeDone?: boolean; status?: string; mySlug?: string }) {
  const params: Record<string, string> = { viewer_slug: 'human', all_channels: 'true' }
  if (opts?.includeDone) params.include_done = 'true'
  if (opts?.status) params.status = opts.status
  if (opts?.mySlug) params.my_slug = opts.mySlug
  return get<{ tasks: Task[] }>('/tasks', params)
}

// ── Signals / Decisions / Watchdogs / Actions ──

export function getSignals() { return get('/signals') }
export function getDecisions() { return get('/decisions') }
export function getWatchdogs() { return get('/watchdogs') }
export function getActions() { return get('/actions') }

// ── Policies ──

export interface Policy {
  id: string
  source: string
  rule: string
  active?: boolean
}

export function getPolicies() {
  return get<{ policies: Policy[] }>('/policies')
}

export function createPolicy(source: string, rule: string) {
  return post('/policies', { source, rule })
}

export function deletePolicy(id: string) {
  return del('/policies', { id })
}

// ── Scheduler ──

export interface SchedulerJob {
  id: string
  name?: string
  cron?: string
  next_run?: string
  last_run?: string
  status?: string
}

export function getScheduler(opts?: { dueOnly?: boolean }) {
  const params: Record<string, string> = {}
  if (opts?.dueOnly) params.due_only = 'true'
  return get<{ jobs: SchedulerJob[] }>('/scheduler', params)
}

// ── Skills ──

export interface Skill {
  name: string
  description?: string
  source?: string
  parameters?: unknown
}

export function getSkills() {
  return get<{ skills: Skill[] }>('/skills')
}

export function invokeSkill(name: string, params?: Record<string, unknown>) {
  return post(`/skills/${encodeURIComponent(name)}/invoke`, params ?? {})
}

// ── Usage ──

export interface AgentUsage {
  input_tokens: number
  output_tokens: number
  cache_read_tokens: number
  cost_usd: number
}

export interface UsageData {
  total?: { cost_usd: number; total_tokens?: number }
  session?: { total_tokens: number }
  agents?: Record<string, AgentUsage>
}

export function getUsage() {
  return get<UsageData>('/usage')
}

// ── Agent Logs ──

export interface AgentLog {
  id: string
  agent: string
  task?: string
  action?: string
  content?: string
  timestamp?: string
  usage?: TokenUsage
}

export function getAgentLogs(opts?: { limit?: number; task?: string }) {
  if (opts?.task) {
    return get<{ logs: AgentLog[] }>('/agent-logs', { task: opts.task })
  }
  const params: Record<string, string> = {}
  if (opts?.limit) params.limit = String(opts.limit)
  return get<{ logs: AgentLog[] }>('/agent-logs', params)
}

// ── Memory ──

export function getMemory(channel: string) {
  return get('/memory', { channel: channel || 'general' })
}

export function setMemory(namespace: string, key: string, value: string) {
  return post('/memory', { namespace, key, value })
}

// ── Studio ──

export function getStudioBootstrapPackage() {
  return get('/operations/bootstrap-package')
}

export function generateStudioPackage(payload?: unknown) {
  return post('/studio/generate-package', payload ?? {})
}

export function runStudioWorkflow(payload?: unknown) {
  return post('/studio/run-workflow', payload ?? {})
}
