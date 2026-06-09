import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import {
  type DecisionAction,
  getDecisionPacket,
  postDecision,
  postInboxCursor,
  postTaskReject,
  postTaskResume,
} from "../../api/lifecycle";
import type { InboxItem } from "../../lib/types/inbox";
import type { DecisionPacket } from "../../lib/types/lifecycle";
import { DecisionPacketView } from "./DecisionPacketView";

interface DecisionPacketRouteProps {
  taskId: string;
  /** Optional initial packet — used by tests + screenshot harness. */
  initialPacket?: DecisionPacket;
  /** Force a state for screenshot capture / E2E. */
  forceState?:
    | "loading"
    | "streaming"
    | "error"
    | "missing_packet"
    | "populated"
    | "reviewer_timeout"
    | "persistence_error";
  onClose?: () => void;
  /**
   * Inbox-row fallback used when the broker has not yet written a packet to
   * disk. Renders the task's basic metadata instead of the cold "details
   * aren't ready yet" placeholder, so the human at least sees what the task
   * is about while the owner agent is still working.
   */
  fallbackItem?: Extract<InboxItem, { kind: "task" }>;
}

/**
 * `/task/:id` route container. Owns:
 *  - Fetch loop (mocked until Lane C lands the real persistence path).
 *  - All 7 interaction states (loading / streaming / error /
 *    missing-packet / populated / reviewer-timeout / persistence-error).
 *  - Decision actions: stub callbacks that POST to the broker once
 *    Lane A merges. Toast confirmations are deferred to the same lane.
 */
export function DecisionPacketRoute({
  taskId,
  initialPacket,
  forceState,
  onClose,
  fallbackItem,
}: DecisionPacketRouteProps) {
  const query = useQuery<DecisionPacket>({
    queryKey: ["lifecycle", "task", taskId],
    queryFn: () => getDecisionPacket(taskId),
    initialData: initialPacket,
    enabled: forceState !== "loading" && forceState !== "error",
    staleTime: 2_000,
    // "not yet available" is a definitive 404 — retrying just keeps the
    // user staring at the loading skeleton. Skip retries for that branch
    // so the PacketPending / PacketError fallback renders immediately.
    retry: (failureCount, error) => {
      const msg = error instanceof Error ? error.message : String(error);
      if (/not yet available/i.test(msg)) return false;
      return failureCount < 2;
    },
  });

  const queryClient = useQueryClient();

  function close() {
    if (onClose) {
      onClose();
      return;
    }
    if (typeof window !== "undefined") {
      window.location.hash = "#/inbox";
    }
  }

  const decisionMutation = useMutation({
    mutationFn: ({
      action,
      comment,
    }: {
      action: DecisionAction;
      comment?: string;
    }) => postDecision(taskId, action, comment),
    onSuccess: () => {
      // Record the cursor first so the badge math reflects the new
      // last-seen-at on the next refresh, then invalidate the cached
      // queries so the inbox + packet re-fetch.
      void postInboxCursor();
      void queryClient.invalidateQueries({
        queryKey: ["lifecycle", "task", taskId],
      });
      void queryClient.invalidateQueries({ queryKey: ["lifecycle", "inbox"] });
      void queryClient.invalidateQueries({
        queryKey: ["lifecycle", "inbox-items"],
      });
      void queryClient.invalidateQueries({ queryKey: ["inbox-badge"] });
    },
  });

  function submitDecision(action: DecisionAction, comment?: string) {
    decisionMutation.mutate({ action, comment });
  }

  async function submitReject(body: string) {
    try {
      await postTaskReject(taskId, body);
      // Reject is terminal — refresh the packet, both inbox query keys
      // (the legacy ["lifecycle","inbox"] and the Phase-2
      // ["lifecycle","inbox-items"]) plus the badge count so the row
      // reflects the rejected state immediately.
      void postInboxCursor();
      void queryClient.invalidateQueries({
        queryKey: ["lifecycle", "task", taskId],
      });
      void queryClient.invalidateQueries({ queryKey: ["lifecycle", "inbox"] });
      void queryClient.invalidateQueries({
        queryKey: ["lifecycle", "inbox-items"],
      });
      void queryClient.invalidateQueries({ queryKey: ["inbox-badge"] });
    } catch (err) {
      console.error("postTaskReject failed", err);
    }
  }

  if (forceState === "loading" || (query.isPending && !initialPacket)) {
    return <PacketSkeleton onClose={close} />;
  }

  if (forceState === "error" || (query.isError && !query.data)) {
    const message =
      query.error instanceof Error
        ? query.error.message
        : "Network or persistence error.";
    if (fallbackItem && /not yet available/i.test(message)) {
      return (
        <PacketPending
          item={fallbackItem}
          onRetry={() => query.refetch()}
          onClose={close}
          onResume={async (reason?: string) => {
            try {
              await postTaskResume(taskId, reason);
              void postInboxCursor();
              void queryClient.invalidateQueries({
                queryKey: ["lifecycle", "task", taskId],
              });
              void queryClient.invalidateQueries({
                queryKey: ["lifecycle", "inbox"],
              });
              void queryClient.invalidateQueries({
                queryKey: ["lifecycle", "inbox-items"],
              });
              void queryClient.invalidateQueries({
                queryKey: ["inbox-badge"],
              });
            } catch (err) {
              console.error("postTaskResume failed", err);
            }
          }}
          onReject={(reason: string) => submitReject(reason)}
        />
      );
    }
    return (
      <PacketError
        message={message}
        onRetry={() => query.refetch()}
        onClose={close}
      />
    );
  }

  const packet = query.data;
  if (!packet) {
    return <PacketSkeleton onClose={close} />;
  }

  const isStreaming =
    forceState === "streaming" ||
    packet.lifecycleState === "running" ||
    packet.lifecycleState === "review";
  // Defensive against the Go-side encoding empty slices as `null`
  // instead of `[]`. The TS type declares `banners: PacketBanner[]` so
  // tsc is happy, but the wire shape can omit it entirely for packets
  // with no banners.
  const hasPersistenceError =
    forceState === "persistence_error" ||
    (packet.banners ?? []).some((b) => b.kind === "persistence_error");
  const effectivePacket: DecisionPacket =
    forceState === "missing_packet"
      ? { ...packet, regeneratedFromMemory: true }
      : packet;

  return (
    <DecisionPacketView
      packet={effectivePacket}
      isStreaming={isStreaming}
      hasPersistenceError={hasPersistenceError}
      onClose={close}
      onApprove={(comment?: string) => submitDecision("approve", comment)}
      onRequestChanges={(comment?: string) =>
        submitDecision("request_changes", comment)
      }
      onDefer={(comment?: string) => submitDecision("defer", comment)}
      onBlock={(_comment?: string) => {
        /* block flow lives behind its own modal (Lane F follow-up). */
      }}
      onReject={(body) => submitReject(body)}
      onOpenInWorktree={() => {
        if (typeof window !== "undefined" && packet?.worktreePath) {
          window.open(`file://${packet.worktreePath}`, "_blank");
        }
      }}
    />
  );
}

