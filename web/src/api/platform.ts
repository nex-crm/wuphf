import { get } from "./client";

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

export function getVersion() {
  return get<VersionInfo>("/version");
}

export function getUsage() {
  return get<UsageData>("/usage");
}
