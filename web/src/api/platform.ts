import { get } from "./client";

export interface HealthResponse {
  status: string;
  provider?: string;
  provider_model?: string;
  agents?: Record<string, unknown>;
}

// /version returns the build-info baked into the running broker binary
// (set at link time via -ldflags `-X .../buildinfo.Version=...`). For an
// upgrade-vs-latest comparison, call /upgrade-check instead.
export interface VersionInfo {
  version: string;
  // Always populated by the backend. Defaults to "unknown" when no
  // BuildTimestamp ldflag was set.
  build_timestamp: string;
}

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

export function getHealth() {
  return get<HealthResponse>("/health");
}

export function getVersion() {
  return get<VersionInfo>("/version");
}

export function getUsage() {
  return get<UsageData>("/usage");
}
