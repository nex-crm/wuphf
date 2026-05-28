import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";

import {
  type ConfigSnapshot,
  type ConfigUpdate,
  updateConfig,
} from "../../../api/client";
import { showNotice } from "../../ui/Toast";
import { CardShell } from "./CardShell";

// OpenClawCard owns the gateway-URL + token form. Connection status is
// derived from the /config snapshot: an OpenClaw is "Connected" when both
// the URL and the token are saved; either-or stays "Not configured" so the
// status badge never lies about a half-set state.
export function OpenClawCard({ cfg }: { cfg: ConfigSnapshot }) {
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

  const disableSubmit =
    mutation.isPending ||
    !(gatewayUrl.trim() || token.trim()) ||
    (connected &&
      !token.trim() &&
      gatewayUrl === (cfg.openclaw_gateway_url ?? ""));

  return (
    <CardShell
      icon={<span aria-hidden="true">🦾</span>}
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
              disabled={disableSubmit}
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
