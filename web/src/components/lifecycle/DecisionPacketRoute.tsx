import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import {
  type DecisionAction,
  getDecisionPacket,
  postDecision,
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
    mutationFn: (action: DecisionAction) => postDecision(taskId, action),
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: ["lifecycle", "task", taskId],
      });
      void queryClient.invalidateQueries({ queryKey: ["lifecycle", "inbox"] });
      void queryClient.invalidateQueries({ queryKey: ["inbox-badge"] });
    },
  });

  function submitDecision(action: DecisionAction) {
    decisionMutation.mutate(action);
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
      onMerge={() => submitDecision("merge")}
      onRequestChanges={() => submitDecision("request_changes")}
      onDefer={() => submitDecision("defer")}
      onBlock={() => {
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

function PacketSkeleton({ onClose }: { onClose: () => void }) {
  return (
    <div
      className="packet-shell"
      data-testid="decision-packet-loading"
      aria-busy="true"
    >
      <aside className="packet-left" aria-hidden="true">
        <div className="crumb">
          <button
            type="button"
            className="kbd"
            onClick={onClose}
            style={{
              background: "transparent",
              border: "none",
              color: "inherit",
              cursor: "pointer",
            }}
            aria-label="Back to inbox"
          >
            ← inbox
          </button>{" "}
          / task
        </div>
      </aside>
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
      <aside className="packet-right" aria-hidden="true">
        <div
          className="packet-skeleton-block"
          style={{ width: "100%", height: 44 }}
        />
        <div
          className="packet-skeleton-block"
          style={{ width: "100%", height: 44 }}
        />
        <div
          className="packet-skeleton-block"
          style={{ width: "100%", height: 44 }}
        />
      </aside>
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
  return (
    <div className="packet-shell" data-testid="decision-packet-error">
      <aside className="packet-left" />
      <main className="packet-center">
        <div className="packet-error" role="alert">
          <h2>Couldn't load this Decision Packet.</h2>
          <p>{message}</p>
          <div style={{ display: "flex", gap: 8 }}>
            <button type="button" className="retry" onClick={onRetry}>
              Retry
            </button>
            <button type="button" className="retry" onClick={onClose}>
              Back to inbox
            </button>
          </div>
        </div>
      </main>
      <aside className="packet-right" />
    </div>
  );
}
