import { OpenNewWindow } from "iconoir-react";

import type { LocalProviderStatus } from "../../../api/client";
import type { IntegrationStatus } from "./types";

// HermesCard reads the local-runtime probe to display connectivity. There's
// no token to paste (Hermes auth lives in the gateway's own config); the
// card's job is to surface whether the API server is reachable and to
// point the user at install docs when it isn't.

export function hermesStatus(
  statuses: LocalProviderStatus[],
): IntegrationStatus {
  const hermes = statuses.find((s) => s.kind === "hermes-agent");
  if (hermes?.binary_installed && hermes.reachable) {
    return { tone: "connected", label: "Reachable" };
  }
  if (hermes?.binary_installed) {
    return { tone: "warning", label: "Installed but unreachable" };
  }
  return { tone: "available", label: "Not detected" };
}

export function HermesDetail({
  statuses,
}: {
  statuses: LocalProviderStatus[];
}) {
  const hermes = statuses.find((s) => s.kind === "hermes-agent");
  return (
    <div>
      <p className="op-card-blurb" style={{ marginBottom: 8 }}>
        Connect to a local Hermes gateway's OpenAI-compatible API server on{" "}
        <code style={{ fontFamily: "var(--font-mono)", fontSize: 12 }}>
          {hermes?.endpoint ?? "http://127.0.0.1:8642/v1"}
        </code>
        . When Hermes is running, imported Hermes-controlled agents route their
        turns through this gateway.
      </p>
      {hermes && !hermes.binary_installed && (
        <p className="op-runtime-note">
          Install Hermes locally (see{" "}
          <a
            href="https://hermesagent.com"
            target="_blank"
            rel="noreferrer"
            style={{
              display: "inline-flex",
              alignItems: "center",
              gap: 4,
            }}
          >
            hermesagent.com <OpenNewWindow width={11} height={11} />
          </a>
          ) and start its API server. This card auto-detects when the endpoint
          becomes reachable.
        </p>
      )}
      {hermes?.binary_installed && !hermes.reachable && (
        <p className="op-runtime-note is-warn">
          Hermes binary detected but the API server isn't responding. Start it
          from a terminal and recheck.
        </p>
      )}
    </div>
  );
}
