import { useEffect, useState } from "react";

import type { IssueComment } from "./IssueDocument";
import { CommentsTimeline } from "./IssueDocument";
import { IssueActivityFeed } from "./IssueActivityFeed";
import { SubIssuesList } from "./SubIssuesList";

// ── Issue detail tabs ────────────────────────────────────────────────

type IssueDetailTab = "activity" | "comments" | "sub-issues";

interface IssueDetailTabsProps {
  taskId: string;
  channel: string;
  comments: IssueComment[];
  isDrafting: boolean;
  showSubIssues: boolean;
  timelineRef: React.RefObject<HTMLDivElement | null>;
  onCommentPosted: () => void;
}

/**
 * Linear/Paperclip-shaped tab strip below the description: Activity (the
 * default), Comments, Sub-Issues. The activity feed is the "what's
 * happened" view; Comments is the discussion thread; Sub-Issues hosts the
 * breakdown. Sub-Issues tab is hidden for sub-issues themselves (no
 * sub-sub-issues), matching the prior single-flat-page rule.
 */
export function IssueDetailTabs({
  taskId,
  channel,
  comments,
  isDrafting,
  showSubIssues,
  timelineRef,
  onCommentPosted,
}: IssueDetailTabsProps) {
  const [tab, setTab] = useState<IssueDetailTab>("activity");
  // If Sub-issues becomes unavailable (e.g. the issue was just promoted
  // to a child of another issue), snap the active tab back to Activity
  // so the panel does not render empty.
  useEffect(() => {
    if (!showSubIssues && tab === "sub-issues") {
      setTab("activity");
    }
  }, [showSubIssues, tab]);
  const commentCount = comments.length;

  return (
    <div className="issue-doc-tabs">
      <div className="issue-doc-tabs-strip" role="tablist" aria-label="Issue detail">
        <TabButton
          active={tab === "activity"}
          onClick={() => setTab("activity")}
          label="Activity"
        />
        <TabButton
          active={tab === "comments"}
          onClick={() => setTab("comments")}
          label="Comments"
          count={commentCount}
        />
        {showSubIssues ? (
          <TabButton
            active={tab === "sub-issues"}
            onClick={() => setTab("sub-issues")}
            label="Sub-issues"
          />
        ) : null}
      </div>

      <div className="issue-doc-tabs-panel" role="tabpanel">
        {tab === "activity" ? <IssueActivityFeed taskId={taskId} /> : null}
        {tab === "comments" ? (
          <CommentsTimeline
            taskId={taskId}
            channel={channel}
            comments={comments}
            isDrafting={isDrafting}
            timelineRef={timelineRef}
            onCommentPosted={onCommentPosted}
          />
        ) : null}
        {tab === "sub-issues" && showSubIssues ? (
          <SubIssuesList taskId={taskId} channel={channel} />
        ) : null}
      </div>
    </div>
  );
}

interface TabButtonProps {
  active: boolean;
  onClick: () => void;
  label: string;
  count?: number;
}

function TabButton({ active, onClick, label, count }: TabButtonProps) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      className={`issue-doc-tab${active ? " issue-doc-tab--active" : ""}`}
      onClick={onClick}
    >
      <span>{label}</span>
      {typeof count === "number" && count > 0 ? (
        <span className="issue-doc-tab-count">{count}</span>
      ) : null}
    </button>
  );
}
