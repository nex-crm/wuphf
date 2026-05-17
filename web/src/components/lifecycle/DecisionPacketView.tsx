import { useEffect } from "react";

import type {
  DecisionPacket,
  FeedbackItem,
  LifecycleState,
  PacketBanner,
} from "../../lib/types/lifecycle";
import { LifecycleStatePill } from "./LifecycleStatePill";
import { PacketActionSidebar } from "./PacketActionSidebar";
import { SeverityGradeCard } from "./SeverityGradeCard";

interface DecisionPacketViewProps {
  packet: DecisionPacket;
  /** Streaming state: owner agent still working. Locks decision buttons. */
  isStreaming?: boolean;
  /** Persistence-error banner shown above everything else. */
  hasPersistenceError?: boolean;
  /** Reviewer-convergence-timeout banner shown above everything else. */
  hasReviewerTimeout?: boolean;
  /** Callback the route uses to navigate back to /inbox on Esc. */
  onClose: () => void;
  onApprove: () => void;
  onRequestChanges: () => void;
  onDefer: () => void;
  onBlock: () => void;
  onComment?: (body: string) => void | Promise<void>;
  onReject?: (body: string) => void | Promise<void>;
  onOpenInWorktree: () => void;
}

/**
 * Three-column Decision Packet view. NOT centered hero, NOT stacked
 * cards, NOT a chat thread.
 *
 *   - Left: deps / sub-issues / reviewer set context (`role=navigation`).
 *   - Center: spec → AC → session report → diff → grades reading flow
 *     (`role=main`).
 *   - Right: sticky action sidebar (`role=complementary`).
 *
 * State coverage handled here: populated, streaming, reviewer-timeout,
 * persistence-error, missing-packet (regenerated). Loading/error are
 * handled at the route layer above this so the view always receives a
 * concrete `DecisionPacket`.
 */
