import { type CSSProperties, type ReactNode, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import {
  getHealth,
  getHumanMe,
  getHumanSessions,
  getShareStatus,
  type HealthResponse,
  type HumanMe,
  type HumanSession,
  revokeHumanSession,
  startShare,
  stopShare,
  type WebShareStatus,
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

export function selfAccessDetails(hostname: string, origin: string) {
  const normalizedHost = hostname.trim().toLowerCase();
  if (normalizedHost === "localhost" || normalizedHost === "127.0.0.1") {
    return {
      detail:
        "For a server you reach through SSH, keep the tunnel open while you work.",
      code: "ssh -L 7890:localhost:7890 user@server",
      footer: "Then open http://localhost:7890",
    };
  }
  return {
    detail: "This browser is already connected through the network web UI.",
    code: origin,
    footer: "Use team-member invites for scoped shared sessions.",
  };
}

type RuntimeItem = {
  label: string;
  value: string;
  active: boolean;
};

function SectionLabel({ children }: { children: ReactNode }) {
  return (
    <div
      style={{
        fontSize: 11,
        fontWeight: 600,
        textTransform: "uppercase",
        letterSpacing: 0,
        color: "var(--text-tertiary)",
        padding: "8px 0 6px",
      }}
    >
      {children}
    </div>
  );
}

function LoadingState({ children }: { children: string }) {
  return (
    <div
      style={{
        padding: "40px 20px",
        textAlign: "center",
        color: "var(--text-tertiary)",
        fontSize: 14,
      }}
    >
      {children}
    </div>
  );
}

function AccessCards({
  brokerConnected,
  humanLabel,
  inviteCopied,
  isHost,
  selfAccess,
  shareError,
  shareInviteURL,
  shareMutationPending,
  shareNetworkLabel,
  shareRunning,
  shareStatus,
  onCopyInvite,
  onStartShareInvite,
  onStopShareInvite,
}: {
  brokerConnected: boolean;
  humanLabel: string;
  inviteCopied: boolean;
  isHost: boolean;
  selfAccess: ReturnType<typeof selfAccessDetails>;
  shareError?: string;
  shareInviteURL: string;
  shareMutationPending: boolean;
  shareNetworkLabel: string;
  shareRunning: boolean;
  shareStatus?: WebShareStatus;
  onCopyInvite: () => void;
  onStartShareInvite: () => void;
  onStopShareInvite: () => void;
}) {
  return (
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

      <SelfAccessCard selfAccess={selfAccess} />
      <TeamInviteCard
        inviteCopied={inviteCopied}
        isHost={isHost}
        shareError={shareError}
        shareInviteURL={shareInviteURL}
        shareMutationPending={shareMutationPending}
        shareNetworkLabel={shareNetworkLabel}
        shareRunning={shareRunning}
        shareStatus={shareStatus}
        onCopyInvite={onCopyInvite}
        onStartShareInvite={onStartShareInvite}
        onStopShareInvite={onStopShareInvite}
      />
    </div>
  );
}

function SelfAccessCard({
  selfAccess,
}: {
  selfAccess: ReturnType<typeof selfAccessDetails>;
}) {
  return (
    <div className="app-card" style={{ minHeight: 126 }}>
      <div style={{ fontWeight: 700, fontSize: 14, marginBottom: 6 }}>
        Access for you
      </div>
      <div className="app-card-meta" style={{ marginBottom: 8 }}>
        {selfAccess.detail}
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
        {selfAccess.code}
      </code>
      <div className="app-card-meta" style={{ marginTop: 8 }}>
        {selfAccess.footer}
      </div>
    </div>
  );
}

function TeamInviteCard({
  inviteCopied,
  isHost,
  shareError,
  shareInviteURL,
  shareMutationPending,
  shareNetworkLabel,
  shareRunning,
  shareStatus,
  onCopyInvite,
  onStartShareInvite,
  onStopShareInvite,
}: {
  inviteCopied: boolean;
  isHost: boolean;
  shareError?: string;
  shareInviteURL: string;
  shareMutationPending: boolean;
  shareNetworkLabel: string;
  shareRunning: boolean;
  shareStatus?: WebShareStatus;
  onCopyInvite: () => void;
  onStartShareInvite: () => void;
  onStopShareInvite: () => void;
}) {
  return (
    <div className="app-card" style={{ minHeight: 126 }}>
      <div style={{ fontWeight: 700, fontSize: 14, marginBottom: 6 }}>
        Invite a team member
      </div>
      {!isHost ? (
        <div className="app-card-meta">Team-member invites are host-only.</div>
      ) : (
        <HostInviteControls
          inviteCopied={inviteCopied}
          shareError={shareError}
          shareInviteURL={shareInviteURL}
          shareMutationPending={shareMutationPending}
          shareNetworkLabel={shareNetworkLabel}
          shareRunning={shareRunning}
          shareStatus={shareStatus}
          onCopyInvite={onCopyInvite}
          onStartShareInvite={onStartShareInvite}
          onStopShareInvite={onStopShareInvite}
        />
      )}
    </div>
  );
}

function HostInviteControls({
  inviteCopied,
  shareError,
  shareInviteURL,
  shareMutationPending,
  shareNetworkLabel,
  shareRunning,
  shareStatus,
  onCopyInvite,
  onStartShareInvite,
  onStopShareInvite,
}: {
  inviteCopied: boolean;
  shareError?: string;
  shareInviteURL: string;
  shareMutationPending: boolean;
  shareNetworkLabel: string;
  shareRunning: boolean;
  shareStatus?: WebShareStatus;
  onCopyInvite: () => void;
  onStartShareInvite: () => void;
  onStopShareInvite: () => void;
}) {
  return (
    <>
      <div className="app-card-meta" style={{ marginBottom: 8 }}>
        Create a one-use private-network invite from this browser.
      </div>
      <div
        style={{ display: "flex", gap: 8, flexWrap: "wrap", marginBottom: 8 }}
      >
        <button
          className="btn btn-primary btn-sm"
          type="button"
          onClick={onStartShareInvite}
          disabled={shareMutationPending}
        >
          {shareRunning ? "Create new invite" : "Create invite"}
        </button>
        {shareRunning ? (
          <button
            className="btn btn-secondary btn-sm"
            type="button"
            onClick={onStopShareInvite}
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
            onClick={onCopyInvite}
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
  );
}

function BrokerStatusCard({
  isHealthy,
  status,
}: {
  isHealthy: boolean;
  status: string;
}) {
  return (
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
  );
}

function TeamMemberSessions({
  isHost,
  isRevokingSession,
  onRevokeSession,
  revokeError,
  revokingSessionID,
  sessions,
}: {
  isHost: boolean;
  isRevokingSession: boolean;
  onRevokeSession: (sessionID: string) => void;
  revokeError?: string;
  revokingSessionID?: string;
  sessions: HumanSession[];
}) {
  return (
    <>
      <SectionLabel>Team-member sessions ({sessions.length})</SectionLabel>
      {revokeError ? (
        <div
          style={{
            marginBottom: 8,
            color: "var(--danger, #b42318)",
            fontSize: 12,
            lineHeight: 1.4,
          }}
        >
          {revokeError}
        </div>
      ) : null}
      {!isHost ? (
        <EmptyCard>Team-member session visibility is host-only.</EmptyCard>
      ) : sessions.length > 0 ? (
        sessions.map((session) => {
          const isThisSessionRevoking =
            isRevokingSession && revokingSessionID === session.id;
          return (
            <StatusRow
              key={session.id}
              action={
                <button
                  aria-label={`Disconnect ${session.display_name || session.human_slug}`}
                  className="btn btn-secondary btn-sm"
                  type="button"
                  onClick={() => onRevokeSession(session.id)}
                  disabled={isThisSessionRevoking}
                >
                  {isThisSessionRevoking ? "Disconnecting" : "Disconnect"}
                </button>
              }
              active={true}
              label={session.display_name || session.human_slug}
              value={`Last seen ${formatSessionTime(session.last_seen_at)} · expires ${formatSessionTime(session.expires_at)}`}
            />
          );
        })
      ) : (
        <EmptyCard>No active team-member browser sessions.</EmptyCard>
      )}
    </>
  );
}

function RuntimeStatusList({
  focusMode,
  items,
}: {
  focusMode?: boolean;
  items: RuntimeItem[];
}) {
  return (
    <>
      <SectionLabel>Runtime</SectionLabel>
      {items.map((item) => (
        <StatusRow
          key={item.label}
          active={item.active}
          label={item.label}
          value={item.value}
        />
      ))}
      {focusMode ? (
        <StatusRow
          active={true}
          label="Focus Mode"
          value="enabled"
          style={{ marginTop: 12 }}
        />
      ) : null}
    </>
  );
}

function StatusRow({
  active,
  action,
  label,
  value,
  style,
}: {
  active: boolean;
  action?: ReactNode;
  label: string;
  value: string;
  style?: CSSProperties;
}) {
  return (
    <div
      className="app-card"
      style={{
        marginBottom: 6,
        display: "flex",
        alignItems: "center",
        gap: 8,
        ...style,
      }}
    >
      <span className={`status-dot ${active ? "active" : ""}`} />
      <div style={{ flex: 1, minWidth: 0 }}>
        <div style={{ fontWeight: 500, fontSize: 13 }}>{label}</div>
        <div
          className="app-card-meta"
          style={{
            overflow: "hidden",
            textOverflow: "ellipsis",
            whiteSpace: "nowrap",
          }}
        >
          {value}
        </div>
      </div>
      {action ? <div style={{ flexShrink: 0 }}>{action}</div> : null}
    </div>
  );
}

function EmptyCard({ children }: { children: string }) {
  return (
    <div
      className="app-card"
      style={{
        marginBottom: 12,
        color: "var(--text-tertiary)",
        fontSize: 13,
      }}
    >
      {children}
    </div>
  );
}

function humanDisplayName(human: HumanMe["human"] | undefined): string {
  return human?.display_name || human?.human_slug || human?.slug || "Host";
}

function runtimeItems(data: HealthResponse | undefined): RuntimeItem[] {
  const providerLabel = [data?.provider, data?.provider_model]
    .filter(Boolean)
    .join(" / ");
  const sessionLabel =
    data?.session_mode === "one_on_one" && data.one_on_one_agent
      ? `${data.session_mode} / ${data.one_on_one_agent}`
      : data?.session_mode;
  const memoryLabel = data?.memory_backend_active || data?.memory_backend;
  return [
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
}

export function HealthCheckApp() {
  const queryClient = useQueryClient();
  const [inviteCopied, setInviteCopied] = useState(false);
  const [shareMutationError, setShareMutationError] = useState("");
  const [revokeSessionError, setRevokeSessionError] = useState("");
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
  const revokeSessionMutation = useMutation({
    mutationFn: (sessionID: string) => revokeHumanSession(sessionID),
    onMutate: () => {
      setRevokeSessionError("");
    },
    onSuccess: (_result, sessionID) => {
      queryClient.setQueryData<{ sessions?: HumanSession[] }>(
        ["humans", "sessions"],
        (current) => ({
          sessions: (current?.sessions ?? []).filter(
            (session) => session.id !== sessionID,
          ),
        }),
      );
      setRevokeSessionError("");
    },
    onError: (err) => {
      setRevokeSessionError(
        err instanceof Error ? err.message : "Could not disconnect session.",
      );
    },
  });

  if (isLoading) {
    return <LoadingState>Checking health...</LoadingState>;
  }

  if (error) {
    return <LoadingState>Could not reach health endpoint.</LoadingState>;
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
  const selfAccess = selfAccessDetails(hostname, ownOrigin);
  const humanLabel = humanDisplayName(human);
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
    try {
      await navigator.clipboard.writeText(shareInviteURL);
      setShareMutationError("");
      setInviteCopied(true);
      setTimeout(() => setInviteCopied(false), 1600);
    } catch (err) {
      console.error("Could not copy share invite URL", err);
      setShareMutationError(
        "Could not copy invite. Copy it manually from the field.",
      );
    }
  };
  const revokeSession = (sessionID: string) => {
    if (revokeSessionMutation.isPending) return;
    revokeSessionMutation.mutate(sessionID);
  };
  const items = runtimeItems(data);

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

      <AccessCards
        brokerConnected={brokerConnected}
        humanLabel={humanLabel}
        inviteCopied={inviteCopied}
        isHost={isHost}
        selfAccess={selfAccess}
        shareError={shareError}
        shareInviteURL={shareInviteURL}
        shareMutationPending={shareMutationPending}
        shareNetworkLabel={shareNetworkLabel}
        shareRunning={shareRunning}
        shareStatus={shareStatus}
        onCopyInvite={() => void copyInvite()}
        onStartShareInvite={startShareInvite}
        onStopShareInvite={stopShareInvite}
      />

      <BrokerStatusCard isHealthy={isHealthy} status={status} />
      <TeamMemberSessions
        isHost={isHost}
        isRevokingSession={revokeSessionMutation.isPending}
        onRevokeSession={revokeSession}
        revokeError={revokeSessionError}
        revokingSessionID={revokeSessionMutation.variables}
        sessions={sessions}
      />
      <RuntimeStatusList focusMode={data?.focus_mode} items={items} />
    </>
  );
}
