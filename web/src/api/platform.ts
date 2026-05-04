import { get, post } from "./client";

// /version returns the build-info baked into the running broker binary
// (set at link time via -ldflags `-X .../buildinfo.Version=...`). For an
// upgrade-vs-latest comparison, call /upgrade-check instead.
export interface VersionInfo {
  version: string;
  // Always populated by the backend. Defaults to "unknown" when no
  // BuildTimestamp ldflag was set.
  build_timestamp: string;
}

export interface HealthResponse {
  status: string;
  session_mode: string;
  one_on_one_agent: string;
  focus_mode: boolean;
  provider: string;
  provider_model: string;
  memory_backend: string;
  memory_backend_active: string;
  memory_backend_ready: boolean;
  nex_connected: boolean;
  build: VersionInfo;
}

export interface UsageTotals {
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens: number;
  cache_creation_tokens: number;
  total_tokens: number;
  cost_usd: number;
  requests: number;
}

export type AgentUsage = UsageTotals;

export interface UsageData {
  total: UsageTotals;
  session?: UsageTotals;
  agents?: Record<string, AgentUsage>;
  since?: string;
}

export function getHealth() {
  return get<HealthResponse>("/health");
}

export interface HumanSession {
  id: string;
  invite_id: string;
  human_slug: string;
  display_name: string;
  device?: string;
  created_at: string;
  expires_at: string;
  revoked_at?: string;
  last_seen_at?: string;
}

export interface HumanMe {
  human: {
    id?: string;
    slug?: string;
    role?: string;
    invite_id?: string;
    human_slug?: string;
    display_name?: string;
    device?: string;
    created_at?: string;
    expires_at?: string;
    revoked_at?: string;
    last_seen_at?: string;
  };
}

export function getHumanMe() {
  return get<HumanMe>("/humans/me");
}

export function getHumanSessions() {
  return get<{ sessions?: HumanSession[] }>("/humans/sessions");
}

export interface WebShareStatus {
  running: boolean;
  bind?: string;
  interface?: string;
  invite_url?: string;
  expires_at?: string;
  error?: string;
}

export function getShareStatus() {
  return get<WebShareStatus>("/share/status");
}

export function startShare() {
  return post<WebShareStatus>("/share/start", {});
}

export function stopShare() {
  return post<WebShareStatus>("/share/stop", {});
}

export function getVersion() {
  return get<VersionInfo>("/version");
}

export function getUsage() {
  return get<UsageData>("/usage");
}