export function DecisionPacketView({
  packet,
  isStreaming = false,
  hasPersistenceError = false,
  hasReviewerTimeout = false,
  onClose,
  onApprove,
  onRequestChanges,
  onDefer,
  onBlock,
  onComment,
  onReject,
  onOpenInWorktree,
}: DecisionPacketViewProps) {
  // Keyboard shortcuts — a / r / b / w / Esc per locked v1 design.
  useEffect(() => {
    const actionMap: Record<string, () => void> = {
      a: onApprove,
      r: onRequestChanges,
      b: onBlock,
      w: onOpenInWorktree,
    };
    function handler(e: KeyboardEvent) {
      if (shouldIgnoreShortcut(e)) return;
      if (e.key === "Escape") {
        onClose();
        return;
      }
      if (isStreaming) return;
      const action = actionMap[e.key.toLowerCase()];
      if (action) action();
    }
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [
    isStreaming,
    onClose,
    onApprove,
    onRequestChanges,
    onBlock,
    onOpenInWorktree,
  ]);

  const acDoneCount = packet.spec.acceptanceCriteria.filter(
    (ac) => ac.done,
  ).length;
  const acTotal = packet.spec.acceptanceCriteria.length;
  const totalAdds = packet.changedFiles.reduce((n, f) => n + f.additions, 0);
  const totalDels = packet.changedFiles.reduce((n, f) => n + f.deletions, 0);
  const grades = packet.reviewerGrades;
  const gradedCount = grades.filter((g) => g.severity !== "skipped").length;
  const skippedCount = grades.filter((g) => g.severity === "skipped").length;

  return (
    <div className="packet-shell">
      <PacketLeftColumn packet={packet} />
      <main className="packet-center">
        {hasPersistenceError ? <PersistenceBanner /> : null}
        {hasReviewerTimeout ? <ReviewerTimeoutForcedBanner /> : null}
        {packet.regeneratedFromMemory ? <RegeneratedBanner /> : null}
        <PacketMeta packet={packet} />
        <h1 className="packet-task-title">{packet.title}</h1>

        <div className="packet-assignment">
          <div className="label">Your call</div>
          <p>{packet.spec.assignment}</p>
        </div>

        <section
          className="packet-section"
          aria-label="Spec and acceptance criteria"
        >
          <h3>
            Spec{" "}
            <span className="count">
              {acDoneCount}/{acTotal} acceptance criteria done
            </span>
          </h3>
          <p>{packet.spec.problem}</p>
          <div className="packet-ac">
            {packet.spec.acceptanceCriteria.map((ac, idx) => (
              <div
                key={`${idx}-${ac.statement}`}
                className={`packet-ac-item ${ac.done ? "done" : "todo"}`}
              >
                <div className="packet-ac-check" aria-hidden="true">
                  {ac.done ? "✓" : ""}
                </div>
                <span>{ac.statement}</span>
              </div>
            ))}
          </div>
        </section>

        <section className="packet-section" aria-label="Session report">
          <h3 className={isStreaming ? "is-streaming" : undefined}>
            What changed{" "}
            <span className="count">
              +{totalAdds} / −{totalDels} across {packet.changedFiles.length}{" "}
              files
            </span>
          </h3>
          {isStreaming ? (
            <p className="packet-streaming-hint">
              Owner agent still working… acceptance criteria can update
              mid-view.
            </p>
          ) : null}
          <div className="packet-report">
            <h4>Highlights</h4>
            <p className="highlights-prose">
              {packet.sessionReport.highlights}
            </p>
          </div>
          {packet.sessionReport.topWins.length > 0 ? (
            <div className="packet-report">
              <h4>What I tried that worked (kept)</h4>
              <ul>
                {packet.sessionReport.topWins.map((win) => (
                  <li key={`${win.delta}-${win.description}`}>
                    <span className="delta">{win.delta}</span>
                    <span>{win.description}</span>
                  </li>
                ))}
              </ul>
            </div>
          ) : null}
          {packet.sessionReport.deadEnds.length > 0 ? (
            <div className="packet-report">
              <h4>What I tried that didn't work (dead ends)</h4>
              <ul>
                {packet.sessionReport.deadEnds.map((d) => (
                  <li key={`${d.tried}-${d.reason}`} className="dead-end">
                    <span className="delta">{d.tried}</span>
                    <span>{d.reason}</span>
                  </li>
                ))}
              </ul>
            </div>
          ) : null}
          <div className="packet-diff">
            {packet.changedFiles.map((f) => (
              <div key={f.path} className="packet-diff-row">
                <span className="stat-pos">+{f.additions}</span>
                <span className="stat-neg">−{f.deletions}</span>
                <span className="file-path">{f.path}</span>
                {f.status === "added" ? (
                  <span className="file-tag">new</span>
                ) : (
                  <span />
                )}
              </div>
            ))}
          </div>
        </section>

        <section className="packet-section" aria-label="Reviewer grades">
          <h3>
            Reviewer grades{" "}
            <span className="count">
              {gradedCount} of {grades.length} graded
              {skippedCount > 0 ? ` · ${skippedCount} timed out` : ""}
            </span>
          </h3>
          {packet.banners
            .filter((b) => b.kind === "reviewer_timeout")
            .map((banner, idx) => (
              <ReviewerTimeoutBanner
                key={`${banner.kind}-${banner.reviewerSlug ?? idx}`}
                banner={banner}
              />
            ))}
          <div className="packet-grades">
            {grades.map((g) => (
              <SeverityGradeCard
                key={`${g.reviewerSlug}-${g.submittedAt}-${g.suggestion}`}
                grade={g}
              />
            ))}
          </div>
        </section>

        <DiscussionSection feedback={packet.spec.feedback ?? []} />
      </main>
      <PacketActionSidebar
        packet={packet}
        isDecisionLocked={isStreaming}
        onApprove={onApprove}
        onRequestChanges={onRequestChanges}
        onDefer={onDefer}
        onBlock={onBlock}
        onComment={onComment}
        onReject={onReject}
        onOpenInWorktree={onOpenInWorktree}
      />
    </div>
  );
}

function PacketMeta({ packet }: { packet: DecisionPacket }) {
  const ago = formatHoursAgo(packet.updatedAt);
  return (
    <div className="packet-task-meta">
      <LifecycleStatePill state={packet.lifecycleState} />
      <span aria-hidden="true">·</span>
      <span>{packet.taskId}</span>
      <span aria-hidden="true">·</span>
      <span>
        {packet.ownerSlug} · {ago}
      </span>
    </div>
  );
}

function PacketLeftColumn({ packet }: { packet: DecisionPacket }) {
  const allReviewersGraded = packet.reviewers.every((r) => r.hasGraded);
  return (
    <nav className="packet-left" aria-label="Task context">
      <div className="crumb">
        <a href="#/inbox">inbox</a> / task
      </div>
      {packet.subIssues.length > 0 ? (
        <>
          <h2>Sub-issues</h2>
          <div className="packet-deps">
            {packet.subIssues.map((sub) => (
              <div key={sub.taskId} className="packet-dep">
                <span className="dot" aria-hidden="true" />
                <span style={{ flex: 1, minWidth: 0 }}>{sub.title}</span>
                <span
                  style={{
                    color: "var(--text-tertiary)",
                    fontSize: 12,
                  }}
                >
                  {stateLabel(sub.state)}
                </span>
              </div>
            ))}
          </div>
        </>
      ) : null}
      {packet.dependencies.blockedOn.length > 0 ? (
        <>
          <h2>Blocked on</h2>
          <div className="packet-deps">
            {packet.dependencies.blockedOn.map((id) => (
              <div key={id} className="packet-dep blocked">
                <span className="dot" aria-hidden="true" />
                {id} · waiting approval
              </div>
            ))}
          </div>
        </>
      ) : null}
      <h2>
        Reviewer set{" "}
        {allReviewersGraded ? <span aria-hidden="true">·</span> : null}
      </h2>
      <div className="packet-deps">
        {packet.reviewers.map((r) => (
          <div
            key={r.slug}
            className={`packet-dep ${r.hasGraded ? "is-graded" : ""}`}
          >
            <span className="dot" aria-hidden="true" />
            <span style={{ flex: 1 }}>
              {r.slug}
              {r.isHuman ? " (you)" : ""}
            </span>
            <span
              style={{
                color: "var(--text-tertiary)",
                fontSize: 12,
              }}
            >
              {r.hasGraded ? "graded" : "—"}
            </span>
          </div>
        ))}
      </div>
    </nav>
  );
}

function ReviewerTimeoutBanner({ banner }: { banner: PacketBanner }) {
  const elapsed = banner.elapsed ?? "10m";
  return (
    <div className="packet-banner warning" role="status">
      <span className="banner-dot" aria-hidden="true" />
      <div>
        Reviewer <code>{banner.reviewerSlug}</code> timed out at {elapsed}.{" "}
        {banner.message}
      </div>
    </div>
  );
}

// ReviewerTimeoutForcedBanner is the screenshot/E2E forced-state banner.
// It does not require a banner object because the only consumer is the
// route layer's forceState path; the in-packet banner case is handled
// by ReviewerTimeoutBanner with a real PacketBanner payload.
function ReviewerTimeoutForcedBanner() {
  return (
    <div className="packet-banner warning" role="status">
      <span className="banner-dot" aria-hidden="true" />
      <div>
        At least one reviewer hit the convergence timeout. The Decision Packet
        is presented with their grade marked as skipped so a human can still
        resolve the task.
      </div>
    </div>
  );
}

function PersistenceBanner() {
  return (
    <div className="packet-banner error" role="alert">
      <span className="banner-dot" aria-hidden="true" />
      <div>
        Persistence error on this task — your changes are still in memory but
        not saved to disk yet. Fix the underlying issue (disk space,
        permissions) and the next transition will retry.
      </div>
    </div>
  );
}

function RegeneratedBanner() {
  return (
    <div className="packet-banner warning" role="status">
      <span className="banner-dot" aria-hidden="true" />
      <div>
        Packet regenerated from in-memory state. Some fields may be incomplete —
        verify before merging.
      </div>
    </div>
  );
}

function stateLabel(state: LifecycleState): string {
  return state.replace(/_/g, " ");
}

/** True when the keydown event came from a text input or carries a modifier. */
function shouldIgnoreShortcut(e: KeyboardEvent): boolean {
  if (e.metaKey || e.ctrlKey || e.altKey) return true;
  const target = e.target as HTMLElement | null;
  if (!target) return false;
  return (
    target.tagName === "INPUT" ||
    target.tagName === "TEXTAREA" ||
    target.isContentEditable
  );
}

function formatHoursAgo(iso: string): string {
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return "recently";
  const diffMs = Date.now() - then;
  const mins = Math.max(1, Math.round(diffMs / 60_000));
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.round(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.round(hours / 24);
  return `${days}d ago`;
}

interface DiscussionSectionProps {
  feedback: FeedbackItem[];
}

function DiscussionSection({ feedback }: DiscussionSectionProps) {
  return (
    <section
      className="packet-section packet-discussion"
      aria-label="Discussion"
      data-testid="packet-discussion"
    >
      <h3>
        Discussion{" "}
        <span className="count">
          {feedback.length} {feedback.length === 1 ? "comment" : "comments"}
        </span>
      </h3>
      {feedback.length === 0 ? (
        <p className="packet-discussion-empty">
          No comments yet. Leave one in the sidebar, request changes with
          inline feedback, or wait for the reviewer to post.
        </p>
      ) : (
        <ol className="packet-discussion-thread">
          {feedback.map((item, idx) => (
            <li
              key={`${item.appendedAt}-${idx}`}
              className="packet-discussion-item"
            >
              <header className="packet-discussion-header">
                <span className="packet-discussion-author">
                  @{item.author || "unknown"}
                </span>
                <span className="packet-discussion-time">
                  {formatHoursAgo(item.appendedAt)}
                </span>
              </header>
              <div className="packet-discussion-body">{item.body}</div>
            </li>
          ))}
        </ol>
      )}
    </section>
  );
}
