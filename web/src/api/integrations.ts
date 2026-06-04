import { get, post } from "./client";

export type IntegrationProvider = "composio" | "one" | string;

export interface IntegrationProviderStatus {
  provider: IntegrationProvider;
  label: string;
  configured: boolean;
  supports_connect: boolean;
  supports_disconnect: boolean;
  detail?: string;
}

export interface IntegrationConnection {
  platform: string;
  state?: string;
  key: string;
  name?: string;
  tags?: string[];
}

export interface IntegrationCatalogItem {
  provider: IntegrationProvider;
  platform: string;
  name: string;
  description?: string;
  category?: string;
  logo_url?: string;
  state: string;
  connection_key?: string;
  connection_name?: string;
  can_connect: boolean;
  can_disconnect: boolean;
  connections?: IntegrationConnection[];
  last_action_at?: string;
  last_action_summary?: string;
}

export interface IntegrationsResponse {
  providers: IntegrationProviderStatus[];
  items: IntegrationCatalogItem[];
  next_cursor?: string;
}

export interface ListIntegrationsParams {
  provider?: string;
  search?: string;
  connected?: string;
  limit?: number;
  cursor?: string;
}

export interface IntegrationConnectResult {
  provider: IntegrationProvider;
  platform: string;
  status: string;
  auth_url?: string;
  connect_id?: string;
  connection_key?: string;
  expires_at?: string;
  instructions?: string;
}

export interface IntegrationDisconnectResult {
  ok: boolean;
  provider: IntegrationProvider;
  platform?: string;
  connection_key: string;
  status: string;
}

export interface IntegrationAuditEvent {
  id: string;
  event_type: string;
  provider?: string;
  platform?: string;
  connection_key?: string;
  action_id?: string;
  status?: string;
  actor?: string;
  channel?: string;
  summary?: string;
  related_id?: string;
  created_at: string;
  metadata?: Record<string, string>;
}

export async function listIntegrations(
  params: ListIntegrationsParams = {},
): Promise<IntegrationsResponse> {
  return get<IntegrationsResponse>("/integrations", {
    provider: params.provider,
    search: params.search,
    connected: params.connected,
    limit: params.limit,
    cursor: params.cursor,
  });
}

export async function startIntegrationConnection(
  provider: IntegrationProvider,
  platform: string,
): Promise<IntegrationConnectResult> {
  return post<IntegrationConnectResult>("/integrations/connect", {
    provider,
    platform,
  });
}

export async function getIntegrationConnectStatus(params: {
  provider: IntegrationProvider;
  platform?: string;
  connect_id?: string;
}): Promise<IntegrationConnectResult> {
  return get<IntegrationConnectResult>("/integrations/connect-status", params);
}

export async function disconnectIntegration(
  provider: IntegrationProvider,
  connectionKey: string,
  platform?: string,
): Promise<IntegrationDisconnectResult> {
  return post<IntegrationDisconnectResult>("/integrations/disconnect", {
    provider,
    platform,
    connection_key: connectionKey,
  });
}

export async function getIntegrationAudit(
  params: {
    provider?: string;
    platform?: string;
    connection_key?: string;
    limit?: number;
  } = {},
): Promise<IntegrationAuditEvent[]> {
  const resp = await get<{ events: IntegrationAuditEvent[] }>(
    "/integrations/audit",
    params,
  );
  return resp.events ?? [];
}
