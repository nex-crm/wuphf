import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  NavArrowRight,
  OpenNewWindow,
  Refresh,
  Search,
  Trash,
  WarningTriangle,
} from "iconoir-react";

import { getConfig, getLocalProvidersStatus } from "../../api/client";
import {
  disconnectIntegration,
  getIntegrationAudit,
  getIntegrationConnectStatus,
  type IntegrationAuditEvent,
  type IntegrationCatalogItem,
  type IntegrationConnectResult,
  type IntegrationProviderStatus,
  listIntegrations,
  startIntegrationConnection,
} from "../../api/integrations";
import { showNotice } from "../ui/Toast";
import {
  IntegrationDetailHeader,
  IntegrationListRow,
} from "./integrations/CardShell";
import { ToolkitBrandLogo } from "./integrations/IntegrationLogos";
import { INTEGRATIONS } from "./integrations/registry";
import {
  INTEGRATION_CATEGORIES,
  type IntegrationCategoryMeta,
  type IntegrationContext,
  type IntegrationDescriptor,
  type IntegrationStatus,
} from "./integrations/types";

function HelpBanner() {
  return (
    <div className="op-integrations-rule">
      <strong>Runtime control stays in Settings.</strong>
      <span>
        This page manages accounts, gateways, channels, and the audit trail for
        external actions.
      </span>
    </div>
  );
}

function ListSection({
  meta,
  descriptors,
  ctx,
  onOpen,
}: {
  meta: IntegrationCategoryMeta;
  descriptors: IntegrationDescriptor[];
  ctx: IntegrationContext;
  onOpen: (id: string) => void;
}) {
  if (descriptors.length === 0) return null;
  return (
    <section className="op-category">
      <header className="op-category-head">
        <h3 className="op-category-title">{meta.title}</h3>
        <p className="op-category-blurb">{meta.description}</p>
      </header>
      <div className="op-list">
        {descriptors.map((descriptor) => (
          <IntegrationListRow
            key={descriptor.id}
            logo={descriptor.logo()}
            title={descriptor.title}
            summary={descriptor.summary}
            status={descriptor.status(ctx)}
            onOpen={() => onOpen(descriptor.id)}
          />
        ))}
      </div>
    </section>
  );
}

function RegistryListView({
  ctx,
  available,
  onOpen,
}: {
  ctx: IntegrationContext;
  available: IntegrationDescriptor[];
  onOpen: (id: string) => void;
}) {
  const grouped = useMemo(() => {
    const map = new Map<string, IntegrationDescriptor[]>();
    for (const descriptor of available) {
      const bucket = map.get(descriptor.category) ?? [];
      bucket.push(descriptor);
      map.set(descriptor.category, bucket);
    }
    return map;
  }, [available]);
  return (
    <>
      {INTEGRATION_CATEGORIES.map((meta) => (
        <ListSection
          key={meta.id}
          meta={meta}
          descriptors={grouped.get(meta.id) ?? []}
          ctx={ctx}
          onOpen={onOpen}
        />
      ))}
    </>
  );
}

function RegistryDetailView({
  descriptor,
  ctx,
  onBack,
}: {
  descriptor: IntegrationDescriptor;
  ctx: IntegrationContext;
  onBack: () => void;
}) {
  return (
    <section className="op-detail">
      <IntegrationDetailHeader
        logo={descriptor.logo()}
        title={descriptor.title}
        summary={descriptor.summary}
        status={descriptor.status(ctx)}
        onBack={onBack}
      />
      <div className="op-detail-body">{descriptor.render(ctx)}</div>
    </section>
  );
}

function ToolkitLogo({ item }: { item: IntegrationCatalogItem }) {
  const [failed, setFailed] = useState(false);
  const label = item.name.slice(0, 1).toUpperCase();
  const brandLogo = ToolkitBrandLogo({ platform: item.platform });
  if (item.logo_url) {
    return (
      <span className="op-toolkit-mark">
        {!failed ? (
          <img
            className="op-toolkit-logo"
            src={item.logo_url}
            alt=""
            loading="lazy"
            onError={() => setFailed(true)}
          />
        ) : brandLogo ? (
          brandLogo
        ) : (
          <span className="op-toolkit-monogram" aria-hidden="true">
            {label}
          </span>
        )}
      </span>
    );
  }
  return (
    <span className="op-toolkit-mark">
      {brandLogo ?? (
        <span className="op-toolkit-monogram" aria-hidden="true">
          {label}
        </span>
      )}
    </span>
  );
}

