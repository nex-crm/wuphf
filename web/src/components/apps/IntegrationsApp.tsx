import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Lock, OpenNewWindow, WarningTriangle } from "iconoir-react";

import {
  type ConfigSnapshot,
  type ConfigUpdate,
  type GatewayKind,
  getConfig,
  getLocalProvidersStatus,
  type LocalProviderStatus,
  updateConfig,
} from "../../api/client";
import { useAppStore } from "../../stores/app";
import { showNotice } from "../ui/Toast";

// IntegrationsApp is the home for gateway-style connections — surfaces that
// import existing agents or external messaging streams into the team rather
// than backing a WUPHF-created agent's turns. Three first-class cards today:
//
//   - OpenClaw: bridges OpenClaw-controlled sessions into the office. Tokens
//     and gateway URL live here, not in Settings, so the configuration sits
//     next to the import action.
//   - Hermes: connects a local Hermes gateway's /v1 API. Status is read from
//     the same loopback probe Local LLMs used to use, but framed as
//     gateway-connected rather than "runtime available."
//   - Telegram: paste a bot token, pick a chat, get a channel. Wraps the
//     existing TelegramConnectModal so this app is the single place a user
//     goes to add an integration.
//
// New gateway types should each get their own GatewayCard — they should not
// be folded into the global LLM provider picker.

interface CardShellProps {
  icon: React.ReactNode;
  title: string;
  status: "connected" | "available" | "unconfigured" | "warning";
  statusLabel: string;
  body: React.ReactNode;
}

function statusColor(status: CardShellProps["status"]): string {
  switch (status) {
    case "connected":
      return "#16a34a";
    case "available":
      return "#3b82f6";
    case "warning":
      return "#d97706";
    default:
      return "var(--text-tertiary)";
  }
}

function CardShell({ icon, title, status, statusLabel, body }: CardShellProps) {
  return (
    <section
      style={{
        border: "1px solid var(--border)",
        borderRadius: 8,
        padding: 16,
        marginBottom: 16,
        background: "var(--bg-card)",
      }}
    >
      <header
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          marginBottom: 12,
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
          <span style={{ fontSize: 22 }}>{icon}</span>
          <h3 style={{ margin: 0, fontSize: 15, fontWeight: 600 }}>{title}</h3>
        </div>
        <span
          style={{
            display: "inline-flex",
            alignItems: "center",
            gap: 6,
            fontSize: 11,
            color: statusColor(status),
            fontWeight: 600,
            textTransform: "uppercase",
            letterSpacing: 0.4,
          }}
        >
          <span
            style={{
              width: 8,
              height: 8,
              borderRadius: "50%",
              background: statusColor(status),
              display: "inline-block",
            }}
          />
          {statusLabel}
        </span>
      </header>
      <div>{body}</div>
    </section>
  );
}

function OpenClawCard({ cfg }: { cfg: ConfigSnapshot }) {
  const queryClient = useQueryClient();
  const [gatewayUrl, setGatewayUrl] = useState(cfg.openclaw_gateway_url ?? "");
  const [token, setToken] = useState("");
  const [revealToken, setRevealToken] = useState(false);
  const tokenSet = Boolean(cfg.openclaw_token_set);
  const urlSet = Boolean(cfg.openclaw_gateway_url);
  const connected = tokenSet && urlSet;

  const mutation = useMutation({
    mutationFn: async () => {
      const patch: ConfigUpdate = {};
      if (gatewayUrl.trim()) patch.openclaw_gateway_url = gatewayUrl.trim();
      if (token.trim()) patch.openclaw_token = token.trim();
      await updateConfig(patch);
    },
    onSuccess: () => {
      showNotice("OpenClaw connection saved.", "success");
      setToken("");
      void queryClient.invalidateQueries({ queryKey: ["config"] });
    },
    onError: (err) => {
      showNotice(
        err instanceof Error ? err.message : "Failed to save OpenClaw",
        "error",
      );
    },
  });

  return (
    <CardShell
      icon={<span aria-hidden="true">🔌</span>}
      title="OpenClaw"
      status={connected ? "connected" : "unconfigured"}
      statusLabel={connected ? "Connected" : "Not configured"}
      body={
        <div>
          <p
            style={{
              margin: "0 0 12px 0",
              fontSize: 13,
              color: "var(--text-secondary)",
            }}
          >
            Bridge OpenClaw-controlled agents into the team. Provide your
            gateway's WebSocket URL and an auth token; new OpenClaw agents can
            then be onboarded from the gateway's session list.
          </p>
          <label
            style={{
              display: "block",
              fontSize: 11,
              fontWeight: 600,
              marginBottom: 4,
              color: "var(--text-secondary)",
              textTransform: "uppercase",
              letterSpacing: 0.4,
            }}
          >
            Gateway URL
          </label>
          <input
            className="input"
            type="text"
            placeholder="ws://127.0.0.1:18789"
            value={gatewayUrl}
            onChange={(e) => setGatewayUrl(e.target.value)}
            style={{
              width: "100%",
              marginBottom: 10,
              fontFamily: "var(--font-mono)",
            }}
          />
          <label
            style={{
              display: "block",
              fontSize: 11,
              fontWeight: 600,
              marginBottom: 4,
              color: "var(--text-secondary)",
              textTransform: "uppercase",
              letterSpacing: 0.4,
            }}
          >
            Token{" "}
            {tokenSet && !token ? (
              <span
                style={{
                  fontWeight: 400,
                  textTransform: "none",
                  letterSpacing: 0,
                  color: "var(--text-tertiary)",
                }}
              >
                (saved · paste to rotate)
              </span>
            ) : null}
          </label>
          <input
            className="input"
            type={revealToken ? "text" : "password"}
            placeholder={tokenSet ? "●●●●●●●●" : "oc_..."}
            value={token}
            onChange={(e) => setToken(e.target.value)}
            style={{ width: "100%", marginBottom: 6 }}
          />
          <label
            style={{
              display: "inline-flex",
              alignItems: "center",
              gap: 4,
              fontSize: 11,
              color: "var(--text-tertiary)",
              cursor: "pointer",
              marginBottom: 14,
            }}
          >
            <input
              type="checkbox"
              checked={revealToken}
              onChange={(e) => setRevealToken(e.target.checked)}
            />
            Show token
          </label>
          <div style={{ display: "flex", gap: 6 }}>
            <button
              type="button"
              className="btn btn-primary btn-sm"
              disabled={
                mutation.isPending ||
                !(gatewayUrl.trim() || token.trim()) ||
                (connected &&
                  !token.trim() &&
                  gatewayUrl === (cfg.openclaw_gateway_url ?? ""))
              }
              onClick={() => mutation.mutate()}
            >
              {mutation.isPending
                ? "Saving..."
                : connected
                  ? "Update connection"
                  : "Connect"}
            </button>
          </div>
        </div>
      }
    />
  );
}

