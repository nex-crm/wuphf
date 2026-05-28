import { Fragment } from "react";
import { useQuery } from "@tanstack/react-query";
import { Lock, WarningTriangle } from "iconoir-react";

import { getConfig, getLocalProvidersStatus } from "../../api/client";
import { INTEGRATIONS } from "./integrations/registry";
import {
  INTEGRATION_CATEGORIES,
  type IntegrationCategoryMeta,
  type IntegrationContext,
  type IntegrationDescriptor,
} from "./integrations/types";

// IntegrationsApp renders the registry from
// components/apps/integrations/registry.ts grouped by category. Adding a
// new integration is a single descriptor entry — this file does not
// hardcode any specific integration.

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
        <em>Settings → Default runtime</em>. Agent-importing gateways (External
        Agents) tag their imported agents with a "Managed by &lt;Gateway&gt;"
        badge in the agent profile.
      </p>
    </div>
  );
}

function CategorySection({
  meta,
  descriptors,
  ctx,
}: {
  meta: IntegrationCategoryMeta;
  descriptors: IntegrationDescriptor[];
  ctx: IntegrationContext;
}) {
  if (descriptors.length === 0) return null;
  return (
    <section className="op-category">
      <header className="op-category-head">
        <h3 className="op-category-title">{meta.title}</h3>
        <p className="op-category-blurb">{meta.description}</p>
      </header>
      {descriptors.map((d) => (
        <Fragment key={d.id}>{d.render(ctx)}</Fragment>
      ))}
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

  if (cfgQuery.isLoading) {
    return <div className="app-panel-loading">Loading integrations…</div>;
  }

  const ctx: IntegrationContext = {
    cfg: cfgQuery.data ?? {},
    localStatuses: statusQuery.data ?? [],
  };

  // Group descriptors by category, applying isAvailable at the same pass
  // so unsupported integrations vanish entirely (no empty headers).
  const grouped = new Map<string, IntegrationDescriptor[]>();
  for (const d of INTEGRATIONS) {
    if (!d.isAvailable(ctx)) continue;
    const bucket = grouped.get(d.category) ?? [];
    bucket.push(d);
    grouped.set(d.category, bucket);
  }
  const anyAvailable = grouped.size > 0;

  return (
    <div
      style={{
        maxWidth: 780,
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

      <HelpBanner />

      {INTEGRATION_CATEGORIES.map((meta) => (
        <CategorySection
          key={meta.id}
          meta={meta}
          descriptors={grouped.get(meta.id) ?? []}
          ctx={ctx}
        />
      ))}

      {!anyAvailable && (
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
