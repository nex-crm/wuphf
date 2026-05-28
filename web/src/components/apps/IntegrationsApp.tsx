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
    <div
      style={{
        display: "flex",
        gap: 10,
        alignItems: "flex-start",
        background: "var(--bg-muted, rgba(0,0,0,0.03))",
        border: "1px solid var(--border)",
        borderRadius: 6,
        padding: "10px 12px",
        marginBottom: 18,
      }}
    >
      <Lock
        width={16}
        height={16}
        style={{ marginTop: 2, color: "var(--text-tertiary)", flexShrink: 0 }}
      />
      <div
        style={{
          fontSize: 12,
          color: "var(--text-secondary)",
          lineHeight: 1.5,
        }}
      >
        Integrations are <strong>gateways</strong> for bringing agents or
        messaging streams into the team from outside. They are not LLM runtimes
        — pick those in <em>Settings → Default runtime</em>. Agent-importing
        gateways (External Agents) tag their imported agents with a "Managed by
        &lt;Gateway&gt;" badge in the agent profile.
      </div>
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
    <section style={{ marginBottom: 28 }}>
      <header style={{ marginBottom: 10 }}>
        <h3
          style={{
            margin: "0 0 4px 0",
            fontSize: 13,
            fontWeight: 700,
            color: "var(--text-secondary)",
            textTransform: "uppercase",
            letterSpacing: 0.6,
          }}
        >
          {meta.title}
        </h3>
        <p
          style={{
            margin: 0,
            fontSize: 12,
            color: "var(--text-tertiary)",
            lineHeight: 1.5,
          }}
        >
          {meta.description}
        </p>
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
        maxWidth: 760,
        margin: "0 auto",
        padding: "24px 24px 48px 24px",
      }}
    >
      <h2 style={{ margin: "0 0 6px 0", fontSize: 20, fontWeight: 700 }}>
        Integrations
      </h2>
      <p
        style={{
          margin: "0 0 18px 0",
          fontSize: 13,
          color: "var(--text-tertiary)",
        }}
      >
        Bring agents and messaging streams in from outside the team.
      </p>

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