function toolkitStatus(item: IntegrationCatalogItem): IntegrationStatus {
  switch (item.state) {
    case "connected":
      return { tone: "connected", label: "Connected" };
    case "pending":
      return { tone: "warning", label: "Pending" };
    case "failed":
      return { tone: "warning", label: "Failed" };
    case "unconfigured":
      return { tone: "unconfigured", label: "Not configured" };
    default:
      return item.can_connect
        ? { tone: "available", label: "Available" }
        : { tone: "unconfigured", label: "Read only" };
  }
}

function toolkitToneClass(status: IntegrationStatus) {
  switch (status.tone) {
    case "connected":
      return "on";
    case "available":
      return "info";
    case "warning":
      return "warn";
    default:
      return "off";
  }
}

function ProviderStrip({
  providers,
}: {
  providers: IntegrationProviderStatus[];
}) {
  if (providers.length === 0) return null;
  return (
    <div className="op-provider-strip">
      {providers.map((provider) => (
        <div className="op-provider-chip" key={provider.provider}>
          <span
            className={`op-led ${provider.configured ? "is-on" : "is-off"}`}
            aria-hidden="true"
          />
          <span className="op-provider-name">{provider.label}</span>
          <span className="op-provider-detail">
            {provider.detail || (provider.configured ? "ready" : "missing")}
          </span>
        </div>
      ))}
    </div>
  );
}

function ActionToolkitsSection({
  items,
  onOpen,
}: {
  items: IntegrationCatalogItem[];
  onOpen: (item: IntegrationCatalogItem) => void;
}) {
  return (
    <section className="op-category">
      <header className="op-category-head">
        <div>
          <h3 className="op-category-title">Action Accounts</h3>
          <p className="op-category-blurb">
            OAuth accounts agents can request through approval.
          </p>
        </div>
        <span className="op-section-count">{items.length} accounts</span>
      </header>
      <div className="op-toolkit-ledger">
        <div className="op-toolkit-ledger-head" aria-hidden="true">
          <span>Integration</span>
          <span>Scope</span>
          <span>Account</span>
          <span>Status</span>
        </div>
        {items.map((item) => (
          <ToolkitRow
            key={`${item.provider}:${item.platform}:${item.connection_key ?? "catalog"}`}
            item={item}
            onOpen={() => onOpen(item)}
          />
        ))}
      </div>
    </section>
  );
}

function formatToolkitCategory(category?: string) {
  const value = category?.trim();
  if (!value) return "Toolkit";
  return value
    .replace(/[-_]+/g, " ")
    .replace(/\s+/g, " ")
    .replace(/\b\w/g, (char) => char.toUpperCase());
}

function toolkitConnectionName(item: IntegrationCatalogItem) {
  return (
    item.connection_name ||
    item.connections?.find((connection) => connection.name)?.name ||
    item.connection_key ||
    ""
  );
}

function ToolkitRow({
  item,
  onOpen,
}: {
  item: IntegrationCatalogItem;
  onOpen: () => void;
}) {
  const status = toolkitStatus(item);
  const tone = toolkitToneClass(status);
  const connectionName = toolkitConnectionName(item);
  const summary =
    item.last_action_summary ??
    item.description ??
    `${item.provider} account actions for ${item.platform}`;
  const category = formatToolkitCategory(item.category);
  return (
    <button
      type="button"
      className={`op-toolkit-row is-${tone}`}
      onClick={onOpen}
      aria-label={`Open ${item.name} integration settings`}
    >
      <span className="op-toolkit-row-logo" aria-hidden="true">
        <ToolkitLogo item={item} />
      </span>
      <span className="op-toolkit-row-body">
        <span className="op-toolkit-row-title">{item.name}</span>
        <span className="op-toolkit-row-summary">{summary}</span>
      </span>
      <span className="op-toolkit-row-cell">{category}</span>
      <span className="op-toolkit-row-cell">
        {connectionName || "No account"}
      </span>
      <span className="op-toolkit-row-state">
        <span className={`op-status is-${tone}`}>
          <span className={`op-led is-${tone}`} />
          {status.label}
        </span>
        <NavArrowRight width={18} height={18} aria-hidden="true" />
      </span>
    </button>
  );
}