function HermesCard({ statuses }: { statuses: LocalProviderStatus[] }) {
  const hermes = statuses.find((s) => s.kind === "hermes-agent");
  let status: CardShellProps["status"] = "available";
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
      icon={<span aria-hidden="true">🔌</span>}
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

function TelegramCard({ cfg }: { cfg: ConfigSnapshot }) {
  const openConnectWizard = useAppStore((s) => s.openConnectWizard);
  const tokenSet = Boolean(cfg.telegram_token_set);
  return (
    <CardShell
      icon={<span aria-hidden="true">🔌</span>}
      title="Telegram"
      status={tokenSet ? "connected" : "unconfigured"}
      statusLabel={tokenSet ? "Bot connected" : "Not configured"}
      body={
        <div>
          <p
            style={{
              margin: "0 0 12px 0",
              fontSize: 13,
              color: "var(--text-secondary)",
            }}
          >
            Bring a Telegram chat into the office as a channel. Paste a bot
            token, pick the chat, and replies route through the bot.
          </p>
          <button
            type="button"
            className="btn btn-primary btn-sm"
            onClick={() => openConnectWizard("telegram")}
          >
            {tokenSet ? "Connect another chat" : "Connect Telegram"}
          </button>
        </div>
      }
    />
  );
}

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
        OpenClaw and Hermes are <strong>gateways</strong> — they import existing
        agents into the team. They are not LLM runtimes you can pick in{" "}
        <em>Settings → Default runtime</em>. Once an agent is imported, the
        agent profile shows a "Managed by &lt;Gateway&gt;" badge in its runtime
        section.
      </div>
    </div>
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
  const cfg = cfgQuery.data ?? {};
  const statuses = statusQuery.data ?? [];

  // gateway_kinds tells the UI which gateway runtimes are compiled in. If a
  // gateway isn't registered on the Go side we hide its card entirely — the
  // backend can't dispatch through it, so offering Connect would be a lie.
  const gateways: GatewayKind[] = (cfg.gateway_kinds ?? [
    "openclaw",
    "hermes-agent",
  ]) as GatewayKind[];
  const hasOpenClaw =
    gateways.includes("openclaw") || gateways.includes("openclaw-http");
  const hasHermes = gateways.includes("hermes-agent");

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
        Bring agents and messaging streams in from outside the team. Configure
        gateways here; pick LLM runtimes in Settings.
      </p>

      <HelpBanner />

      {hasOpenClaw && <OpenClawCard cfg={cfg} />}
      {hasHermes && <HermesCard statuses={statuses} />}
      <TelegramCard cfg={cfg} />

      {!(hasOpenClaw || hasHermes) && (
        <p
          style={{ marginTop: 12, fontSize: 12, color: "var(--text-tertiary)" }}
        >
          <WarningTriangle
            width={12}
            height={12}
            style={{ marginRight: 4, verticalAlign: "text-bottom" }}
          />
          No OpenClaw or Hermes runtime is registered in this build.
        </p>
      )}
    </div>
  );
}
