import type { CSSProperties } from "react";
import type { ApprovalClaim, ApprovalScope } from "@wuphf/protocol";

interface ClaimSummaryProps {
  readonly claim: ApprovalClaim;
  readonly scope: ApprovalScope;
}

interface DetailRow {
  readonly label: string;
  readonly value: string | number;
  readonly mono?: boolean;
}

const panelStyle: CSSProperties = {
  border: "1px solid var(--border)",
  borderRadius: 8,
  background: "var(--bg-card)",
  padding: 16,
};

const titleStyle: CSSProperties = {
  fontSize: 14,
  fontWeight: 700,
  margin: "0 0 4px",
};

const subtitleStyle: CSSProperties = {
  color: "var(--text-secondary)",
  fontSize: 12,
  lineHeight: 1.45,
  margin: "0 0 14px",
};

const gridStyle: CSSProperties = {
  display: "grid",
  gridTemplateColumns: "minmax(120px, 0.38fr) minmax(0, 1fr)",
  gap: "8px 12px",
  fontSize: 12,
};

const labelStyle: CSSProperties = {
  color: "var(--text-tertiary)",
  fontWeight: 600,
};

const valueStyle: CSSProperties = {
  color: "var(--text)",
  wordBreak: "break-word",
};

const monoValueStyle: CSSProperties = {
  ...valueStyle,
  fontFamily: "var(--font-mono)",
  fontSize: 11,
};

export function ClaimSummary({ claim, scope }: ClaimSummaryProps) {
  return (
    <section style={panelStyle} aria-label="Approval payload">
      <h3 style={titleStyle}>{claimKindLabel(claim.kind)}</h3>
      <p style={subtitleStyle}>
        Review the exact signed payload before touching your security key.
      </p>
      <DetailGrid rows={claimRows(claim)} />
      <div style={{ marginTop: 16 }}>
        <h4 style={{ ...titleStyle, fontSize: 12 }}>Scope</h4>
        <DetailGrid rows={scopeRows(scope)} />
      </div>
    </section>
  );
}

function DetailGrid({ rows }: { readonly rows: readonly DetailRow[] }) {
  return (
    <dl style={gridStyle}>
      {rows.map((row) => (
        <FragmentRow key={row.label} row={row} />
      ))}
    </dl>
  );
}

function FragmentRow({ row }: { readonly row: DetailRow }) {
  return (
    <>
      <dt style={labelStyle}>{row.label}</dt>
      <dd style={row.mono ? monoValueStyle : valueStyle}>{row.value}</dd>
    </>
  );
}

function claimRows(claim: ApprovalClaim): readonly DetailRow[] {
  const base = [
    { label: "Claim ID", value: claim.claimId, mono: true },
    { label: "Kind", value: claim.kind, mono: true },
  ];
  switch (claim.kind) {
    case "cost_spike_acknowledgement":
      return [
        ...base,
        { label: "Agent", value: claim.agentId, mono: true },
        { label: "Cost ceiling", value: claim.costCeilingId, mono: true },
        { label: "Threshold", value: `${claim.thresholdBps} bps` },
        { label: "Current cost", value: microUsdLabel(claim.currentMicroUsd) },
        { label: "Ceiling", value: microUsdLabel(claim.ceilingMicroUsd) },
      ];
    case "endpoint_allowlist_extension":
      return [
        ...base,
        { label: "Agent", value: claim.agentId, mono: true },
        { label: "Provider", value: claim.providerKind, mono: true },
        { label: "Endpoint", value: claim.endpointOrigin, mono: true },
        { label: "Reason", value: claim.reason },
      ];
    case "credential_grant_to_agent":
      return [
        ...base,
        { label: "Grantee agent", value: claim.granteeAgentId, mono: true },
        {
          label: "Credential handle",
          value: claim.credentialHandleId,
          mono: true,
        },
        { label: "Credential scope", value: claim.credentialScope, mono: true },
      ];
    case "receipt_co_sign":
      return [
        ...base,
        { label: "Receipt", value: claim.receiptId, mono: true },
        ...(claim.writeId
          ? [{ label: "Write", value: claim.writeId, mono: true }]
          : []),
        { label: "Frozen args hash", value: claim.frozenArgsHash, mono: true },
        { label: "Risk", value: claim.riskClass },
      ];
  }
}

function scopeRows(scope: ApprovalScope): readonly DetailRow[] {
  const base = [
    { label: "Mode", value: scope.mode, mono: true },
    { label: "Role", value: scope.role, mono: true },
    { label: "Max uses", value: scope.maxUses },
    { label: "Claim kind", value: scope.claimKind, mono: true },
  ];
  switch (scope.claimKind) {
    case "cost_spike_acknowledgement":
      return [
        ...base,
        { label: "Agent", value: scope.agentId, mono: true },
        { label: "Cost ceiling", value: scope.costCeilingId, mono: true },
      ];
    case "endpoint_allowlist_extension":
      return [
        ...base,
        { label: "Agent", value: scope.agentId, mono: true },
        { label: "Provider", value: scope.providerKind, mono: true },
        { label: "Endpoint", value: scope.endpointOrigin, mono: true },
      ];
    case "credential_grant_to_agent":
      return [
        ...base,
        { label: "Grantee agent", value: scope.granteeAgentId, mono: true },
        {
          label: "Credential handle",
          value: scope.credentialHandleId,
          mono: true,
        },
      ];
    case "receipt_co_sign":
      return [
        ...base,
        { label: "Receipt", value: scope.receiptId, mono: true },
        ...(scope.writeId
          ? [{ label: "Write", value: scope.writeId, mono: true }]
          : []),
        { label: "Frozen args hash", value: scope.frozenArgsHash, mono: true },
      ];
  }
}

function claimKindLabel(kind: ApprovalClaim["kind"]): string {
  switch (kind) {
    case "cost_spike_acknowledgement":
      return "Cost spike acknowledgement";
    case "endpoint_allowlist_extension":
      return "Endpoint allowlist extension";
    case "credential_grant_to_agent":
      return "Credential grant to agent";
    case "receipt_co_sign":
      return "Receipt co-sign";
  }
}

function microUsdLabel(value: number): string {
  const sign = value < 0 ? "-" : "";
  const absolute = Math.abs(value);
  const dollars = Math.trunc(absolute / 1_000_000);
  const micros = String(absolute % 1_000_000)
    .padStart(6, "0")
    .replace(/0+$/, "");
  return `${sign}$${dollars}${micros ? `.${micros}` : ""}`;
}
