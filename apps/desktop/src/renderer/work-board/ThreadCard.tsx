import type {
  ThreadAttentionReason,
  ThreadCurrentSeat,
  ThreadEffectiveStatus,
  ThreadView,
} from "@wuphf/protocol/browser";
import { Computer, ProfileCircle } from "iconoir-react";

import { StatusBadge, type StatusBadgeTone } from "../ui/StatusBadge.tsx";
import { cn } from "../ui/cn.ts";

export interface ThreadCardProps {
  readonly thread: ThreadView;
  readonly onSelect?: ((thread: ThreadView) => void) | undefined;
}

// Renders a single thread on the work board. Static layout only — R2's
// goal is to surface what's in `ThreadView`; navigation to the
// thread-detail surface ships with R3.
export function ThreadCard({ thread, onSelect }: ThreadCardProps) {
  const tone = effectiveStatusTone(thread.effectiveStatus);
  const SeatIcon = thread.currentSeat === "agent" ? Computer : ProfileCircle;
  const isInteractive = onSelect !== undefined;

  const content = (
    <>
      <header className="flex items-start justify-between gap-3">
        <h3 className="text-sm font-semibold leading-tight text-foreground">
          {thread.title.length > 0 ? thread.title : "Untitled thread"}
        </h3>
        <StatusBadge
          label={effectiveStatusLabel(thread.effectiveStatus)}
          tone={tone}
          data-testid="thread-card-status-badge"
        />
      </header>

      <dl className="mt-3 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
        <div className="flex items-center gap-1">
          <dt className="sr-only">Current seat</dt>
          <SeatIcon aria-hidden="true" height={14} width={14} />
          <dd>{thread.currentSeat === "agent" ? "Agent" : "You"}</dd>
        </div>
        {thread.pendingApprovalCount > 0 && (
          <div className="flex items-center gap-1">
            <dt className="sr-only">Pending approvals</dt>
            <dd className="rounded-full bg-amber-50 px-2 py-0.5 font-medium text-amber-800">
              {thread.pendingApprovalCount} pending
            </dd>
          </div>
        )}
        {thread.attentionReason !== undefined && (
          <div className="flex items-center gap-1">
            <dt className="sr-only">Attention reason</dt>
            <dd className="rounded-full bg-red-50 px-2 py-0.5 font-medium text-red-800">
              {attentionReasonLabel(thread.attentionReason)}
            </dd>
          </div>
        )}
      </dl>

      <footer className="mt-3 font-mono text-[10px] uppercase tracking-wide text-muted-foreground">
        {thread.id}
      </footer>
    </>
  );

  if (!isInteractive) {
    return (
      <article
        className="rounded-md border border-border bg-background p-3 shadow-sm"
        data-testid="thread-card"
        data-thread-id={thread.id}
      >
        {content}
      </article>
    );
  }
  return (
    <button
      className={cn(
        "block w-full rounded-md border border-border bg-background p-3 text-left shadow-sm",
        "hover:border-amber-300 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-amber-500",
      )}
      data-testid="thread-card"
      data-thread-id={thread.id}
      onClick={() => onSelect(thread)}
      type="button"
    >
      {content}
    </button>
  );
}

function effectiveStatusTone(status: ThreadEffectiveStatus): StatusBadgeTone {
  switch (status) {
    case "merged":
    case "closed":
      return "ok";
    case "in_progress":
    case "open":
      return "neutral";
    case "needs_review":
      return "pending";
    case "needs_attention":
      return "error";
  }
}

function effectiveStatusLabel(status: ThreadEffectiveStatus): string {
  switch (status) {
    case "open":
      return "Open";
    case "in_progress":
      return "In progress";
    case "needs_review":
      return "Needs review";
    case "needs_attention":
      return "Needs attention";
    case "merged":
      return "Merged";
    case "closed":
      return "Closed";
  }
}

function attentionReasonLabel(reason: ThreadAttentionReason): string {
  switch (reason) {
    case "pending_approval":
      return "Approval pending";
    case "failed":
      return "Failed";
    case "stalled":
      return "Stalled";
  }
}

// Re-exported for tests so they can assert the static label/tone
// contracts without re-encoding them.
export const __testing__ = {
  effectiveStatusLabel,
  effectiveStatusTone,
  attentionReasonLabel,
  seatIconKey: (seat: ThreadCurrentSeat): "agent" | "human" =>
    seat === "agent" ? "agent" : "human",
};
