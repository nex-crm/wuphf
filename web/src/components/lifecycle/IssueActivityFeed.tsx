import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ArrowRight,
  CheckCircle,
  ChatLines,
  GitFork,
  HelpCircle,
  Refresh,
  Xmark,
} from "iconoir-react";

import {
  getIssueActivity,
  type IssueActivityEvent,
  type IssueActivityEventKind,
} from "../../api/tasks";
import { router } from "../../lib/router";

interface IssueActivityFeedProps {
  taskId: string;
}

/**
 * Activity tab content — Paperclip-style chronological feed of everything
 * that happened to this Issue. Mounted under the IssueDocument's Activity
 * tab. Sources:
 *   - lifecycle transitions (officeActionLog kind=lifecycle_*)
 *   - workflow actions (task_created, task_updated, …)
 *   - comments tied to this Issue
 *   - human_interview requests with their resolution
 *   - sub-issue creations
 *
 * Open requests are clickable — they deep-link into the Inbox so the
 * human can answer without leaving the Activity view.
 */
export function IssueActivityFeed({ taskId }: IssueActivityFeedProps) {
  const { data, isLoading, isError, refetch, isFetching } = useQuery({
    queryKey: ["issue", taskId, "activity"],
    queryFn: () => getIssueActivity(taskId),
    refetchInterval: 8_000,
    staleTime: 4_000,
  });

  // Newest events first — the broker returns oldest → newest for stable
  // server-side rendering; reverse here so the most recent activity is
  // visible at the top.
  const events = useMemo(() => {
    const list = data?.events ?? [];
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
          No activity yet. Events appear here as the issue moves through
          its lifecycle.
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

function ActivityRow({ event }: { event: IssueActivityEvent }) {
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
      </div>
    </li>
  );
}

function RequestResolution({
  req,
}: {
  req: NonNullable<IssueActivityEvent["request"]>;
}) {
  if (req.status === "answered") {
    const answer = req.custom_text?.trim() || req.choice_text?.trim() || req.choice_id;
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

function iconForKind(kind: IssueActivityEventKind) {
  switch (kind) {
    case "lifecycle":
      return <ArrowRight width={14} height={14} aria-hidden="true" />;
    case "comment":
      return <ChatLines width={14} height={14} aria-hidden="true" />;
    case "request":
      return <HelpCircle width={14} height={14} aria-hidden="true" />;
    case "sub_issue":
      return <GitFork width={14} height={14} aria-hidden="true" />;
    case "action":
    default:
      return <CheckCircle width={14} height={14} aria-hidden="true" />;
  }
}

function verbForEvent(event: IssueActivityEvent): string {
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
      return "added a sub-issue";
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
