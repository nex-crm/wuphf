/**
 * IssueLifecycleCard — chat card emitted by the broker on important
 * lifecycle transitions of an Issue (Drafting → Running, → Done, → Closed,
 * → Needs your input, etc).
 *
 * The card is the "what happened to this Issue" surface for humans and
 * other agents scrolling the channel. Click → navigates to the Issue
 * detail.
 *
 * The broker sends Kind="issue_lifecycle" with a structured payload
 * carrying the transition shape ({task_id, title, owner, from_state,
 * to_state, transition}). This component switches on `transition` to
 * pick the right eyebrow / CTA / accent so the human can tell at a
 * glance whether work started, finished, was blocked, or needs them.
 */

import { router } from "../../../lib/router";

export type IssueLifecycleTransition =
  | "started"
  | "in_review"
  | "approved"
  | "rejected"
  | "blocked"
  | "needs_input"
  | "revising"
  | "generic";

export interface IssueLifecyclePayload {
  task_id?: string;
  title?: string;
  owner?: string;
  channel?: string;
  from_state?: string;
  to_state?: string;
  transition?: IssueLifecycleTransition;
  actor?: string;
}

function isStringField(value: unknown): value is string {
  return typeof value === "string" && value.length > 0;
}

const TRANSITIONS: ReadonlyArray<IssueLifecycleTransition> = [
  "started",
  "in_review",
  "approved",
  "rejected",
  "blocked",
  "needs_input",
  "revising",
  "generic",
];

function parseTransition(raw: unknown): IssueLifecycleTransition | undefined {
  if (typeof raw !== "string") return undefined;
  return (TRANSITIONS as ReadonlyArray<string>).includes(raw)
    ? (raw as IssueLifecycleTransition)
    : undefined;
}

export function parseIssueLifecyclePayload(raw: unknown): IssueLifecyclePayload {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) {
    return {};
  }
  const r = raw as Record<string, unknown>;
  const out: IssueLifecyclePayload = {};
  if (isStringField(r.task_id)) out.task_id = r.task_id;
  if (isStringField(r.title)) out.title = r.title;
  if (isStringField(r.owner)) out.owner = r.owner;
  if (isStringField(r.channel)) out.channel = r.channel;
  if (isStringField(r.from_state)) out.from_state = r.from_state;
  if (isStringField(r.to_state)) out.to_state = r.to_state;
  const transition = parseTransition(r.transition);
  if (transition) out.transition = transition;
  if (isStringField(r.actor)) out.actor = r.actor;
  return out;
}

interface TransitionPresentation {
  eyebrow: string;
  icon: string;
  accent: "go" | "review" | "done" | "stop" | "warn" | "neutral";
}

function presentationFor(
  transition: IssueLifecycleTransition,
  owner: string | undefined,
): TransitionPresentation {
  const tag = owner ? `@${owner}` : "the owner";
  switch (transition) {
    case "started":
      return {
        eyebrow: `Approved — ${tag} on it`,
        icon: "🚀",
        accent: "go",
      };
    case "in_review":
      return {
        eyebrow: `Ready for your review — submitted by ${tag}`,
        icon: "👀",
        accent: "review",
      };
    case "approved":
      return {
        eyebrow: `Done — ${tag} wrapped it`,
        icon: "✅",
        accent: "done",
      };
    case "rejected":
      return { eyebrow: "Closed", icon: "🚫", accent: "stop" };
    case "blocked":
      return {
        eyebrow: `Blocked — ${tag} can't proceed`,
        icon: "⏸",
        accent: "warn",
      };
    case "needs_input":
      return {
        eyebrow: `Needs your input — ${tag} is waiting`,
        icon: "❓",
        accent: "warn",
      };
    case "revising":
      return {
        eyebrow: `Revising — ${tag} is reworking`,
        icon: "✏️",
        accent: "review",
      };
    case "generic":
    default:
      return { eyebrow: "Issue updated", icon: "📋", accent: "neutral" };
  }
}

export interface IssueLifecycleCardProps {
  payload: IssueLifecyclePayload;
}

export function IssueLifecycleCard({ payload }: IssueLifecycleCardProps) {
  const taskId = payload.task_id ?? "";
  const title = payload.title ?? "(untitled issue)";
  const transition = payload.transition ?? "generic";
  const owner = payload.owner;
  const presentation = presentationFor(transition, owner);

  function openIssue() {
    if (!taskId) return;
    void router.navigate({
      to: "/issues/$issueId",
      params: { issueId: taskId },
    });
  }

  return (
    <button
      type="button"
      className={`issue-lifecycle-card issue-lifecycle-card--${presentation.accent}`}
      onClick={openIssue}
      data-testid="issue-lifecycle-card"
      data-task-id={taskId}
      data-transition={transition}
      aria-label={`Open issue ${taskId}: ${title}`}
      disabled={!taskId}
    >
      <span className="issue-lifecycle-card-icon" aria-hidden="true">
        {presentation.icon}
      </span>
      <span className="issue-lifecycle-card-body">
        <span className="issue-lifecycle-card-eyebrow">
          {presentation.eyebrow}
          {taskId ? (
            <span className="issue-lifecycle-card-id"> · {taskId}</span>
          ) : null}
        </span>
        <span className="issue-lifecycle-card-title">{title}</span>
        {payload.from_state && payload.to_state ? (
          <span className="issue-lifecycle-card-meta">
            {payload.from_state} → {payload.to_state}
          </span>
        ) : null}
      </span>
      <span className="issue-lifecycle-card-cta" aria-hidden="true">
        Open →
      </span>
    </button>
  );
}
