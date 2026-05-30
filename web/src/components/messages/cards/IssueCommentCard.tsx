/**
 * IssueCommentCard — chat card emitted when a human (or agent) leaves a
 * PR-style comment on an Issue via POST /tasks/{id}/comment.
 *
 * The broker posts a system-authored message (Kind="issue_comment") into
 * the channel where the Issue lives. Without a dedicated card, that
 * message rendered as a plain markdown chat bubble — which made the
 * comment text look like a direct chat ask, prompting the woken agent
 * to act on it rather than reply to the thread on the Issue.
 *
 * This card surfaces the comment as a clear "this happened on Issue X"
 * affordance with a Read & Reply CTA that routes to the Issue detail
 * view (where the activity feed is the canonical reply thread).
 *
 * Security: payload fields are plain text only. The broker-side
 * sanitizer is authoritative; this component is defense-in-depth.
 */

import { router } from "../../../lib/router";

export interface IssueCommentPayload {
  task_id?: string;
  title?: string;
  owner?: string;
  channel?: string;
  lifecycle_state?: string;
  author?: string;
  excerpt?: string;
}

function isStringField(value: unknown): value is string {
  return typeof value === "string" && value.length > 0;
}

export function parseIssueCommentPayload(raw: unknown): IssueCommentPayload {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) {
    return {};
  }
  const r = raw as Record<string, unknown>;
  const out: IssueCommentPayload = {};
  if (isStringField(r.task_id)) out.task_id = r.task_id;
  if (isStringField(r.title)) out.title = r.title;
  if (isStringField(r.owner)) out.owner = r.owner;
  if (isStringField(r.channel)) out.channel = r.channel;
  if (isStringField(r.lifecycle_state))
    out.lifecycle_state = r.lifecycle_state;
  if (isStringField(r.author)) out.author = r.author;
  if (isStringField(r.excerpt)) out.excerpt = r.excerpt;
  return out;
}

export interface IssueCommentCardProps {
  payload: IssueCommentPayload;
}

export function IssueCommentCard({ payload }: IssueCommentCardProps) {
  const taskId = payload.task_id ?? "";
  const title = payload.title ?? "(untitled issue)";
  const author = payload.author ?? "Human";
  const excerpt = payload.excerpt ?? "";
  const state = payload.lifecycle_state;
  const isDrafting = state === "drafting";

  function openIssue() {
    if (!taskId) return;
    void router.navigate({
      to: "/issues/$issueId",
      params: { issueId: taskId },
    });
  }

  const eyebrow = isDrafting
    ? `Comment on Drafting issue · @${author}`
    : `Comment on issue · @${author}`;

  return (
    <button
      type="button"
      className="issue-comment-card"
      onClick={openIssue}
      data-testid="issue-comment-card"
      data-task-id={taskId}
      data-lifecycle-state={state ?? ""}
      aria-label={`Open issue ${taskId}: ${title}`}
      disabled={!taskId}
    >
      <span className="issue-comment-card-icon" aria-hidden="true">
        💬
      </span>
      <span className="issue-comment-card-body">
        <span className="issue-comment-card-eyebrow">
          {eyebrow}
          {taskId ? (
            <span className="issue-comment-card-id"> · {taskId}</span>
          ) : null}
        </span>
        <span className="issue-comment-card-title">{title}</span>
        {excerpt ? (
          <span className="issue-comment-card-excerpt">{excerpt}</span>
        ) : null}
      </span>
      <span className="issue-comment-card-cta" aria-hidden="true">
        Read & Reply →
      </span>
    </button>
  );
}
