// Integrations a Work Tool uses, in the operator's own styling. We reuse the
// REAL data + logos + connect flow (listIntegrations, the toolkit brand logos,
// and the human-interview ConnectIntegrationCard), but render them with operator
// tokens and operator copy — no main-app chrome, no "agents"/"channels"
// vocabulary. Connected once for the workspace; shown scoped under each tool.

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";

import type { AgentRequest } from "../../api/client";
import {
  type IntegrationCatalogItem,
  listIntegrations,
} from "../../api/integrations";
import {
  GenericIntegrationLogo,
  ToolkitBrandLogo,
} from "../../components/apps/integrations/IntegrationLogos";
import { ConnectIntegrationCard } from "../../components/messages/ConnectIntegrationCard";
import { Eyebrow } from "../components/primitives";

interface ToolIntegrationsProps {
  /** Integration names the tool's workflow references (from its steps). */
  usedNames: string[];
}

export function ToolIntegrations({ usedNames }: ToolIntegrationsProps) {
  const [search, setSearch] = useState("");
  const [connecting, setConnecting] = useState<string | null>(null);

  const query = useQuery({
    queryKey: ["operator-integrations"],
    queryFn: () => listIntegrations({ limit: 100 }),
    staleTime: 30_000,
  });

  const items = query.data?.items ?? [];
  const filtered = useMemo(() => {
    const needle = search.trim().toLowerCase();
    if (!needle) return items;
    return items.filter(
      (i) =>
        i.name.toLowerCase().includes(needle) ||
        i.platform.toLowerCase().includes(needle),
    );
  }, [items, search]);

  const connected = filtered.filter((i) => i.state === "connected");
  const available = filtered.filter((i) => i.state !== "connected");

  return (
    <div className="opr-tool-scoped">
      {usedNames.length > 0 ? (
        <>
          <Eyebrow>Used by this tool</Eyebrow>
          <ul className="opr-scoped-chips">
            {usedNames.map((name) => (
              <li key={name} className="opr-scoped-chip">
                {name}
              </li>
            ))}
          </ul>
        </>
      ) : (
        <p className="opr-scoped-note">
          This tool does not use any integrations yet.
        </p>
      )}
      <p className="opr-scoped-note">
        Connected once for your workspace and reused across tools. Search and
        connect from your full catalog below.
      </p>

      <input
        className="opr-conn-input opr-int-search"
        type="search"
        placeholder="Search integrations"
        aria-label="Search integrations"
        value={search}
        onChange={(e) => setSearch(e.target.value)}
      />

      {query.isLoading ? (
        <p className="opr-scoped-note">Loading your integrations…</p>
      ) : query.isError ? (
        <p className="opr-scoped-note">
          Could not load integrations right now.
        </p>
      ) : (
        <>
          {connected.length > 0 ? (
            <IntegrationSection
              label="Connected"
              items={connected}
              connecting={connecting}
              onConnect={setConnecting}
            />
          ) : null}
          <IntegrationSection
            label="Available"
            items={available}
            connecting={connecting}
            onConnect={setConnecting}
          />
        </>
      )}
    </div>
  );
}

function IntegrationSection({
  label,
  items,
  connecting,
  onConnect,
}: {
  label: string;
  items: IntegrationCatalogItem[];
  connecting: string | null;
  onConnect: (platform: string | null) => void;
}) {
  if (items.length === 0) return null;
  return (
    <section>
      <div className="opr-section-label">
        <Eyebrow>{label}</Eyebrow>
        <div className="opr-section-rule" />
      </div>
      <div className="opr-int-grid">
        {items.map((item) => (
          <IntegrationCard
            key={`${item.platform}:${item.connection_key ?? "catalog"}`}
            item={item}
            connecting={connecting === item.platform}
            onConnect={() => onConnect(item.platform)}
            onClose={() => onConnect(null)}
          />
        ))}
      </div>
    </section>
  );
}

// The real logo: the catalog's logo_url first (covers every integration), then
// the curated brand SVG, then a generic glyph — mirroring the main app.
function IntegrationLogo({ item }: { item: IntegrationCatalogItem }) {
  const [failed, setFailed] = useState(false);
  if (item.logo_url && !failed) {
    return (
      <img
        className="opr-int-logo-img"
        src={item.logo_url}
        alt=""
        loading="lazy"
        onError={() => setFailed(true)}
      />
    );
  }
  return ToolkitBrandLogo({ platform: item.platform }) ?? <GenericIntegrationLogo />;
}

function IntegrationCard({
  item,
  connecting,
  onConnect,
  onClose,
}: {
  item: IntegrationCatalogItem;
  connecting: boolean;
  onConnect: () => void;
  onClose: () => void;
}) {
  const isConnected = item.state === "connected";

  if (connecting) {
    return (
      <div className="opr-int-card is-connecting">
        <ConnectIntegrationCard
          request={toConnectRequest(item)}
          submitting={false}
          onSkip={onClose}
          onDismiss={onClose}
        />
      </div>
    );
  }

  return (
    <div className="opr-int-card">
      <span className="opr-int-logo" aria-hidden={true}>
        <IntegrationLogo item={item} />
      </span>
      <div className="opr-int-body">
        <div className="opr-int-name">{item.name}</div>
        {item.category ? (
          <div className="opr-int-category">{item.category}</div>
        ) : null}
      </div>
      {isConnected ? (
        <span className="opr-int-connected">Connected</span>
      ) : (
        <button
          type="button"
          className="opr-btn opr-btn-sm"
          onClick={onConnect}
          disabled={!item.can_connect}
        >
          Connect
        </button>
      )}
    </div>
  );
}

function toConnectRequest(item: IntegrationCatalogItem): AgentRequest {
  return {
    id: `operator-connect-${item.platform.toLowerCase()}`,
    from: "your AI",
    question: `Connect ${item.name} so your tools can use it.`,
    title: `Connect ${item.name}`,
    platform: item.platform,
    kind: "connect",
  };
}
