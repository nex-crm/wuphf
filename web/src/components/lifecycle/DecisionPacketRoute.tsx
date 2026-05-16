import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import {
  type DecisionAction,
  getDecisionPacket,
  postDecision,
  postInboxCursor,
} from "../../api/lifecycle";
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
}: DecisionPacketRouteProps) {
  const query = useQuery<DecisionPacket>({
    queryKey: ["lifecycle", "task", taskId],
    queryFn: () => getDecisionPacket(taskId),
    initialData: initialPacket,
    enabled: forceState !== "loading" && forceState !== "error",
    staleTime: 2_000,
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

  if (forceState === "loading" || (query.isPending && !initialPacket)) {
    return <PacketSkeleton onClose={close} />;
  }

  if (forceState === "error" || (query.isError && !query.data)) {
    return (
      <PacketError
        message={
          query.error instanceof Error
            ? query.error.message
            : "Network or persistence error."
        }
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
