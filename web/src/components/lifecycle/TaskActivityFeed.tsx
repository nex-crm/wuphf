import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ArrowRight,
  ChatLines,
  CheckCircle,
  GitFork,
  HelpCircle,
  Refresh,
  Xmark,
} from "iconoir-react";

import {
  getTaskActivity,
  type TaskActivityEvent,
  type TaskActivityEventKind,
} from "../../api/tasks";
import { router } from "../../lib/router";

interface TaskActivityFeedProps {
  taskId: string;
}

/**
 * Activity is a structured audit of an Issue's state changes — NOT a second
 * copy of the conversation. It surfaces only:
 *   - lifecycle transitions (officeActionLog kind=lifecycle_*)
 *   - human_interview requests with their resolution
 *   - sub-issue creations
 *
 * Comments are deliberately excluded — they render in the chat stream
 * (TaskCommentCard) which is the canonical reply thread, so duplicating
 * them here would just be the chat again. Generic `action` log entries are
 * excluded too; only the three state-event kinds above belong in the audit.
 *
 * Open requests are clickable — they deep-link into the Inbox so the
 * human can answer without leaving the Activity view.
 */
const FEED_KINDS: ReadonlySet<TaskActivityEventKind> = new Set([
  "lifecycle",
  "request",
  "sub_issue",
  "turn",
]);

export function TaskActivityFeed({ taskId }: TaskActivityFeedProps) {
  const { data, isLoading, isError, refetch, isFetching } = useQuery({
    queryKey: ["issue", taskId, "activity"],
    queryFn: () => getTaskActivity(taskId),
    refetchInterval: 8_000,
    staleTime: 4_000,
  });

  // Keep only state-event kinds (lifecycle / request / sub-issue), then show
  // newest first — the broker returns oldest → newest for stable server-side
  // rendering, so reverse here for most-recent-on-top.
  const events = useMemo(() => {
    const list = (data?.events ?? []).filter((ev) => FEED_KINDS.has(ev.kind));
    return [...list].reverse();
  }, [data]);

  if (isLoading) {
    return (
      <div className="issue-activity-feed">
        <p className="issue-activity-feed-empty">Loading activity…</p>
      </div>
    );
  }
  if (isError) {
    return (
      <div className="issue-activity-feed">
        <p className="issue-activity-feed-empty issue-activity-feed-empty--error">
          Could not load activity.
          <button
            type="button"
            className="issue-activity-feed-retry"
            onClick={() => void refetch()}
          >
            <Refresh width={12} height={12} aria-hidden="true" /> Retry
          </button>
        </p>
      </div>
    );
  }
  if (events.length === 0) {
    return (
      <div className="issue-activity-feed">
        <p className="issue-activity-feed-empty">
          No activity yet. Events appear here as the issue moves through its
          lifecycle.
        </p>
      </div>
    );
  }

  return (
    <div className="issue-activity-feed" data-fetching={isFetching}>
      <ul className="issue-activity-feed-list">
        {events.map((ev) => (
          <ActivityRow key={ev.id} event={ev} />
        ))}
      </ul>
    </div>
  );
}

