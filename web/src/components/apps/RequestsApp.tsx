import { useQuery, useQueryClient } from "@tanstack/react-query";

import {
  type AgentRequest,
  answerRequest,
  cancelRequest,
  getRequests,
} from "../../api/client";
import { formatRelativeTime } from "../../lib/format";
import { useFallbackChannelSlug } from "../../routes/useCurrentRoute";
import { showNotice } from "../ui/Toast";

export function RequestsApp() {
  // /apps/requests is reachable from any route, so the channel must come
  // from the last-visited conversation rather than the URL's null
  // channelSlug → "general" collapse that would otherwise hide the user's
  // pending requests in their working channel.
  const currentChannel = useFallbackChannelSlug();
  const queryClient = useQueryClient();

  const { data, isLoading, error } = useQuery({
    queryKey: ["requests", currentChannel],
    queryFn: () => getRequests(currentChannel),
    refetchInterval: 5_000,
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
        Loading requests...
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
        Failed to load requests.
      </div>
    );
  }

  const allRequests = dedupeRequests(data);
  const pending = allRequests.filter(
    (r) => !r.status || r.status === "open" || r.status === "pending",
  );
  const answered = allRequests.filter(
    (r) => r.status && r.status !== "open" && r.status !== "pending",
  );

  // Blocking requests are surfaced one at a time so a flood of agent
  // approvals (e.g. retries of the same external action) cannot bury the
  // human under a stack of identical cards. The next blocking request
  // becomes visible as soon as the active one is answered or canceled.
  const blockingPending = pending.filter((r) => r.blocking);
  const nonBlockingPending = pending.filter((r) => !r.blocking);
  const [activeBlocking] = blockingPending;
  const queuedBlockingCount = Math.max(blockingPending.length - 1, 0);

  const onAnswer = (id: string, choiceId: string) => {
    answerRequest(id, choiceId)
      .then(() => {
        queryClient.invalidateQueries({ queryKey: ["requests"] });
      })
      .catch((e: Error) => showNotice(`Answer failed: ${e.message}`, "error"));
  };

  const dismissAllNonBlocking = () => {
    if (nonBlockingPending.length === 0) return;
    Promise.allSettled(nonBlockingPending.map((r) => cancelRequest(r.id))).then(
      (results) => {
        const failed = results.filter((r) => r.status === "rejected").length;
        if (failed > 0) {
          showNotice(`Dismissed with ${failed} error(s)`, "error");
        }
        queryClient.invalidateQueries({ queryKey: ["requests"] });
      },
    );
  };

  if (allRequests.length === 0) {
    return (
      <div
        style={{
          padding: "40px 20px",
          textAlign: "center",
          color: "var(--text-tertiary)",
          fontSize: 14,
        }}
      >
        No requests right now. Your agents are working independently.
      </div>
    );
  }

  return (
    <>
      {activeBlocking ? (
        <>
          <div
            style={{
              display: "flex",
              alignItems: "center",
              justifyContent: "space-between",
              gap: 8,
              padding: "8px 0 4px",
            }}
          >
            <div
              style={{
                fontSize: 13,
                fontWeight: 600,
                color: "var(--text-secondary)",
              }}
            >
              Blocking
            </div>
            {queuedBlockingCount > 0 ? (
              <div style={{ fontSize: 12, color: "var(--text-tertiary)" }}>
                {queuedBlockingCount} more queued
              </div>
            ) : null}
          </div>
          <RequestItem
            key={activeBlocking.id}
            request={activeBlocking}
            isPending={true}
            onAnswer={(choiceId) => onAnswer(activeBlocking.id, choiceId)}
          />
        </>
      ) : null}

      {nonBlockingPending.length > 0 && (
        <>
          <div
            style={{
              display: "flex",
              alignItems: "center",
              justifyContent: "space-between",
              gap: 8,
              padding: "8px 0 4px",
            }}
          >
            <div
              style={{
                fontSize: 13,
                fontWeight: 600,
                color: "var(--text-secondary)",
              }}
            >
              Pending ({nonBlockingPending.length})
            </div>
            <button
              type="button"
              className="btn btn-sm btn-ghost"
              onClick={dismissAllNonBlocking}
            >
              Dismiss all
            </button>
          </div>
          {nonBlockingPending.map((req) => (
            <RequestItem
              key={req.id}
              request={req}
              isPending={true}
              onAnswer={(choiceId) => onAnswer(req.id, choiceId)}
            />
          ))}
        </>
      )}

      {answered.length > 0 && (
        <>
          <div
            style={{
              fontSize: 13,
              fontWeight: 600,
              color: "var(--text-secondary)",
              padding: "12px 0 4px",
            }}
          >
            Answered ({answered.length})
          </div>
          {answered.map((req) => (
            <RequestItem key={req.id} request={req} isPending={false} />
          ))}
        </>
      )}
    </>
  );
}

function dedupeRequests(
  data: { requests: AgentRequest[] } | undefined,
): AgentRequest[] {
  const raw = data?.requests ?? [];
  const seen = new Set<string>();
  return raw.filter((r) => {
    if (!r.id || seen.has(r.id)) return false;
    seen.add(r.id);
    return true;
  });
}

interface RequestItemProps {
  request: AgentRequest;
  isPending: boolean;
  onAnswer?: (choiceId: string) => void;
}

function RequestItem({ request, isPending, onAnswer }: RequestItemProps) {
  // Broker uses `options`; legacy used `choices`. Accept either.
  const options = request.options ?? request.choices ?? [];
  const ts = request.updated_at ?? request.created_at ?? request.timestamp;

  return (
    <div className="app-card" style={{ marginBottom: 8 }}>
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 8,
          marginBottom: 4,
        }}
      >
        <span style={{ fontWeight: 600, fontSize: 13 }}>
          {request.from || "Unknown"}
        </span>
        {request.status ? (
          <span className="badge badge-accent">
            {request.status.toUpperCase()}
          </span>
        ) : null}
        {request.blocking ? (
          <span className="badge badge-yellow">BLOCKING</span>
        ) : null}
        {request.redacted ? (
          <span
            className="badge badge-neutral"
            title={
              request.redaction_reasons?.length
                ? `Redacted: ${request.redaction_reasons.join(", ")}`
                : "Sensitive information was redacted from this request"
            }
          >
            redacted
          </span>
        ) : null}
      </div>

      {request.title && request.title !== "Request" && (
        <div style={{ fontSize: 13, fontWeight: 600, marginBottom: 4 }}>
          {request.title}
        </div>
      )}

      <div style={{ fontSize: 14, marginBottom: 8 }}>
        {request.question || ""}
      </div>

      {request.context ? (
        <div
          style={{
            fontSize: 12,
            color: "var(--text-secondary)",
            marginBottom: 8,
            whiteSpace: "pre-wrap",
          }}
        >
          {request.context}
        </div>
      ) : null}

      {ts ? (
        <div className="app-card-meta" style={{ marginBottom: 6 }}>
          {formatRelativeTime(ts)}
        </div>
      ) : null}

      {isPending && options.length > 0 && (
        <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
          {options.map((opt) => (
            <button
              type="button"
              key={opt.id}
              className={`btn btn-sm ${opt.id === request.recommended_id ? "btn-primary" : "btn-ghost"}`}
              title={opt.description}
              onClick={() => onAnswer?.(opt.id)}
            >
              {opt.label}
            </button>
          ))}
        </div>
      )}

      {!isPending && (
        <div style={{ fontSize: 12, color: "var(--green)", fontWeight: 500 }}>
          Answered
        </div>
      )}
    </div>
  );
}
