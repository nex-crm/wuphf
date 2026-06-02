import { useState } from "react";
import { useMutation } from "@tanstack/react-query";

import { type TaskStatusAction, updateTaskStatus } from "../../api/tasks";
import type { LifecycleState } from "../../lib/types/lifecycle";
import { ReopenTaskButton } from "./ReopenTaskButton";
import { ApproveAndStartButton, CloseTaskButton } from "./TaskDocument";

// ── State-aware Issue action toolbar ─────────────────────────────────

interface TaskActionToolbarProps {
  taskId: string;
  channel: string;
  lifecycleState: LifecycleState;
  onAfterAction: () => void;
}

interface ActionDef {
  /** Broker action verb (matches TaskStatusAction). */
  action: TaskStatusAction;
  /** Button label the human sees. */
  label: string;
  /** Visual variant: primary (positive next step), danger (terminal/scary), neutral (everything else). */
  variant: "primary" | "danger" | "neutral";
  /** When true, prompt the human for a 1-line reason before firing. */
  requiresReason?: boolean;
  /** Placeholder for the reason prompt. */
  reasonHint?: string;
}

/** Returns the action set valid for the current lifecycle state. The
 *  set is intentionally small — only show actions that have a clear
 *  resolution path for the human. Pure presentation logic; the broker
 *  is the source of truth for what's actually allowed (the hybrid
 *  CEO/owner gate in Slice 7). */
function actionsForState(state: LifecycleState): ActionDef[] {
  switch (state) {
    case "drafting":
      // Approve & Start has its own button (special verb postDecision).
      // From drafting we offer Cancel as the close path.
      return [
        {
          action: "cancel",
          label: "Cancel issue",
          variant: "danger",
          requiresReason: true,
          reasonHint: "Why cancel? One short line.",
        },
      ];
    case "intake":
    case "ready":
      return [
        {
          action: "review",
          label: "Mark ready for review",
          variant: "primary",
        },
        {
          action: "block",
          label: "Block",
          variant: "neutral",
          requiresReason: true,
          reasonHint: "What's blocking this?",
        },
        {
          action: "cancel",
          label: "Cancel",
          variant: "danger",
          requiresReason: true,
          reasonHint: "Why cancel? One short line.",
        },
      ];
    case "running":
    case "changes_requested":
      return [
        {
          action: "submit_for_review",
          label: "Submit for review",
          variant: "primary",
        },
        {
          action: "complete",
          label: "Mark done",
          variant: "primary",
        },
        {
          action: "block",
          label: "Block",
          variant: "neutral",
          requiresReason: true,
          reasonHint: "What's blocking this?",
        },
        {
          action: "cancel",
          label: "Cancel",
          variant: "danger",
          requiresReason: true,
          reasonHint: "Why cancel? One short line.",
        },
      ];
    case "blocked_on_pr_merge":
      // Blocked tasks are agent-internal blockers. The human only
      // sees this surface on the Issue detail page (the Inbox
      // intentionally hides blocked items per the product model:
      // human input goes through human_interview requests instead).
      // We still offer a manual unblock for the rare case where the
      // human knows they fixed the underlying issue out-of-band
      // (paid a bill, reconnected an account) — no reason required
      // since they probably can't articulate the agent's blocker.
      return [
        {
          action: "resume",
          label: "Force unblock",
          variant: "neutral",
        },
        {
          action: "cancel",
          label: "Cancel",
          variant: "danger",
          requiresReason: true,
          reasonHint: "Why cancel? One short line.",
        },
      ];
    case "review":
    case "decision":
      return [
        {
          action: "approve",
          label: "Approve",
          variant: "primary",
        },
        {
          action: "request_changes",
          label: "Request changes",
          variant: "neutral",
          requiresReason: true,
          reasonHint: "What needs to change?",
        },
      ];
    case "approved":
    case "rejected":
      // Terminal — actions handled by the Reopen button (separate
      // component) and read-only otherwise.
      return [];
    default:
      return [];
  }
}