function PacketSkeleton({ onClose: _onClose }: { onClose: () => void }) {
  return (
    <div
      className="packet-shell packet-shell--message"
      data-testid="decision-packet-loading"
      aria-busy="true"
    >
      <main className="packet-center">
        <div className="packet-skeleton-title" />
        <div className="packet-skeleton-block" style={{ width: "85%" }} />
        <div className="packet-skeleton-block" style={{ width: "70%" }} />
        {[0, 1, 2, 3, 4].map((i) => (
          <div
            key={i}
            className="packet-skeleton-block"
            style={{ width: `${60 + (i % 3) * 10}%` }}
          />
        ))}
      </main>
    </div>
  );
}

function pendingStateExplainer(state: string, details?: string): string {
  const trimmed = (details ?? "").trim();
  switch (state) {
    case "decision":
      return "Waiting on a packet write. The owner agent has flagged this for a human call.";
    case "review":
      return "Reviewers are grading the work. Detail surfaces once enough grades land.";
    case "changes_requested":
      return "Changes were requested. The owner agent is iterating on the spec.";
    case "blocked_on_pr_merge":
      // The lifecycle state name is historical — most real blocks are agent
      // timeouts, agent errors, or cross-task dependencies, not actual PR
      // merges. Prefer the broker's own reason when present so the human
      // sees the real "why" instead of the generic framing.
      return (
        trimmed ||
        "The owner agent is paused. Resume to retry, or reject to drop the task."
      );
    case "approved":
      return "Approved. The packet write is still in flight.";
    case "rejected":
      return "Rejected. The packet write is still in flight.";
    default:
      return (
        trimmed ||
        "The owner agent is still working. Full decision packet will surface here once it lands."
      );
  }
}

