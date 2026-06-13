import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { type AgentRequest, getConfig } from "../../api/client";
import {
  type ComposioSigninState,
  getComposioSigninStatus,
  getIntegrationConnectStatus,
  type IntegrationConnectResult,
  startComposioSignin,
  startIntegrationConnection,
} from "../../api/integrations";
import {
  GenericIntegrationLogo,
  ToolkitBrandLogo,
} from "../apps/integrations/IntegrationLogos";
import { showNotice } from "../ui/Toast";

// ConnectIntegrationCard is the human-facing side of a `connect` decision: an
// agent tried a mutating action against an integration that is not connected, so
// the resolver raised this blocking card.
//
// Two gates, in order:
//   1. Composio account sign-in. Connecting any integration needs a Composio
//      project key. If the office isn't signed in yet (config.composio_key_set
//      is false), the FIRST click runs the broker-driven "Sign in with Composio"
//      flow (open its auth_url, poll until done) and only THEN initiates the
//      integration connection — the user never has to find a separate settings
//      screen first.
//   2. Integration OAuth. start → window.open(auth_url) → poll connect-status.
//      The broker fan-out (handleIntegrationConnectStatus → fanOutConnected)
//      auto-answers THIS card the moment the connection goes live, so the parked
//      action resumes with no second prompt.

interface ConnectIntegrationCardProps {
  request: AgentRequest;
  submitting: boolean;
  /** Answer the card with "skip" — abandon the action. */
  onSkip: () => void;
  /** Cancel the request entirely. */
  onDismiss: () => void;
}

function platformName(request: AgentRequest): string {
  const slug = (request.platform ?? "").trim();
  const fromTitle = (request.title ?? "").replace(/^connect\s+/i, "").trim();
  return (
    fromTitle ||
    (slug ? slug[0].toUpperCase() + slug.slice(1) : "the integration")
  );
}

