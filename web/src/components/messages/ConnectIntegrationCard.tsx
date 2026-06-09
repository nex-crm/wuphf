import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import type { AgentRequest } from "../../api/client";
import {
  getIntegrationConnectStatus,
  type IntegrationConnectResult,
  startIntegrationConnection,
} from "../../api/integrations";
import {
  GenericIntegrationLogo,
  ToolkitBrandLogo,
} from "../apps/integrations/IntegrationLogos";
import { showNotice } from "../ui/Toast";

// ConnectIntegrationCard is the human-facing side of a `connect` decision: an
// agent tried a mutating action against an integration that is not connected, so
// the resolver raised this blocking card. Connecting reuses the shipped Composio
// OAuth flow (start → window.open(auth_url) → poll connect-status). The broker
// fan-out (handleIntegrationConnectStatus → fanOutConnected) auto-answers THIS
// card the moment the connection goes live, so the parked action resumes with no
// second prompt — this card just drives the OAuth and the polling.

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
  return fromTitle || (slug ? slug[0].toUpperCase() + slug.slice(1) : "the integration");
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
          onClick={() => connectMutation.mutate()}
          // Only disabled while the start-connect call is in flight — NOT for
          // the whole polling wait — so the human can always reopen a popup that
          // was closed or blocked.
          disabled={submitting || !platform || connectMutation.isPending}
        >
          {pending && !failed ? "Reopen connect window" : `Connect ${name}`}
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
