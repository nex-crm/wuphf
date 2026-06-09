import { useEffect, useMemo, useState } from "react";

import type { LifecycleState } from "../../lib/types/lifecycle";
import { useAppStore } from "../../stores/app";

/**
 * TaskActivityStream — surfaces the per-owner live activity snapshot on
 * the Issue detail page so the human sees, in real time, what the owning
 * agent is doing right now. Pairs with the StatusDot which encodes the
 * Issue's overall state at a glance:
 *
 *   • green-blink — RUNNING (owner is actively working)
 *   • red         — BLOCKED (technical block, no human can unblock)
 *   • orange      — NEEDS_INPUT (waiting on a human decision/review)
 *   • grey        — IDLE (drafting, ready, terminal, etc.)
 *
 * Snapshot data flows from broker SSE → useAppStore.agentActivitySnapshots
 * already (consumed by AgentEventPill on the sidebar). This component
 * subscribes to the same map keyed by ownerSlug — no new wire shape.
 */

export type StatusDotKind = "running" | "blocked" | "needs-input" | "idle";

/**
 * Public mapper — exported so other surfaces (LifecycleStatePill, future
 * inbox row chips) can derive the same activity-color for a given state
 * without duplicating the table.
 */
export function activityDotForLifecycleState(
  state: LifecycleState,
): StatusDotKind {
  return dotKindForLifecycleState(state);
}

export function ariaLabelForActivityDot(dot: StatusDotKind): string {
  switch (dot) {
    case "running":
      return "Running";
    case "blocked":
      return "Blocked";
    case "needs-input":
      return "Needs human input";
    case "idle":
      return "Idle";
  }
}

/**
 * Standalone dot for compact contexts (sub-issue rows, kanban cards,
 * inbox lines). Same color/blink logic as the dot inside
 * LifecycleStatePill but with no surrounding pill chrome so it fits in
 * tight grids. Use the pill when you also want the state label.
 */
export function TaskStatusDot({
  lifecycleState,
}: {
  lifecycleState: LifecycleState;
}) {
  const dot = activityDotForLifecycleState(lifecycleState);
  return (
    <span
      className={`issue-activity-dot issue-activity-dot--${dot}${
        dot === "running" ? " issue-activity-dot--blink" : ""
      }`}
      role="img"
      aria-label={`Status: ${ariaLabelForActivityDot(dot)}`}
      title={ariaLabelForActivityDot(dot)}
    />
  );
}

interface TaskActivityStreamProps {
  ownerSlug: string | undefined;
  lifecycleState: LifecycleState;
}

export function TaskActivityStream({
  ownerSlug,
  lifecycleState,
}: TaskActivityStreamProps) {
  const snapshot = useAppStore((s) =>
    ownerSlug ? s.agentActivitySnapshots[ownerSlug] : undefined,
  );
  const dot = dotKindForLifecycleState(lifecycleState);
  const isLive = dot === "running" && Boolean(snapshot?.activity);

  // 1Hz tick for "X seconds ago" — local instead of the shared
  // AgentEventTickProvider so this component is mountable without the
  // sidebar's provider in scope (tests, alternative shells).
  const [, setTick] = useState(0);
  useEffect(() => {
    if (!isLive) return;
    const id = window.setInterval(() => setTick((t) => t + 1), 1000);
    return () => window.clearInterval(id);
  }, [isLive]);

  const elapsedLabel = useMemo(() => {
    if (!snapshot?.lastTime) return null;
    const lastMs = Date.parse(snapshot.lastTime);
    if (Number.isNaN(lastMs)) return null;
    return formatRelative(Date.now() - lastMs);
  }, [snapshot?.lastTime, snapshot]);

  // No owner = nothing to surface; let the caller decide whether to show
  // a "unowned" hint elsewhere.
  if (!ownerSlug) return null;

  const summary = describeActivity(lifecycleState, snapshot?.activity);
  const detail = snapshot?.detail?.trim() ?? "";

  return (
    <section
      className="issue-activity-stream"
      data-state={dot}
      aria-label="Live activity"
    >
      <span
        className={`issue-activity-dot issue-activity-dot--${dot}${
          dot === "running" ? " issue-activity-dot--blink" : ""
        }`}
        aria-hidden="true"
      />
      <div className="issue-activity-body">
        <div className="issue-activity-row">
          <span className="issue-activity-owner">@{ownerSlug}</span>
          <span className="issue-activity-summary">{summary}</span>
          {elapsedLabel ? (
            <time className="issue-activity-elapsed">{elapsedLabel}</time>
          ) : null}
        </div>
        {detail ? <p className="issue-activity-detail">{detail}</p> : null}
      </div>
    </section>
  );
}

/**
 * Map every LifecycleState to a dot color. New states added in the
 * future must extend this table — exhaustiveness is enforced via the
 * `LifecycleState` union (TS will widen the switch's never branch).
 */
function dotKindForLifecycleState(state: LifecycleState): StatusDotKind {
  switch (state) {
    case "planning":
    case "running":
    case "intake":
      return "running";
    case "blocked_on_pr_merge":
    case "queued_behind_owner":
      return "blocked";
    case "review":
    case "decision":
    case "changes_requested":
      return "needs-input";
    case "drafting":
    case "ready":
    case "approved":
    case "rejected":
    case "archived":
      return "idle";
    default: {
      // Exhaustiveness check — adding a new LifecycleState without
      // extending this switch surfaces as a compile error.
      const _exhaustive: never = state;
      void _exhaustive;
      return "idle";
    }
  }
}

function describeActivity(
  state: LifecycleState,
  activity: string | undefined,
): string {
  const trimmed = activity?.trim();
  if (trimmed) return trimmed;
  switch (state) {
    case "running":
    case "intake":
      return "Idle — waiting for the next event.";
    case "blocked_on_pr_merge":
      return "Blocked — waiting for an upstream PR to merge.";
    case "review":
      return "In review — waiting for a reviewer.";
    case "decision":
      return "Awaiting a human decision.";
    case "changes_requested":
      return "Owner is revising after a review comment.";
    case "drafting":
      return "Drafting — not started yet.";
    case "ready":
      return "Ready to start.";
    case "approved":
      return "Approved.";
    case "rejected":
      return "Rejected.";
    case "archived":
      return "Archived.";
    default:
      return "Idle.";
  }
}

function formatRelative(deltaMs: number): string {
  if (deltaMs < 0) return "just now";
  const sec = Math.floor(deltaMs / 1000);
  if (sec < 5) return "just now";
  if (sec < 60) return `${sec}s ago`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min}m ago`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr}h ago`;
  const days = Math.floor(hr / 24);
  return `${days}d ago`;
}