export function ConnectIntegrationCard({
  request,
  submitting,
  onSkip,
  onDismiss,
}: ConnectIntegrationCardProps) {
  const queryClient = useQueryClient();
  const platform = (request.platform ?? "").trim();
  const name = platformName(request);
  const [pending, setPending] = useState<IntegrationConnectResult | null>(null);

  const statusQuery = useQuery({
    queryKey: [
      "connect-card-status",
      pending?.provider,
      pending?.connect_id,
      pending?.platform,
    ],
    // Poll once a connection is pending, even if Composio returned no
    // connect_id — fall back to polling by platform so the card cannot deadlock
    // in `connecting` with no status progression.
    enabled: Boolean(pending),
    queryFn: () =>
      getIntegrationConnectStatus({
        provider: pending?.provider ?? "composio",
        platform: pending?.platform ?? platform,
        connect_id: pending?.connect_id,
      }),
    // Poll until the connection settles. Each poll hits the broker's
    // connect-status endpoint, which is what fires the server-side fan-out that
    // auto-answers this card.
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status === "connected" || status === "failed" ? false : 3_000;
    },
  });

  const status = statusQuery.data?.status ?? pending?.status;

  // When the connection goes live the broker has already answered this card;
  // refetch the requests so the overlay drops it and the parked action resumes.
  useEffect(() => {
    if (status !== "connected") return;
    showNotice(`${name} connected.`, "success");
    void queryClient.invalidateQueries({ queryKey: ["requests"] });
    void queryClient.invalidateQueries({ queryKey: ["requests-badge"] });
  }, [status, name, queryClient]);

  const connectMutation = useMutation({
    mutationFn: () => startIntegrationConnection("composio", platform),
    onSuccess: (result) => {
      setPending(result);
      if (result.auth_url) {
        window.open(result.auth_url, "_blank", "noopener,noreferrer");
      }
    },
    onError: (err: unknown) => {
      showNotice(
        err instanceof Error ? err.message : `Failed to connect ${name}`,
        "error",
      );
    },
  });

  // ── Composio account sign-in gate ──────────────────────────────────
  // Connecting any integration needs a Composio project key. If the office is
  // not signed in yet, the first Connect click runs the "Sign in with Composio"
  // flow, then chains into the integration connection.
  const configQuery = useQuery({
    queryKey: ["config"],
    queryFn: getConfig,
    staleTime: 10_000,
  });
  const composioSignedIn = configQuery.data?.composio_key_set === true;

  const [signin, setSignin] = useState<ComposioSigninState | null>(null);

  const signinStatusQuery = useQuery({
    queryKey: ["composio-signin-status"],
    // Poll while the broker is working: installing the CLI, awaiting the
    // browser login, or provisioning the key.
    enabled:
      Boolean(signin) &&
      (signin?.status === "installing" ||
        signin?.status === "awaiting_login" ||
        signin?.status === "provisioning"),
    queryFn: getComposioSigninStatus,
    refetchInterval: (query) => {
      const s = query.state.data?.status;
      return s === "done" || s === "error" || s === "cli_missing"
        ? false
        : 2_500;
    },
  });
  const signinInfo = signinStatusQuery.data ?? signin;
  const signinStatus = signinInfo?.status;

  // The auth_url may arrive on the START response (CLI already present) OR later
  // via the status poll (after an auto-install completes). Open it once, the
  // moment it first appears, from whichever source.
  // Prefer whichever source carries a URL: the start response (CLI present) or
  // the poll (auto-install path). A poll frame without auth_url must not mask
  // one the start already provided.
  const authUrl = signin?.auth_url ?? signinStatusQuery.data?.auth_url;
  const openedAuthUrlRef = useRef<string | null>(null);
  useEffect(() => {
    if (!authUrl || openedAuthUrlRef.current === authUrl) return;
    openedAuthUrlRef.current = authUrl;
    window.open(authUrl, "_blank", "noopener,noreferrer");
  }, [authUrl]);

  const signinMutation = useMutation({
    mutationFn: startComposioSignin,
    // Don't open the popup here — the effect above is the single opener so the
    // auto-install path (auth_url arrives later, via the poll) works the same.
    onSuccess: (state) => setSignin(state),
    onError: (err: unknown) => {
      showNotice(
        err instanceof Error ? err.message : "Could not start Composio sign-in",
        "error",
      );
    },
  });

  // When Composio sign-in finishes, refresh config and chain straight into the
  // integration connection — the user clicked Connect once and should not have
  // to click again after authorizing Composio itself.
  // biome-ignore lint/correctness/useExhaustiveDependencies: fire exactly once when sign-in flips to done
  useEffect(() => {
    if (signinStatus !== "done") return;
    setSignin(null);
    void queryClient.invalidateQueries({ queryKey: ["config"] });
    connectMutation.mutate();
  }, [signinStatus]);

  const signingIn =
    signinMutation.isPending ||
    signinStatus === "installing" ||
    signinStatus === "awaiting_login" ||
    signinStatus === "provisioning";
  const cliMissing = signinStatus === "cli_missing";
  const signinFailed = signinStatus === "error";

  // Connect entry point: gate on Composio sign-in, then initiate the connection.
  const handleConnect = () => {
    if (!composioSignedIn) {
      signinMutation.mutate();
      return;
    }
    connectMutation.mutate();
  };

  const connecting =
    connectMutation.isPending ||
    (Boolean(pending) && status !== "failed" && status !== "connected");
  const failed = status === "failed";
  const brandLogo = platform ? <ToolkitBrandLogo platform={platform} /> : null;

  return (
    <div className="eac eac-connect">
      <header className="eac-head">
        <div className="eac-logo" aria-hidden="true">
          {brandLogo ?? <GenericIntegrationLogo />}
        </div>
        <div className="eac-headings">
          <span className="eac-eyebrow">Connect to continue</span>
          <h3 className="eac-headline">Connect {name}</h3>
          {request.from ? (
            <span className="eac-connect-sub">
              @{request.from} needs this to run an external action.
            </span>
          ) : null}
        </div>
      </header>

      {request.question ? (
        <p className="eac-connect-question">{request.question}</p>
      ) : null}

      {signingIn ? (
        <div className="eac-connect-status" role="status">
          <span className="eac-spinner" aria-hidden="true" />
          <span>
            {signinStatus === "installing"
              ? "Setting up Composio (one-time)…"
              : signinStatus === "provisioning"
                ? "Setting up your Composio account…"
                : "Finish signing in to Composio in the popup. We'll connect " +
                  `${name} automatically once you're in.`}
          </span>
        </div>
      ) : null}
      {cliMissing ? (
        <div className="eac-connect-status eac-connect-failed" role="status">
          The Composio CLI isn't installed.{" "}
          {signinInfo?.install_command ? (
            <>
              Run <code>{signinInfo.install_command}</code> in a terminal, then
              try again.
            </>
          ) : (
            "Install it, then try again."
          )}
        </div>
      ) : null}
      {signinFailed ? (
        <div className="eac-connect-status eac-connect-failed" role="status">
          Composio sign-in didn't complete
          {signinInfo?.reason ? `: ${signinInfo.reason}` : "."} You can try
          again.
        </div>
      ) : null}
      {connecting ? (
        <div className="eac-connect-status" role="status">
          <span className="eac-spinner" aria-hidden="true" />
          <span>
            Waiting for you to finish in the popup window. This resumes
            automatically once {name} is connected.
          </span>
        </div>
      ) : null}
      {failed ? (
        <div className="eac-connect-status eac-connect-failed" role="status">
          Connection did not complete. You can try again.
        </div>
      ) : null}

      <div className="eac-actions">
        <button
          type="button"
          className="btn btn-sm btn-primary"
          onClick={handleConnect}
          // Only disabled while a start call is in flight — NOT for the whole
          // polling wait — so the human can always reopen a popup that was
          // closed or blocked.
          disabled={
            submitting ||
            !platform ||
            connectMutation.isPending ||
            signinMutation.isPending
          }
        >
          {signingIn
            ? "Signing in to Composio…"
            : !composioSignedIn
              ? `Sign in & connect ${name}`
              : pending && !failed
                ? "Reopen connect window"
                : `Connect ${name}`}
        </button>
        <button
          type="button"
          className="btn btn-sm btn-ghost eac-reject"
          onClick={onSkip}
          disabled={submitting}
        >
          Skip
        </button>
        <button
          type="button"
          className="btn btn-sm btn-ghost eac-dismiss"
          onClick={onDismiss}
          disabled={submitting}
        >
          Dismiss
        </button>
      </div>
    </div>
  );
}
