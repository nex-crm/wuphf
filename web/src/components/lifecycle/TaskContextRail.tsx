import { type ReactNode, useState } from "react";

import type { TaskVerification } from "../../api/tasks";
import { ChannelParticipants } from "../messages/ChannelParticipants";
import { SubTasksList } from "./SubTasksList";
import { TaskActivityFeed } from "./TaskActivityFeed";
import { TaskDescription } from "./TaskDescription";

interface TaskContextRailProps {
  taskId: string;
  channel: string;
  /** Linear-style task body (task.details). */
  description: string;
  isDrafting: boolean;
  showSubTasks: boolean;
  /** Machine-checkable definition of done (U1); renders atop the Details. */
  verification?: TaskVerification;
}

interface RailSectionProps {
  title: string;
  defaultOpen?: boolean;
  testId?: string;
  children: ReactNode;
}

function RailSection({
  title,
  defaultOpen = true,
  testId,
  children,
}: RailSectionProps) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <section
      className={`task-rail-section${open ? " is-open" : ""}`}
      data-testid={testId}
    >
      <button
        type="button"
        className="task-rail-section-header"
        aria-expanded={open}
        onClick={() => setOpen((o) => !o)}
      >
        <span className="task-rail-caret" aria-hidden="true">
          {open ? "▾" : "▸"}
        </span>
        <span className="task-rail-section-title">{title}</span>
      </button>
      {open ? <div className="task-rail-section-body">{children}</div> : null}
    </section>
  );
}

/**
 * TaskContextRail — the right-hand details panel of the task detail view.
 *
 * In the chat-primary layout the conversation owns the main column; the
 * secondary context (who's in the channel, the task description, the
 * activity log, sub-tasks) lives here so it's a glance away without
 * stealing space from the chat. Participants lead (the owner's live status
 * shows there, single source — the header names the owner), then
 * collapsible sections. Details opens by default while drafting (you're
 * reviewing it); once running, the chat leads and the rail sections start
 * collapsed.
 */
export function TaskContextRail({
  taskId,
  channel,
  description,
  isDrafting,
  showSubTasks,
  verification,
}: TaskContextRailProps) {
  const hasCheck = Boolean(verification && verification.kind !== "none");
  return (
    <aside className="task-context-rail" aria-label="Task details">
      {/* ChannelParticipants carries its own "Participants" header + add
       *  control, so it renders directly (not inside a RailSection) to avoid
       *  a duplicate header. It stays always-visible — who's in the channel
       *  is the most-glanced context. */}
      <div
        className="task-rail-participants"
        data-testid="task-rail-participants"
      >
        <ChannelParticipants channelSlug={channel} />
      </div>
      <RailSection
        title="Details"
        defaultOpen={isDrafting}
        testId="task-rail-details"
      >
        {hasCheck && verification ? (
          <div
            className="task-verification-dod"
            data-testid="task-verification-dod"
          >
            <span className="task-verification-dod-label">
              Definition of done
              {verification.required ? " (required)" : ""}
            </span>
            <code className="task-verification-dod-spec">
              {verification.spec
                ? `${verification.kind}: ${verification.spec}`
                : verification.kind}
            </code>
          </div>
        ) : null}
        <TaskDescription description={description} isDrafting={isDrafting} />
      </RailSection>
      <RailSection
        title="Activity"
        defaultOpen={false}
        testId="task-rail-activity"
      >
        <TaskActivityFeed taskId={taskId} />
      </RailSection>
      {showSubTasks ? (
        <RailSection
          title="Sub-tasks"
          defaultOpen={false}
          testId="task-rail-subtasks"
        >
          <SubTasksList taskId={taskId} channel={channel} />
        </RailSection>
      ) : null}
    </aside>
  );
}
