import { useEffect, useState } from "react";

import { SubTasksList } from "./SubTasksList";
import { TaskActivityFeed } from "./TaskActivityFeed";
import type { TaskComment } from "./TaskDocument";
import { CommentsTimeline } from "./TaskDocument";

// ── Task detail tabs ─────────────────────────────────────────────────

type TaskDetailTab = "activity" | "comments" | "sub-issues";

interface TaskDetailTabsProps {
  taskId: string;
  channel: string;
  comments: TaskComment[];
  isDrafting: boolean;
  showSubTasks: boolean;
  timelineRef: React.RefObject<HTMLDivElement | null>;
  onCommentPosted: () => void;
}

/**
 * Linear/Paperclip-shaped tab strip below the description: Activity (the
 * default), Comments, Sub-Tasks. The activity feed is the "what's
 * happened" view; Comments is the discussion thread; Sub-Tasks hosts the
 * breakdown. Sub-Tasks tab is hidden for sub-tasks themselves (no
 * sub-sub-tasks), matching the prior single-flat-page rule.
 */
export function TaskDetailTabs({
  taskId,
  channel,
  comments,
  isDrafting,
  showSubTasks,
  timelineRef,
  onCommentPosted,
}: TaskDetailTabsProps) {
  const [tab, setTab] = useState<TaskDetailTab>("activity");
  // If Sub-tasks becomes unavailable (e.g. the task was just promoted
  // to a child of another task), snap the active tab back to Activity
  // so the panel does not render empty.
  useEffect(() => {
    if (!showSubTasks && tab === "sub-issues") {
      setTab("activity");
    }
  }, [showSubTasks, tab]);
  const commentCount = comments.length;

  return (
    <div className="issue-doc-tabs">
      <div
        className="issue-doc-tabs-strip"
        role="tablist"
        aria-label="Task detail"
      >
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
        {showSubTasks ? (
          <TabButton
            active={tab === "sub-issues"}
            onClick={() => setTab("sub-issues")}
            label="Sub-tasks"
          />
        ) : null}
      </div>

      <div className="issue-doc-tabs-panel" role="tabpanel">
        {tab === "activity" ? <TaskActivityFeed taskId={taskId} /> : null}
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
        {tab === "sub-issues" && showSubTasks ? (
          <SubTasksList taskId={taskId} channel={channel} />
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
