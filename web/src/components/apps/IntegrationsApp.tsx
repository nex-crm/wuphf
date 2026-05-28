import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Lock, WarningTriangle } from "iconoir-react";

import { getConfig, getLocalProvidersStatus } from "../../api/client";
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
} from "./integrations/types";

// IntegrationsApp drives a two-mode UX on top of the registry:
//
//   1. List mode (default): grid of clickable rows showing logo + title +
//      one-line summary + status pill, grouped by category. The whole row
//      is the click target — no per-row buttons compete with it.
//
//   2. Detail mode: when a row is selected, the app swaps to a detail view
//      with a back button, a re-stated header, and the descriptor's
//      render() body. The body is whatever form / actions that specific
//      integration needs.
//
// Adding a new integration is still a single descriptor entry in
// registry.tsx plus a logo and a *Detail component — IntegrationsApp has
// zero per-integration knowledge.

function HelpBanner() {
  return (
    <div className="op-lock-card" style={{ marginBottom: 28 }}>
      <div className="op-lock-head">
        <div className="op-lock-title">
          <span className="op-lock-icon" aria-hidden="true">
            <Lock width={14} height={14} />
          </span>
          <span className="op-lock-title-text">
            Integrations are gateways, not runtimes
          </span>
        </div>
      </div>
      <p className="op-lock-body" style={{ marginBottom: 0 }}>
        Integrations bring agents or messaging streams into the team from
        outside. They are not LLM runtimes — pick those in{" "}
        <em>Settings → Default runtime</em>. Agent-importing gateways
        (External Agents) tag their imported agents with a "Managed by
        &lt;Gateway&gt;" badge in the agent profile.
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
        {descriptors.map((d) => (
          <IntegrationListRow
            key={d.id}
            logo={d.logo()}
            title={d.title}
            summary={d.summary}
            status={d.status(ctx)}
            onOpen={() => onOpen(d.id)}
          />
        ))}
      </div>
    </section>
  );
}

function ListView({
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
    for (const d of available) {
      const bucket = map.get(d.category) ?? [];
      bucket.push(d);
      map.set(d.category, bucket);
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

function DetailView({
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

  if (cfgQuery.isLoading) {
    return <div className="app-panel-loading">Loading integrations…</div>;
  }

  const ctx: IntegrationContext = {
    cfg: cfgQuery.data ?? {},
    localStatuses: statusQuery.data ?? [],
  };

  const available = INTEGRATIONS.filter((d) => d.isAvailable(ctx));
  const selected = selectedId
    ? available.find((d) => d.id === selectedId) ?? null
    : null;

  return (
    <div
      style={{
        maxWidth: 820,
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
            letterSpacing: -0.2,
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
          Bring agents and messaging streams in from outside the team.
        </p>
      </header>

      {!selected && <HelpBanner />}

      {selected ? (
        <DetailView
          descriptor={selected}
          ctx={ctx}
          onBack={() => setSelectedId(null)}
        />
      ) : (
        <ListView
          ctx={ctx}
          available={[...available]}
          onOpen={(id) => setSelectedId(id)}
        />
      )}

      {available.length === 0 && (
        <p
          style={{ marginTop: 12, fontSize: 12, color: "var(--text-tertiary)" }}
        >
          <WarningTriangle
            width={12}
            height={12}
            style={{ marginRight: 4, verticalAlign: "text-bottom" }}
          />
          No integrations are registered in this build.
        </p>
      )}
    </div>
  );
}
