import { useCallback, useEffect, useState } from "react";

import type { DecisionPacket } from "../../lib/types/lifecycle";

interface PacketActionSidebarProps {
  packet: DecisionPacket;
  /** True during streaming/loading — disables decision actions. */
  isDecisionLocked: boolean;
  /**
   * Optional human-authored comment to attach to the decision. The
   * sidebar owns the textarea state and passes the trimmed value
   * back through these callbacks; the route reads spec.feedback on
   * the next packet fetch to render the comment in-thread.
   */
  onApprove: (comment?: string) => void;
  onRequestChanges: (comment?: string) => void;
  onDefer: (comment?: string) => void;
  onBlock: (comment?: string) => void;
  /**
   * Optional comment-only handler. When provided, a "Comment" button
   * appears alongside the decision actions; pressing it posts the
   * comment as a FeedbackItem to the task without transitioning state.
   * Mirrors a PR review-comment that does not approve or request changes.
   */
  onComment?: (body: string) => void | Promise<void>;
  /**
   * Optional terminal-reject handler. When provided, a "Reject" button
   * appears alongside the other decision actions; pressing it posts a
   * terminal rejection (downstream dependents stay blocked, because
   * the work did not land).
   */
  onReject?: (comment: string) => void | Promise<void>;
  onOpenInWorktree: () => void;
}

/**
 * Sticky right column of the Decision Packet view. Action button
 * hierarchy is locked by /plan-design-review:
 *  - Approve:    primary CTA (cyan accent, key `a`).
 *  - Request:    secondary (bg-card + strong border, key `r`).
 *  - Defer:      quiet (transparent + border, no keybind).
 *  - Block:      tertiary danger (transparent + red-500 border, key `b`).
 *  - Worktree:   quiet (transparent + border, key `w`).
 *
 * Disabled when the packet is in `streaming` / `loading` state. A11y:
 * `role="complementary"` so the right column reads as a sidebar
 * landmark, distinct from the center reading column.
 */
