// Integrations — the apps a tool can read from and act on. Capture discovers
// which apps the operator uses; Composio is the preferred way to connect them.
// Mock data only.

import {
  Eyebrow,
  IntegrationStatusPill,
  sigil,
  SurfaceHeader,
} from "../components/primitives";
import { INTEGRATIONS, type Integration } from "../mock/data";

export function IntegrationsSurface() {
  const connected = INTEGRATIONS.filter((i) => i.status === "connected");
  const rest = INTEGRATIONS.filter((i) => i.status !== "connected");

  return (
    <div className="opr-surface-wide">
      <SurfaceHeader
        eyebrow="Integrations"
        title="Connected apps"
        lede="Your tools read from and act on these. When a tool needs an app you haven't connected, your AI asks you to connect it here."
      />

      <div className="opr-section-label">
        <Eyebrow>Connected</Eyebrow>
        <div className="opr-section-rule" />
      </div>
      <div className="opr-int-grid">
        {connected.map((i) => (
          <IntegrationCard key={i.id} integration={i} />
        ))}
      </div>

      <div className="opr-section-label">
        <Eyebrow>Available</Eyebrow>
        <div className="opr-section-rule" />
      </div>
      <div className="opr-int-grid">
        {rest.map((i) => (
          <IntegrationCard key={i.id} integration={i} />
        ))}
      </div>
    </div>
  );
}

function IntegrationCard({ integration }: { integration: Integration }) {
  const connected = integration.status === "connected";
  const needsAttn = integration.status === "needs-attention";
  return (
    <div className="opr-int-card">
      <span className="opr-int-emoji" aria-hidden>
        {sigil(integration.name)}
      </span>
      <div style={{ flex: 1, minWidth: 0 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
          <span style={{ fontWeight: 600 }}>{integration.name}</span>
          <IntegrationStatusPill status={integration.status} />
        </div>
        <div className="opr-cell-sub" style={{ marginTop: 2 }}>
          {integration.detail}
        </div>
      </div>
      {connected ? null : (
        <button
          type="button"
          className={`opr-btn opr-btn-sm${needsAttn ? " opr-btn-primary" : ""}`}
        >
          {needsAttn ? "Reconnect" : "Connect"}
        </button>
      )}
    </div>
  );
}