function AuditList({ events }: { events: IntegrationAuditEvent[] }) {
  if (events.length === 0) {
    return <p className="op-runtime-note">No integration audit events yet.</p>;
  }
  return (
    <div className="op-audit-list">
      {events.map((event) => (
        <div className="op-audit-row" key={`${event.id}:${event.event_type}`}>
          <div className="op-audit-main">
            <span className="op-audit-kind">{event.event_type}</span>
            <span className="op-audit-summary">
              {event.summary || event.action_id || event.related_id}
            </span>
          </div>
          <div className="op-audit-meta">
            {event.actor ? `@${event.actor}` : "system"} ·{" "}
            {new Date(event.created_at).toLocaleString()}
          </div>
        </div>
      ))}
    </div>
  );
}

function ToolkitDetail({
  item,
  onBack,
}: {
  item: IntegrationCatalogItem;
  onBack: () => void;
}) {
  const queryClient = useQueryClient();
  const [pending, setPending] = useState<IntegrationConnectResult | null>(null);
  const [confirmDisconnect, setConfirmDisconnect] = useState(false);
  const connectionKey = item.connection_key || item.connections?.[0]?.key || "";
  const auditQuery = useQuery({
    queryKey: [
      "integrations-audit",
      item.provider,
      item.platform,
      connectionKey,
    ],
    queryFn: () =>
      getIntegrationAudit({
        provider: item.provider,
        platform: item.platform,
        connection_key: connectionKey || undefined,
        limit: 30,
      }),
    staleTime: 10_000,
  });
  const statusQuery = useQuery({
    queryKey: [
      "integration-connect-status",
      pending?.provider,
      pending?.connect_id,
      pending?.platform,
    ],
    enabled: Boolean(pending?.connect_id),
    queryFn: () =>
      getIntegrationConnectStatus({
        provider: pending?.provider ?? item.provider,
        platform: pending?.platform ?? item.platform,
        connect_id: pending?.connect_id,
      }),
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status === "connected" || status === "failed" ? false : 5_000;
    },
  });

  useEffect(() => {
    if (statusQuery.data?.status !== "connected") return;
    setPending(null);
    void queryClient.invalidateQueries({ queryKey: ["integrations"] });
    void queryClient.invalidateQueries({ queryKey: ["integrations-audit"] });
  }, [queryClient, statusQuery.data?.status]);

  const connectMutation = useMutation({
    mutationFn: () => startIntegrationConnection(item.provider, item.platform),
    onSuccess: (result) => {
      setPending(result);
      if (result.auth_url) {
        window.open(result.auth_url, "_blank", "noopener,noreferrer");
      }
      showNotice(`Started ${item.name} connection.`, "success");
      void queryClient.invalidateQueries({ queryKey: ["integrations-audit"] });
    },
    onError: (err) => {
      showNotice(
        err instanceof Error ? err.message : `Failed to connect ${item.name}`,
        "error",
      );
    },
  });
  const disconnectMutation = useMutation({
    mutationFn: () => disconnectIntegration(item.provider, connectionKey),
    onSuccess: () => {
      setConfirmDisconnect(false);
      showNotice(`${item.name} disconnected.`, "success");
      void queryClient.invalidateQueries({ queryKey: ["integrations"] });
      void queryClient.invalidateQueries({ queryKey: ["integrations-audit"] });
      onBack();
    },
    onError: (err) => {
      showNotice(
        err instanceof Error
          ? err.message
          : `Failed to disconnect ${item.name}`,
        "error",
      );
    },
  });

  const latestStatus = statusQuery.data?.status ?? pending?.status;
  const canDisconnect = item.can_disconnect && connectionKey !== "";
  const connectionName = toolkitConnectionName(item);
  return (
    <section className="op-detail">
      <IntegrationDetailHeader
        logo={<ToolkitLogo item={item} />}
        title={item.name}
        summary={item.description || `${item.provider} · ${item.platform}`}
        status={toolkitStatus(item)}
        onBack={onBack}
      />
      <div className="op-detail-body">
        <section className="op-toolkit-panel">
          <div className="op-toolkit-panel-copy">
            <span className="op-eyebrow">Connection</span>
            <dl className="op-connection-grid">
              <div>
                <dt>Provider</dt>
                <dd>{item.provider}</dd>
              </div>
              <div>
                <dt>Category</dt>
                <dd>{formatToolkitCategory(item.category)}</dd>
              </div>
              <div>
                <dt>Account</dt>
                <dd>{connectionName || "Not connected"}</dd>
              </div>
              <div>
                <dt>Connection key</dt>
                <dd>
                  {connectionKey ? (
                    <code className="op-inline-code">{connectionKey}</code>
                  ) : (
                    "None"
                  )}
                </dd>
              </div>
            </dl>
          </div>
          <div className="op-toolkit-actions">
            <button
              type="button"
              className="btn btn-primary btn-sm"
              disabled={!item.can_connect || connectMutation.isPending}
              onClick={() => connectMutation.mutate()}
            >
              <OpenNewWindow width={14} height={14} aria-hidden="true" />
              {connectionKey ? "Connect another account" : "Connect"}
            </button>
            <button
              type="button"
              className="btn btn-secondary btn-sm"
              disabled={statusQuery.isFetching || !pending}
              onClick={() => void statusQuery.refetch()}
            >
              <Refresh width={14} height={14} aria-hidden="true" />
              Check status
            </button>
            <button
              type="button"
              className="btn btn-secondary btn-sm"
              disabled={!canDisconnect || disconnectMutation.isPending}
              onClick={() => setConfirmDisconnect(true)}
            >
              <Trash width={14} height={14} aria-hidden="true" />
              Disconnect
            </button>
          </div>
        </section>
        {confirmDisconnect ? (
          <div className="op-confirm-row">
            <span>Disconnect {item.name}?</span>
            <button
              type="button"
              className="btn btn-secondary btn-sm"
              onClick={() => setConfirmDisconnect(false)}
            >
              Cancel
            </button>
            <button
              type="button"
              className="btn btn-primary btn-sm"
              disabled={disconnectMutation.isPending}
              onClick={() => disconnectMutation.mutate()}
            >
              Confirm
            </button>
          </div>
        ) : null}
        {latestStatus ? (
          <p className="op-runtime-note">
            Connection status: <strong>{latestStatus}</strong>
          </p>
        ) : null}
        <section className="op-audit-panel">
          <div className="op-audit-head">
            <div>
              <h3 className="op-subhead">Audit</h3>
              <p>Action and approval events for this account.</p>
            </div>
            <button
              type="button"
              className="btn btn-secondary btn-sm"
              onClick={() => void auditQuery.refetch()}
              disabled={auditQuery.isFetching}
            >
              <Refresh width={14} height={14} aria-hidden="true" />
              Refresh
            </button>
          </div>
          <AuditList events={auditQuery.data ?? []} />
        </section>
      </div>
    </section>
  );
}