export function PacketActionSidebar({
  packet,
  isDecisionLocked,
  onApprove,
  onRequestChanges,
  onDefer,
  onBlock,
  onComment,
  onReject,
  onOpenInWorktree,
}: PacketActionSidebarProps) {
  const [comment, setComment] = useState("");
  const trimmedComment = comment.trim();
  const submit = (callback: (comment?: string) => void) => {
    callback(trimmedComment ? trimmedComment : undefined);
    setComment("");
  };
  // submitComment / submitReject only clear the textarea AFTER the
  // async handler settles. If the network call fails, the reviewer's
  // drafted text stays in the box so they can retry without retyping
  // (CodeRabbit catch on async-clear race).
  const submitComment = useCallback(() => {
    if (!onComment || trimmedComment.length === 0) return;
    const body = trimmedComment;
    void Promise.resolve(onComment(body)).then(() => setComment(""));
  }, [onComment, trimmedComment]);
  const submitReject = useCallback(() => {
    if (!onReject || trimmedComment.length === 0) return;
    const body = trimmedComment;
    void Promise.resolve(onReject(body)).then(() => setComment(""));
  }, [onReject, trimmedComment]);
  const lockedTooltip = isDecisionLocked ? "Wait for review state" : undefined;

  // Keyboard shortcuts for the actions this sidebar owns (Comment + Reject).
  // Approve/Request changes/Block/Worktree shortcuts are registered by
  // DecisionPacketView at the page level. Ignore key events that originate
  // inside form controls so the comment textarea remains typeable.
  useEffect(() => {
    function handler(e: KeyboardEvent) {
      if (isDecisionLocked) return;
      // Don't hijack Cmd/Ctrl/Alt + c / x — those are OS-level copy/cut
      // shortcuts the user expects to keep working.
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      const target = e.target as HTMLElement | null;
      if (target) {
        const tag = target.tagName;
        if (
          tag === "INPUT" ||
          tag === "TEXTAREA" ||
          tag === "SELECT" ||
          target.isContentEditable
        ) {
          return;
        }
      }
      if (e.key === "c" && onComment) {
        e.preventDefault();
        submitComment();
      } else if (e.key === "x" && onReject) {
        e.preventDefault();
        submitReject();
      }
    }
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [isDecisionLocked, onComment, onReject, submitComment, submitReject]);
  const runtime = packet.sessionReport?.metadata?.runtime;
  const toolCalls = packet.sessionReport?.metadata?.tool_calls;
  const ownerSummary = runtime
    ? `${packet.ownerSlug} · ran ${runtime}${
        toolCalls ? ` · ${toolCalls} tool calls` : ""
      }`
    : packet.ownerSlug;

  const watchingValue = packet.reviewers
    .filter((r) => !r.isHuman)
    .map((r) => r.slug)
    .join(", ");

  return (
    <aside className="packet-right" aria-label="Decision actions">
      <h3>Decision</h3>
      <label className="packet-comment-label" htmlFor="packet-comment">
        Add a comment
        <span className="packet-comment-optional">optional</span>
      </label>
      <textarea
        id="packet-comment"
        className="packet-comment"
        placeholder={
          trimmedComment.length > 0
            ? ""
            : "Why are you approving / requesting changes? The agent reads this."
        }
        value={comment}
        disabled={isDecisionLocked}
        onChange={(e) => setComment(e.target.value)}
        rows={3}
      />
      <div className="packet-actions">
        {onComment ? (
          <button
            type="button"
            className="packet-action packet-action--quiet"
            onClick={submitComment}
            disabled={isDecisionLocked || trimmedComment.length === 0}
            title={
              trimmedComment.length === 0
                ? "Type something in the comment box first"
                : lockedTooltip
            }
            data-testid="packet-comment-submit"
          >
            Comment <span className="kbd">c</span>
          </button>
        ) : null}
        <button
          type="button"
          className="packet-action packet-action--approve"
          onClick={() => submit(onApprove)}
          disabled={isDecisionLocked}
          title={lockedTooltip}
        >
          Approve <span className="kbd">a</span>
        </button>
        <button
          type="button"
          className="packet-action packet-action--secondary"
          onClick={() => submit(onRequestChanges)}
          disabled={isDecisionLocked}
          title={lockedTooltip}
        >
          Request changes <span className="kbd">r</span>
        </button>
        <button
          type="button"
          className="packet-action packet-action--quiet"
          onClick={() => submit(onDefer)}
          disabled={isDecisionLocked}
          title={lockedTooltip}
        >
          Defer
          <span className="kbd" aria-hidden="true">
            ·
          </span>
        </button>
        <button
          type="button"
          className="packet-action packet-action--danger"
          onClick={() => submit(onBlock)}
          disabled={isDecisionLocked}
          title={lockedTooltip}
        >
          Block <span className="kbd">b</span>
        </button>
        {onReject ? (
          <button
            type="button"
            className="packet-action packet-action--danger"
            onClick={submitReject}
            disabled={isDecisionLocked || trimmedComment.length === 0}
            title={
              trimmedComment.length === 0
                ? "Reject needs a reason — type one in the comment box first"
                : "Reject is terminal — downstream dependents stay blocked"
            }
            data-testid="packet-reject-submit"
          >
            Reject <span className="kbd">x</span>
          </button>
        ) : null}
        <button
          type="button"
          className="packet-action packet-action--quiet"
          onClick={onOpenInWorktree}
        >
          Open in worktree <span className="kbd">w</span>
        </button>
      </div>

      <h3>Context</h3>
      <div className="packet-aside-card">
        <div className="label">Owner agent</div>
        <div className="value">{ownerSummary}</div>
      </div>
      <div className="packet-aside-card">
        <div className="label">Worktree</div>
        <div className="value">
          <code>{packet.worktreePath}</code>
        </div>
      </div>
      {watchingValue ? (
        <div className="packet-aside-card">
          <div className="label">Watching</div>
          <div className="value">{watchingValue}</div>
        </div>
      ) : null}
      {packet.dependencies.blockedOn.length > 0 ? (
        <div className="packet-aside-card">
          <div className="label">Blocked on</div>
          <div className="value" style={{ color: "var(--warning-500)" }}>
            {packet.dependencies.blockedOn.join(", ")} — waiting merge
          </div>
        </div>
      ) : null}
    </aside>
  );
}
