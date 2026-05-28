import { OpenNewWindow } from "iconoir-react";

import type { LocalProviderStatus } from "../../../api/client";
import { CardShell, type CardStatus } from "./CardShell";

// HermesCard reads the local-runtime probe to display connectivity. There's
// no token to paste (Hermes auth lives in the gateway's own config); the
// card's job is to surface whether the API server is reachable and to
// point the user at install docs when it isn't.
export function HermesCard({ statuses }: { statuses: LocalProviderStatus[] }) {
  const hermes = statuses.find((s) => s.kind === "hermes-agent");
  let status: CardStatus = "available";
  let statusLabel = "Not detected";
  if (hermes?.binary_installed && hermes.reachable) {
    status = "connected";
    statusLabel = "Reachable";
  } else if (hermes?.binary_installed) {
    status = "warning";
    statusLabel = "Installed but unreachable";
  }

  return (
    <CardShell
      icon={<span aria-hidden="true">🪽</span>}
      title="Hermes"
      status={status}
      statusLabel={statusLabel}
      body={
        <div>
          <p
            style={{
              margin: "0 0 10px 0",
              fontSize: 13,
              color: "var(--text-secondary)",
            }}
          >
            Connect to a local Hermes gateway's OpenAI-compatible API server on{" "}
            <code style={{ fontFamily: "var(--font-mono)", fontSize: 12 }}>
              {hermes?.endpoint ?? "http://127.0.0.1:8642/v1"}
            </code>
            . When Hermes is running, imported Hermes-controlled agents route
            their turns through this gateway.
          </p>
          {hermes && !hermes.binary_installed && (
            <p
              style={{ margin: 0, fontSize: 12, color: "var(--text-tertiary)" }}
            >
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
              ) and start its API server. This card auto-detects when the
              endpoint becomes reachable.
            </p>
          )}
          {hermes?.binary_installed && !hermes.reachable && (
            <p style={{ margin: 0, fontSize: 12, color: "#d97706" }}>
              Hermes binary detected but the API server isn't responding. Start
              it from a terminal and recheck.
            </p>
          )}
        </div>
      }
    />
  );
}