function PacketPending({
  item,
  onRetry,
  onClose,
  onResume,
  onReject,
}: {
  item: Extract<InboxItem, { kind: "task" }>;
  onRetry: () => void;
  onClose: () => void;
  onResume?: (reason?: string) => void;
  onReject?: (reason: string) => void;
}) {
  const state = item.task?.state ?? "";
  const details = item.task?.details;
  const blockedOn = item.task?.blockedOn ?? [];
  const explainer = pendingStateExplainer(state, details);
  const owner = item.agentSlug || item.task?.assignment || "";
  const isBlocked = state === "blocked_on_pr_merge";
  return (
    <div
      className="packet-shell packet-shell--message"
      data-testid="decision-packet-pending"
    >
      <main className="packet-center">
        <div className="packet-error" role="status">
          <h2>
            {isBlocked
              ? `Blocked: ${item.title || "(no subject)"}`
              : item.title || "(no subject)"}
          </h2>
          <p>{explainer}</p>
          <dl
            style={{
              display: "grid",
              gridTemplateColumns: "auto 1fr",
              columnGap: 12,
              rowGap: 4,
              fontSize: 13,
              margin: "12px 0",
              textAlign: "left",
            }}
          >
            <dt style={{ color: "var(--text-tertiary)" }}>Task</dt>
            <dd style={{ margin: 0 }}>
              <code>{item.taskId}</code>
            </dd>
            {state ? (
              <>
                <dt style={{ color: "var(--text-tertiary)" }}>State</dt>
                <dd style={{ margin: 0 }}>{state}</dd>
              </>
            ) : null}
            {owner ? (
              <>
                <dt style={{ color: "var(--text-tertiary)" }}>Owner</dt>
                <dd style={{ margin: 0 }}>@{owner}</dd>
              </>
            ) : null}
            {item.channel ? (
              <>
                <dt style={{ color: "var(--text-tertiary)" }}>Channel</dt>
                <dd style={{ margin: 0 }}>#{item.channel}</dd>
              </>
            ) : null}
            {blockedOn.length > 0 ? (
              <>
                <dt style={{ color: "var(--text-tertiary)" }}>Blocked on</dt>
                <dd
                  style={{
                    margin: 0,
                    display: "flex",
                    flexWrap: "wrap",
                    gap: 6,
                  }}
                >
                  {blockedOn.map((blockerId) => (
                    // Real anchor links so middle-click / cmd-click /
                    // "copy link" / screen-reader announce all behave
                    // the way users expect for navigation. A button
                    // with a click handler would force keyboard-only
                    // open via Enter and break copy-link entirely.
                    <a
                      key={blockerId}
                      href={`#/task/${encodeURIComponent(blockerId)}`}
                      className="retry"
                      data-testid="packet-pending-blocker"
                      style={{
                        padding: "2px 8px",
                        fontSize: 12,
                        textDecoration: "none",
                      }}
                    >
                      {blockerId}
                    </a>
                  ))}
                </dd>
              </>
            ) : null}
          </dl>
          <div
            style={{
              display: "flex",
              gap: 8,
              justifyContent: "center",
              flexWrap: "wrap",
            }}
          >
            {isBlocked && onResume ? (
              <button
                type="button"
                className="retry"
                data-testid="packet-pending-resume"
                onClick={() => onResume()}
              >
                Resume
              </button>
            ) : null}
            {isBlocked && onReject ? (
              <button
                type="button"
                className="retry"
                data-testid="packet-pending-reject"
                onClick={() => {
                  if (typeof window === "undefined") return;
                  const reason = window.prompt(
                    "Reject this task? Reason (required):",
                    "Manual reject from inbox.",
                  );
                  const trimmed = (reason ?? "").trim();
                  if (trimmed) onReject(trimmed);
                }}
              >
                Reject
              </button>
            ) : null}
            <button type="button" className="retry" onClick={onRetry}>
              Refresh
            </button>
            <button type="button" className="retry" onClick={onClose}>
              Back to inbox
            </button>
          </div>
        </div>
      </main>
    </div>
  );
}

function PacketError({
  message,
  onRetry,
  onClose,
}: {
  message: string;
  onRetry: () => void;
  onClose: () => void;
}) {
  const isNotReadyYet = /not yet available/i.test(message);
  const heading = isNotReadyYet
    ? "Decision details aren't ready yet."
    : "Couldn't load this decision.";
  const body = isNotReadyYet
    ? "The owner agent is still working. This task will surface a full decision packet once it transitions to review."
    : message;
  return (
    <div
      className="packet-shell packet-shell--message"
      data-testid="decision-packet-error"
    >
      <main className="packet-center">
        <div className="packet-error" role="alert">
          <h2>{heading}</h2>
          <p>{body}</p>
          {!isNotReadyYet ? (
            <div style={{ display: "flex", gap: 8 }}>
              <button type="button" className="retry" onClick={onRetry}>
                Retry
              </button>
              <button type="button" className="retry" onClick={onClose}>
                Back to inbox
              </button>
            </div>
          ) : null}
        </div>
      </main>
    </div>
  );
}
