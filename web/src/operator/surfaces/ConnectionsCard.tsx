// The inline "this tool needs these connections" card the build chat shows after
// a plan lands. For each integration the workflow references that isn't ready it
// reuses WUPHF's EXISTING connect flow — the same human-interview connect card
// used everywhere else (ConnectIntegrationCard). That card handles BOTH gates in
// order: the Composio account CLI sign-in (install the CLI + OAuth) and then the
// per-integration OAuth, with the broker auto-answering when the connection goes
// live. We do not reimplement any of it here; we only raise the card with a
// synthetic connect request per referenced integration.
//
// "unavailable" (not in the Composio catalog) or a skipped card drops to the
// browser-setup fallback (browser automation is not built yet).

import { useState } from "react";

import type { AgentRequest } from "../../api/client";
import { ConnectIntegrationCard } from "../../components/messages/ConnectIntegrationCard";
import type { ReferencedIntegration } from "../builder/integrationStatus";

interface ConnectionsCardProps {
  integrations: ReferencedIntegration[];
}

export function ConnectionsCard({ integrations }: ConnectionsCardProps) {
  const pending = integrations.filter((i) => i.readiness !== "connected");
  if (pending.length === 0) return null;
  return (
    <div className="opr-conn-card">
      <p className="opr-conn-lead">
        This tool needs{" "}
        {pending.length === 1 ? "an integration" : "integrations"} you have not
        connected yet.
      </p>
      <ul className="opr-conn-list">
        {pending.map((integration) => (
          <ConnectionRow
            key={integration.name.toLowerCase()}
            integration={integration}
          />
        ))}
      </ul>
    </div>
  );
}

// A synthetic `connect` request so the existing ConnectIntegrationCard can drive
// the real flow. The card only needs platform (+ a name/question for display);
// it never answers a real broker request, so a local id is fine.
function toConnectRequest(integration: ReferencedIntegration): AgentRequest {
  const platform = integration.platform ?? "";
  return {
    id: `operator-connect-${(platform || integration.name).toLowerCase()}`,
    from: "your AI",
    question: `Connect ${integration.name} so this tool can use it.`,
    title: `Connect ${integration.name}`,
    platform,
    kind: "connect",
  };
}

function ConnectionRow({
  integration,
}: {
  integration: ReferencedIntegration;
}) {
  const [fellBack, setFellBack] = useState(
    integration.readiness === "unavailable",
  );

  if (fellBack) {
    return (
      <li className="opr-conn-row">
        <span className="opr-conn-name">{integration.name}</span>
        <span className="opr-conn-fallback">
          I can set this up in your browser instead — coming soon.
        </span>
      </li>
    );
  }

  return (
    <li className="opr-conn-row is-connect-card">
      <ConnectIntegrationCard
        request={toConnectRequest(integration)}
        submitting={false}
        onSkip={() => setFellBack(true)}
        onDismiss={() => setFellBack(true)}
      />
    </li>
  );
}
