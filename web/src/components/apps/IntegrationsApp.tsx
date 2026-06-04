import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Lock,
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
    <div className="op-lock-card" style={{ marginBottom: 28 }}>
      <div className="op-lock-head">
        <div className="op-lock-title">
          <span className="op-lock-icon" aria-hidden="true">
            <Lock width={14} height={14} />
          </span>
          <span className="op-lock-title-text">
            Integrations are gateways and action accounts, not runtimes
          </span>
        </div>
      </div>
      <p className="op-lock-body" style={{ marginBottom: 0 }}>
        Connect external accounts agents can use for approved actions, and bring
        agents or messaging streams in from outside the team. Pick LLM runtimes
        in <em>Settings → Default runtime</em>.
      </p>
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
  if (item.logo_url) {
    return <img src={item.logo_url} alt="" style={{ width: 28, height: 28 }} />;
  }
  return (
    <span className="op-toolkit-monogram" aria-hidden="true">
      {item.name.slice(0, 1).toUpperCase()}
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
          <div>
            <div className="op-provider-name">{provider.label}</div>
            <div className="op-provider-detail">{provider.detail}</div>
          </div>
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
        <h3 className="op-category-title">Action Toolkits</h3>
        <p className="op-category-blurb">
          OAuth-backed accounts agents can use for approved external actions.
        </p>
      </header>
      <div className="op-list">
        {items.map((item) => (
          <IntegrationListRow
            key={`${item.provider}:${item.platform}:${item.connection_key ?? "catalog"}`}
            logo={<ToolkitLogo item={item} />}
            title={item.name}
            summary={
              item.last_action_summary ??
              item.description ??
              `${item.provider} · ${item.platform}`
            }
            status={toolkitStatus(item)}
            onOpen={() => onOpen(item)}
          />
        ))}
      </div>
    </section>
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
        {connectionKey ? (
          <p className="op-runtime-note">
            Connection key:{" "}
            <code style={{ fontFamily: "var(--font-mono)", fontSize: 12 }}>
              {connectionKey}
            </code>
          </p>
        ) : null}
        <h3 className="op-subhead">Audit</h3>
        <AuditList events={auditQuery.data ?? []} />
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
            placeholder="Search toolkits"
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
        <div className="app-panel-loading">Loading action toolkits…</div>
      ) : toolkitItems.length > 0 ? (
        <ActionToolkitsSection items={toolkitItems} onOpen={onOpenToolkit} />
      ) : (
        <p className="op-runtime-note" style={{ marginBottom: 24 }}>
          No action toolkits found. Configure Composio in Settings or adjust the
          search filter.
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
    <p style={{ marginTop: 12, fontSize: 12, color: "var(--text-tertiary)" }}>
      <WarningTriangle
        width={12}
        height={12}
        style={{ marginRight: 4, verticalAlign: "text-bottom" }}
      />
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
    <div
      style={{
        maxWidth: 900,
        margin: "0 auto",
        padding: "28px 24px 56px 24px",
      }}
    >
      <header style={{ marginBottom: 22 }}>
        <span className="op-eyebrow op-eyebrow-strong">PATCH BAY</span>
        <h2
          style={{
            margin: "6px 0 4px 0",
            fontSize: 24,
            fontWeight: 700,
          }}
        >
          Integrations
        </h2>
        <p
          style={{
            margin: 0,
            fontSize: "var(--text-base)",
            color: "var(--text-tertiary)",
            lineHeight: 1.5,
          }}
        >
          Add, manage, and audit external systems connected to the office.
        </p>
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