function ActivityRow({ event }: { event: TaskActivityEvent }) {
  const icon = iconForKind(event.kind);
  const verb = verbForEvent(event);
  const isOpenRequest =
    event.kind === "request" && event.request?.status === "open";

  const handleClick = () => {
    if (!isOpenRequest) return;
    void router.navigate({ to: "/inbox" });
  };

  return (
    <li
      className={`issue-activity-feed-row${isOpenRequest ? " issue-activity-feed-row--clickable" : ""}`}
      onClick={isOpenRequest ? handleClick : undefined}
      onKeyDown={
        isOpenRequest
          ? (e) => {
              if (e.key === "Enter" || e.key === " ") {
                e.preventDefault();
                handleClick();
              }
            }
          : undefined
      }
      role={isOpenRequest ? "button" : undefined}
      tabIndex={isOpenRequest ? 0 : undefined}
    >
      <span
        className={`issue-activity-feed-icon issue-activity-feed-icon--${event.kind}`}
        aria-hidden="true"
      >
        {icon}
      </span>
      <div className="issue-activity-feed-body">
        <div className="issue-activity-feed-row-head">
          {event.actor ? (
            <span className="issue-activity-feed-actor">@{event.actor}</span>
          ) : (
            <span className="issue-activity-feed-actor issue-activity-feed-actor--system">
              system
            </span>
          )}
          <span className="issue-activity-feed-verb">{verb}</span>
          <time className="issue-activity-feed-time">
            {formatTimestamp(event.timestamp)}
          </time>
        </div>
        {event.kind === "lifecycle" && event.lifecycle ? (
          <div className="issue-activity-feed-lifecycle">
            <span className="issue-activity-feed-state">
              {event.lifecycle.from || "—"}
            </span>
            <ArrowRight width={12} height={12} aria-hidden="true" />
            <span className="issue-activity-feed-state">
              {event.lifecycle.to || "—"}
            </span>
          </div>
        ) : null}
        {event.summary ? (
          <p className="issue-activity-feed-summary">{event.summary}</p>
        ) : null}
        {event.kind === "request" && event.request ? (
          <RequestResolution req={event.request} />
        ) : null}
        {event.kind === "turn" ? (
          <TurnContextList items={event.context_used} />
        ) : null}
      </div>
    </li>
  );
}

/**
 * B4 context transparency: the knowledge-item ids a turn's work packet
 * injected ("learning:<id>", "wiki:<ref>", ...), recorded deterministically
 * at packet-build time and surfaced under the turn's activity entry.
 */
function TurnContextList({ items }: { items?: string[] }) {
  if (!items || items.length === 0) {
    return null;
  }
  return (
    <p className="issue-activity-feed-context">context: {items.join(", ")}</p>
  );
}

function RequestResolution({
  req,
}: {
  req: NonNullable<TaskActivityEvent["request"]>;
}) {
  if (req.status === "answered") {
    const answer =
      req.custom_text?.trim() || req.choice_text?.trim() || req.choice_id;
    return (
      <div className="issue-activity-feed-resolution issue-activity-feed-resolution--answered">
        <CheckCircle width={12} height={12} aria-hidden="true" />
        <span>Answered: {answer || "—"}</span>
      </div>
    );
  }
  if (req.status === "canceled") {
    return (
      <div className="issue-activity-feed-resolution issue-activity-feed-resolution--canceled">
        <Xmark width={12} height={12} aria-hidden="true" />
        <span>Canceled</span>
      </div>
    );
  }
  // Open — clickable into Inbox.
  return (
    <div className="issue-activity-feed-resolution issue-activity-feed-resolution--open">
      <HelpCircle width={12} height={12} aria-hidden="true" />
      <span>Open — answer in Inbox →</span>
    </div>
  );
}

function iconForKind(kind: TaskActivityEventKind) {
  switch (kind) {
    case "lifecycle":
      return <ArrowRight width={14} height={14} aria-hidden="true" />;
    case "comment":
      return <ChatLines width={14} height={14} aria-hidden="true" />;
    case "request":
      return <HelpCircle width={14} height={14} aria-hidden="true" />;
    case "sub_issue":
      return <GitFork width={14} height={14} aria-hidden="true" />;
    case "turn":
      return <Refresh width={14} height={14} aria-hidden="true" />;
    case "action":
    default:
      return <CheckCircle width={14} height={14} aria-hidden="true" />;
  }
}

function verbForEvent(event: TaskActivityEvent): string {
  switch (event.kind) {
    case "lifecycle":
      return "moved state";
    case "comment":
      return "commented";
    case "request":
      if (event.request?.status === "answered") return "request answered";
      if (event.request?.status === "canceled") return "request canceled";
      return "asked";
    case "sub_issue":
      return "added a sub-task";
    case "turn":
      return "ran a turn";
    case "action":
    default:
      return event.summary || "took action";
  }
}

function formatTimestamp(ts: string): string {
  if (!ts) return "";
  const ms = Date.parse(ts);
  if (Number.isNaN(ms)) return ts;
  const delta = Date.now() - ms;
  const sec = Math.floor(delta / 1000);
  if (sec < 5) return "just now";
  if (sec < 60) return `${sec}s ago`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min}m ago`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr}h ago`;
  const days = Math.floor(hr / 24);
  if (days < 7) return `${days}d ago`;
  return new Date(ms).toLocaleDateString();
}