function IntegrationsHome({
  providers,
  search,
  connected,
  toolkitItems,
  isLoading,
  available,
  ctx,
  onSearch,
  onConnected,
  onOpenToolkit,
  onOpenRegistry,
}: {
  providers: IntegrationProviderStatus[];
  search: string;
  connected: string;
  toolkitItems: IntegrationCatalogItem[];
  isLoading: boolean;
  available: IntegrationDescriptor[];
  ctx: IntegrationContext;
  onSearch: (value: string) => void;
  onConnected: (value: string) => void;
  onOpenToolkit: (item: IntegrationCatalogItem) => void;
  onOpenRegistry: (id: string) => void;
}) {
  return (
    <>
      <ProviderStrip providers={providers} />
      <div className="op-toolbar">
        <label className="op-search">
          <Search width={14} height={14} aria-hidden="true" />
          <input
            type="search"
            placeholder="Search accounts"
            value={search}
            onChange={(event) => onSearch(event.target.value)}
          />
        </label>
        <select
          className="input op-filter-select"
          value={connected}
          onChange={(event) => onConnected(event.target.value)}
          aria-label="Filter integrations"
        >
          <option value="">All</option>
          <option value="connected">Connected</option>
          <option value="available">Available</option>
        </select>
      </div>
      {isLoading ? (
        <div className="app-panel-loading">Loading accounts…</div>
      ) : toolkitItems.length > 0 ? (
        <ActionToolkitsSection items={toolkitItems} onOpen={onOpenToolkit} />
      ) : (
        <p className="op-runtime-note op-empty-state">
          No accounts match this view. Configure Composio or clear the filter.
        </p>
      )}
      <RegistryListView
        ctx={ctx}
        available={available}
        onOpen={onOpenRegistry}
      />
    </>
  );
}

