import { useEffect, useState } from "react";

import { SubTasksList } from "./SubTasksList";
import { TaskActivityFeed } from "./TaskActivityFeed";
import { TaskChannelChat } from "./TaskChannelChat";
import { TaskDescription } from "./TaskDescription";

// ── Task detail tabs ─────────────────────────────────────────────────

type TaskDetailTab = "channel" | "spec" | "activity" | "sub-issues";

interface TaskDetailTabsProps {
  taskId: string;
  channel: string;
  /** Linear-style task body (the spec). Rendered in the Spec tab. */
  description: string;
  isDrafting: boolean;
  showSubTasks: boolean;
  onCommentPosted: () => void;
}

/**
 * Task detail tab strip: Channel · Spec · Activity (+ Sub-tasks when the
 * task has children). This mirrors the task-scoped model — every task has
 * its own channel (the Channel tab is the conversation where the owner,
 * CEO, and human collaborate), a spec (the body the team agreed on before
 * execution), and an activity feed (what's happened).
 *
 * Default tab is spec-first while the task is still drafting (you are
 * reviewing/approving the spec); once it is moving, the Channel is the
 * live working surface so it leads.
 */
export function TaskDetailTabs({
  taskId,
  channel,
  description,
  isDrafting,
  showSubTasks,
  onCommentPosted,
}: TaskDetailTabsProps) {
  const [tab, setTab] = useState<TaskDetailTab>(
    isDrafting ? "spec" : "channel",
  );
  // If Sub-tasks becomes unavailable (e.g. the task was just promoted to
  // a child of another task), snap the active tab back to Channel so the
  // panel does not render empty.
  useEffect(() => {
    if (!showSubTasks && tab === "sub-issues") {
      setTab("channel");
    }
  }, [showSubTasks, tab]);

  return (
    <div className="issue-doc-tabs">
      <div
        className="issue-doc-tabs-strip"
        role="tablist"
        aria-label="Task detail"
      >
        <TabButton
          active={tab === "channel"}
          onClick={() => setTab("channel")}
          label="Channel"
        />
        <TabButton
          active={tab === "spec"}
          onClick={() => setTab("spec")}
          label="Spec"
        />
        <TabButton
          active={tab === "activity"}
          onClick={() => setTab("activity")}
          label="Activity"
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
        {tab === "channel" ? (
          <TaskChannelChat
            taskId={taskId}
            channel={channel}
            onCommentPosted={onCommentPosted}
          />
        ) : null}
        {tab === "spec" ? (
          <TaskDescription description={description} isDrafting={isDrafting} />
        ) : null}
        {tab === "activity" ? <TaskActivityFeed taskId={taskId} /> : null}
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