export function TaskActionToolbar({
  taskId,
  channel,
  lifecycleState,
  onAfterAction,
}: TaskActionToolbarProps) {
  const [pendingReason, setPendingReason] = useState<{
    action: ActionDef;
    reason: string;
  } | null>(null);
  const [error, setError] = useState<string | null>(null);

  const isDrafting = lifecycleState === "drafting";
  const isTerminal =
    lifecycleState === "approved" || lifecycleState === "rejected";
  // Archive is a filing affordance available on every task that is not
  // already archived. It does not gate on lifecycle position the way the
  // state-specific actions do — a human can file away a task from any
  // column (terminal or not) once they are done looking at it.
  const canArchive = lifecycleState !== "archived";

  const statusMutation = useMutation({
    mutationFn: (input: { action: TaskStatusAction; reason?: string }) =>
      updateTaskStatus(taskId, input.action, channel, "human", {
        overrideReason: input.reason,
      }),
    onSuccess: () => {
      setPendingReason(null);
      setError(null);
      onAfterAction();
    },
    onError: (err: unknown) => {
      setError(err instanceof Error ? err.message : "Action failed.");
    },
  });

  const actions = actionsForState(lifecycleState);

  function fire(action: ActionDef) {
    setError(null);
    if (action.requiresReason) {
      setPendingReason({ action, reason: "" });
      return;
    }
    statusMutation.mutate({ action: action.action });
  }

  function submitReason() {
    if (!pendingReason) return;
    const reason = pendingReason.reason.trim();
    if (!reason) {
      setError("Reason is required for this action.");
      return;
    }
    statusMutation.mutate({
      action: pendingReason.action.action,
      reason,
    });
  }

  return (
    <div className="issue-action-toolbar">
      {/* Approve & Start (drafting only) — uses postDecision via the
       * existing button so the special drafting→running transition
       * keeps its current behavior (Slice 1). */}
      {isDrafting ? (
        <ApproveAndStartButton taskId={taskId} onApproved={onAfterAction} />
      ) : null}

      {actions.map((act) => (
        <button
          key={act.action}
          type="button"
          className={`issue-action-button issue-action-button--${act.variant}`}
          onClick={() => fire(act)}
          disabled={statusMutation.isPending}
          data-testid={`action-${act.action}`}
        >
          {act.label}
        </button>
      ))}

      {/* Archive — available on any non-archived task. Files the task
       * into the board's Archive column without a reason prompt. Fires
       * through the same status mutation so onAfterAction() invalidates
       * the task + board queries on success. */}
      {canArchive ? (
        <button
          type="button"
          className="issue-action-button issue-action-button--neutral"
          onClick={() => statusMutation.mutate({ action: "archive" })}
          disabled={statusMutation.isPending}
          data-testid="action-archive"
        >
          Archive
        </button>
      ) : null}

      {/* Reopen for terminal states — separate component because it
       * uses the dedicated reopen endpoint. */}
      {isTerminal ? (
        <ReopenTaskButton
          taskId={taskId}
          channel={channel}
          onReopened={onAfterAction}
        />
      ) : null}

      {/* Inline reason prompt for actions that need a 1-line context. */}
      {pendingReason ? (
        <div className="issue-action-reason-row">
          <input
            type="text"
            className="issue-action-reason-input"
            value={pendingReason.reason}
            placeholder={pendingReason.action.reasonHint ?? "Reason"}
            onChange={(event) =>
              setPendingReason({
                action: pendingReason.action,
                reason: event.target.value,
              })
            }
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                event.preventDefault();
                submitReason();
              } else if (event.key === "Escape") {
                event.preventDefault();
                setPendingReason(null);
              }
            }}
            autoFocus={true}
            data-testid="action-reason-input"
          />
          <button
            type="button"
            className="issue-action-button issue-action-button--primary"
            onClick={submitReason}
            disabled={statusMutation.isPending}
          >
            {statusMutation.isPending ? "…" : pendingReason.action.label}
          </button>
          <button
            type="button"
            className="issue-action-button issue-action-button--neutral"
            onClick={() => setPendingReason(null)}
            disabled={statusMutation.isPending}
          >
            Cancel
          </button>
        </div>
      ) : null}

      {error ? (
        <span className="issue-action-error" role="alert">
          {error}
        </span>
      ) : null}

      {/* CloseTaskButton is intentionally NOT shown here — its old
       * meaning ("reject") is now subsumed into Cancel for non-terminal
       * states and is irrelevant for terminal ones. The void below
       * silences the unused-import warning. */}
      {false ? (
        <CloseTaskButton taskId={taskId} onClosed={onAfterAction} />
      ) : null}
    </div>
  );
}
