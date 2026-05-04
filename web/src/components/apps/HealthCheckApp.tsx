import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import {
  getHealth,
  getHumanMe,
  getHumanSessions,
  getShareStatus,
  startShare,
  stopShare,
} from "../../api/platform";
import { useAppStore } from "../../stores/app";

function formatSessionTime(value?: string): string {
  if (!value) return "never";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

export function HealthCheckApp() {
  const queryClient = useQueryClient();
  const [inviteCopied, setInviteCopied] = useState(false);
  const [shareMutationError, setShareMutationError] = useState("");
  const brokerConnected = useAppStore((s) => s.brokerConnected);
  const { data, isLoading, error } = useQuery({
    queryKey: ["health"],
    queryFn: () => getHealth(),
    refetchInterval: 10_000,
  });
  const { data: me } = useQuery({
    queryKey: ["humans", "me"],
    queryFn: () => getHumanMe(),
    refetchInterval: 30_000,
  });
  const human = me?.human;
  const isHost = human?.role === "host";
  const { data: humanSessions } = useQuery({
    queryKey: ["humans", "sessions"],
    queryFn: () => getHumanSessions(),
    refetchInterval: 30_000,
    enabled: isHost,
  });
  const { data: shareStatus } = useQuery({
    queryKey: ["share", "status"],
    queryFn: () => getShareStatus(),
    refetchInterval: 10_000,
    enabled: isHost,
  });
  const startShareMutation = useMutation({
    mutationFn: () => startShare(),
    onMutate: () => {
      setShareMutationError("");
    },
    onSuccess: (share) => {
      queryClient.setQueryData(["share", "status"], share);
      setShareMutationError("");
    },
    onError: (err) => {
      setShareMutationError(
        err instanceof Error ? err.message : "Could not create invite.",
      );
    },
  });
  const stopShareMutation = useMutation({
    mutationFn: () => stopShare(),
    onMutate: () => {
      setShareMutationError("");
    },
    onSuccess: (share) => {
      queryClient.setQueryData(["share", "status"], share);
      setShareMutationError("");
    },
    onError: (err) => {
      setShareMutationError(
        err instanceof Error ? err.message : "Could not stop sharing.",
      );
    },
  });

  if (isLoading) {
    return (
      <div
        style={{
          padding: "40px 20px",
          textAlign: "center",
          color: "var(--text-tertiary)",
          fontSize: 14,
        }}
      >
        Checking health...
      </div>
    );
  }

  if (error) {
    return (
      <div
        style={{
          padding: "40px 20px",
          textAlign: "center",
          color: "var(--text-tertiary)",
          fontSize: 14,
        }}
      >
        Could not reach health endpoint.
      </div>
    );
  }

  const status = data?.status ?? "unknown";
  const isHealthy = status === "ok" || status === "healthy";
  const sessions = (humanSessions?.sessions ?? []).filter(
    (session) => !session.revoked_at,
  );
  const ownOrigin =
    typeof window !== "undefined"
      ? window.location.origin
      : "http://localhost:7890";
  const hostname =
    typeof window !== "undefined" ? window.location.hostname : "localhost";
  const remoteTarget =
    hostname === "localhost" || hostname === "127.0.0.1"
      ? "http://<server>:7890"
      : ownOrigin;
  const humanLabel =
    human?.display_name || human?.human_slug || human?.slug || "Host";
  const providerLabel = [data?.provider, data?.provider_model]
    .filter(Boolean)
    .join(" / ");
  const sessionLabel =
    data?.session_mode === "one_on_one" && data.one_on_one_agent
      ? `${data.session_mode} / ${data.one_on_one_agent}`
      : data?.session_mode;
  const memoryLabel = data?.memory_backend_active || data?.memory_backend;
  const shareRunning = Boolean(shareStatus?.running);
  const shareMutationPending =
    startShareMutation.isPending || stopShareMutation.isPending;
  const shareError = shareMutationError || shareStatus?.error;
  const shareInviteURL = shareRunning ? shareStatus?.invite_url || "" : "";
  const shareNetworkLabel = [shareStatus?.interface, shareStatus?.bind]
    .filter(Boolean)
    .join(" / ");
  const startShareInvite = () => {
    if (shareMutationPending) return;
    startShareMutation.mutate();
  };
  const stopShareInvite = () => {
    if (shareMutationPending) return;
    stopShareMutation.mutate();
  };
  const copyInvite = async () => {
    if (!shareInviteURL || typeof navigator === "undefined") return;
    await navigator.clipboard.writeText(shareInviteURL);
    setInviteCopied(true);
    setTimeout(() => setInviteCopied(false), 1600);
  };
  const runtimeItems = [
    {
      label: "Session",
      value: sessionLabel || "unknown",
      active: Boolean(data?.session_mode),
    },
    {
      label: "Provider",
      value: providerLabel || "unknown",
      active: Boolean(data?.provider),
    },
    {
      label: "Memory",
      value: memoryLabel || "none",
      active: Boolean(data?.memory_backend_ready),
    },
    {
      label: "Nex",
      value: data?.nex_connected ? "connected" : "disconnected",
      active: Boolean(data?.nex_connected),
    },
    {
      label: "Build",
      value: data?.build?.version ?? "unknown",
      active: Boolean(data?.build?.version),
    },
  ];

  return (
    <>
      <div
        style={{
          padding: "0 0 12px",
          borderBottom: "1px solid var(--border)",
          marginBottom: 12,
        }}
      >
        <h3 style={{ fontSize: 16, fontWeight: 600, marginBottom: 4 }}>
          Access & Health
        </h3>
      </div>

      <div
        style={{
          display: "grid",
          gridTemplateColumns: "repeat(auto-fit, minmax(220px, 1fr))",
          gap: 10,
          marginBottom: 12,
        }}
      >
        <div className="app-card" style={{ minHeight: 126 }}>
          <div style={{ fontWeight: 700, fontSize: 14, marginBottom: 6 }}>
            This browser
          </div>
          <div className="app-card-meta" style={{ marginBottom: 10 }}>
            Signed in as {humanLabel}
          </div>
          <span
            className={
              brokerConnected ? "badge badge-green" : "badge badge-yellow"
            }
          >
            {brokerConnected ? "Live event stream" : "Reconnecting events"}
          </span>
        </div>

        <div className="app-card" style={{ minHeight: 126 }}>
          <div style={{ fontWeight: 700, fontSize: 14, marginBottom: 6 }}>
            Access for you
          </div>
          <div className="app-card-meta" style={{ marginBottom: 8 }}>
            Keep using the normal WUPHF UI through SSH, LAN, Tailscale, or
            WireGuard.
          </div>
          <code
            style={{
              display: "block",
              padding: "8px 10px",
              borderRadius: 8,
              background: "var(--bg-warm)",
              color: "var(--text)",
              fontSize: 11,
              whiteSpace: "normal",
              wordBreak: "break-word",
            }}
          >
            ssh -L 7890:localhost:7890 user@server
          </code>
          <div className="app-card-meta" style={{ marginTop: 8 }}>
            Then open {remoteTarget}
          </div>
        </div>

        <div className="app-card" style={{ minHeight: 126 }}>
          <div style={{ fontWeight: 700, fontSize: 14, marginBottom: 6 }}>
            Invite a team member
          </div>
          {!isHost ? (
            <div className="app-card-meta">
              Team-member invites are host-only.
            </div>
          ) : (
            <>
              <div className="app-card-meta" style={{ marginBottom: 8 }}>
                Create a one-use private-network invite from this browser.
              </div>
              <div
                style={{
                  display: "flex",
                  gap: 8,
                  flexWrap: "wrap",
                  marginBottom: 8,
                }}
              >
                <button
                  className="btn btn-primary btn-sm"
                  type="button"
                  onClick={startShareInvite}
                  disabled={shareMutationPending}
                >
                  {shareRunning ? "Create new invite" : "Create invite"}
                </button>
                {shareRunning ? (
                  <button
                    className="btn btn-secondary btn-sm"
                    type="button"
                    onClick={stopShareInvite}
                    disabled={shareMutationPending}
                  >
                    Stop sharing
                  </button>
                ) : null}
              </div>
              {shareInviteURL ? (
                <div
                  style={{
                    display: "grid",
                    gridTemplateColumns: "1fr auto",
                    gap: 8,
                    alignItems: "center",
                  }}
                >
                  <code
                    style={{
                      display: "block",
                      padding: "8px 10px",
                      borderRadius: 8,
                      background: "var(--bg-warm)",
                      color: "var(--text)",
                      fontSize: 11,
                      whiteSpace: "normal",
                      wordBreak: "break-word",
                    }}
                  >
                    {shareInviteURL}
                  </code>
                  <button
                    className="btn btn-secondary btn-sm"
                    type="button"
                    onClick={() => void copyInvite()}
                  >
                    {inviteCopied ? "Copied" : "Copy"}
                  </button>
                </div>
              ) : null}
              {shareRunning && shareNetworkLabel ? (
                <div className="app-card-meta" style={{ marginTop: 8 }}>
                  Sharing on {shareNetworkLabel}
                </div>
              ) : null}
              {shareRunning && shareStatus?.expires_at ? (
                <div className="app-card-meta" style={{ marginTop: 4 }}>
                  Invite expires {formatSessionTime(shareStatus.expires_at)}
                </div>
              ) : null}
              {shareError ? (
                <div
                  style={{
                    marginTop: 8,
                    color: "var(--danger, #b42318)",
                    fontSize: 12,
                    lineHeight: 1.4,
                    whiteSpace: "pre-wrap",
                  }}
                >
                  {shareError}
                </div>
              ) : null}
            </>
          )}
        </div>
      </div>

      {/* Overall status */}
      <div
        className="app-card"
        style={{
          display: "flex",
          alignItems: "center",
          gap: 10,
          marginBottom: 12,
        }}
      >
        <span
          className={`status-dot ${isHealthy ? "active" : ""}`}
          style={{ width: 10, height: 10 }}
        />
        <div>
          <div style={{ fontWeight: 600, fontSize: 14 }}>Broker Status</div>
          <div className="app-card-meta">
            <span
              className={isHealthy ? "badge badge-green" : "badge badge-yellow"}
            >
              {status.toUpperCase()}
            </span>
          </div>
        </div>
      </div>

      <div
        style={{
          fontSize: 11,
          fontWeight: 600,
          textTransform: "uppercase",
          letterSpacing: "0.05em",
          color: "var(--text-tertiary)",
          padding: "8px 0 6px",
        }}
      >
        Team-member sessions ({sessions.length})
      </div>
      {!isHost ? (
        <div
          className="app-card"
          style={{
            marginBottom: 12,
            color: "var(--text-tertiary)",
            fontSize: 13,
          }}
        >
          Team-member session visibility is host-only.
        </div>
      ) : sessions.length > 0 ? (
        sessions.map((session) => (
          <div
            key={session.id}
            className="app-card"
            style={{
              marginBottom: 6,
              display: "flex",
              alignItems: "center",
              gap: 8,
            }}
          >
            <span className="status-dot active" />
            <div style={{ flex: 1, minWidth: 0 }}>
              <div style={{ fontWeight: 500, fontSize: 13 }}>
                {session.display_name || session.human_slug}
              </div>
              <div className="app-card-meta">
                Last seen {formatSessionTime(session.last_seen_at)} · expires{" "}
                {formatSessionTime(session.expires_at)}
              </div>
            </div>
          </div>
        ))
      ) : (
        <div
          className="app-card"
          style={{
            marginBottom: 12,
            color: "var(--text-tertiary)",
            fontSize: 13,
          }}
        >
          No active team-member browser sessions.
        </div>
      )}

      <div
        style={{
          fontSize: 11,
          fontWeight: 600,
          textTransform: "uppercase",
          letterSpacing: "0.05em",
          color: "var(--text-tertiary)",
          padding: "8px 0 6px",
        }}
      >
        Runtime
      </div>
      {runtimeItems.map((item) => (
        <div
          key={item.label}
          className="app-card"
          style={{
            marginBottom: 6,
            display: "flex",
            alignItems: "center",
            gap: 8,
          }}
        >
          <span className={`status-dot ${item.active ? "active" : ""}`} />
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ fontWeight: 500, fontSize: 13 }}>{item.label}</div>
            <div
              className="app-card-meta"
              style={{
                overflow: "hidden",
                textOverflow: "ellipsis",
                whiteSpace: "nowrap",
              }}
            >
              {item.value}
            </div>
          </div>
        </div>
      ))}

      {data?.focus_mode ? (
        <div
          className="app-card"
          style={{
            marginTop: 12,
            display: "flex",
            alignItems: "center",
            gap: 8,
          }}
        >
          <span className="status-dot active" />
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ fontWeight: 500, fontSize: 13 }}>Focus Mode</div>
            <div className="app-card-meta">enabled</div>
          </div>
        </div>
      ) : null}
    </>
  );
}