function EmptyIntegrationsWarning({
  available,
  toolkitItems,
}: {
  available: IntegrationDescriptor[];
  toolkitItems: IntegrationCatalogItem[];
}) {
  if (available.length > 0 || toolkitItems.length > 0) return null;
  return (
    <p className="op-empty-warning">
      <WarningTriangle width={12} height={12} />
      No integrations are registered in this build.
    </p>
  );
}

export function IntegrationsApp() {
  const cfgQuery = useQuery({
    queryKey: ["config"],
    queryFn: getConfig,
    staleTime: 30_000,
  });
  const statusQuery = useQuery({
    queryKey: ["local-providers-status"],
    queryFn: getLocalProvidersStatus,
    refetchInterval: 30_000,
    staleTime: 5_000,
  });

  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [selectedToolkit, setSelectedToolkit] =
    useState<IntegrationCatalogItem | null>(null);
  const [search, setSearch] = useState("");
  const [connected, setConnected] = useState("");
  const integrationsQuery = useQuery({
    queryKey: ["integrations", search, connected],
    queryFn: () =>
      listIntegrations({
        search: search.trim() || undefined,
        connected: connected || undefined,
        limit: 60,
      }),
    staleTime: 10_000,
  });

  if (cfgQuery.isLoading) {
    return <div className="app-panel-loading">Loading integrations…</div>;
  }

  const ctx: IntegrationContext = {
    cfg: cfgQuery.data ?? {},
    localStatuses: statusQuery.data ?? [],
  };

  const available = INTEGRATIONS.filter((descriptor) =>
    descriptor.isAvailable(ctx),
  );
  const selected = selectedId
    ? (available.find((descriptor) => descriptor.id === selectedId) ?? null)
    : null;
  const toolkitItems = integrationsQuery.data?.items ?? [];

  return (
    <div className="op-page">
      <header className="op-page-header">
        <h2>Integrations</h2>
        <p>External accounts, gateways, channels, and action audit.</p>
      </header>

      {!(selected || selectedToolkit) && <HelpBanner />}

      {selectedToolkit ? (
        <ToolkitDetail
          item={selectedToolkit}
          onBack={() => setSelectedToolkit(null)}
        />
      ) : selected ? (
        <RegistryDetailView
          descriptor={selected}
          ctx={ctx}
          onBack={() => setSelectedId(null)}
        />
      ) : (
        <IntegrationsHome
          providers={integrationsQuery.data?.providers ?? []}
          search={search}
          connected={connected}
          toolkitItems={toolkitItems}
          isLoading={integrationsQuery.isLoading}
          available={available}
          ctx={ctx}
          onSearch={setSearch}
          onConnected={setConnected}
          onOpenToolkit={setSelectedToolkit}
          onOpenRegistry={setSelectedId}
        />
      )}

      <EmptyIntegrationsWarning
        available={available}
        toolkitItems={toolkitItems}
      />
    </div>
  );
}
