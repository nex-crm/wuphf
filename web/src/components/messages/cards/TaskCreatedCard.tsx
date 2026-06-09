/**
 * TaskCreatedCard — chat card emitted when a Task is filed via
 * team_task action=create with task_type=issue.
 *
 * The broker posts a system-authored message (Kind="issue_created") into
 * the channel where the Task lands. This card is the visual surface for
 * that message: a clickable banner-style card with the task id, title,
 * owner, and lifecycle state. Click → navigates to /tasks/$taskId.
 *
 * Why a card and not a plain agent message: RULE ZERO means every piece
 * of work has a Task behind it. The card is the audit-trail anchor
 * humans (and other agents) latch onto when scrolling the channel later.
 *
 * Security: payload fields are plain text only. The broker-side
 * sanitizer is authoritative; this component is defense-in-depth.
 */

import { router } from "../../../lib/router";

export interface TaskCreatedPayload {
  task_id?: string;
  title?: string;
  owner?: string;
  channel?: string;
  lifecycle_state?: string;
  created_by?: string;
}

function isStringField(value: unknown): value is string {
  return typeof value === "string" && value.length > 0;
}

export function parseTaskCreatedPayload(raw: unknown): TaskCreatedPayload {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) {
    return {};
  }
  const r = raw as Record<string, unknown>;
  const out: TaskCreatedPayload = {};
  if (isStringField(r.task_id)) out.task_id = r.task_id;
  if (isStringField(r.title)) out.title = r.title;
  if (isStringField(r.owner)) out.owner = r.owner;
  if (isStringField(r.channel)) out.channel = r.channel;
  if (isStringField(r.lifecycle_state)) out.lifecycle_state = r.lifecycle_state;
  if (isStringField(r.created_by)) out.created_by = r.created_by;
  return out;
}

export interface TaskCreatedCardProps {
  payload: TaskCreatedPayload;
}

export function TaskCreatedCard({ payload }: TaskCreatedCardProps) {
  const taskId = payload.task_id ?? "";
  const title = payload.title ?? "(untitled task)";
  const owner = payload.owner;
  const state = payload.lifecycle_state;
  const isDrafting = state === "drafting";

  function openTask() {
    if (!taskId) return;
    void router.navigate({
      to: "/tasks/$taskId",
      params: { taskId },
    });
  }

  // CTA + eyebrow shift when the Task is awaiting human approval —
  // this is the most common state immediately after creation and the
  // human is the gate. Other states render with a neutral "Open" CTA.
  const eyebrow = isDrafting ? "Task ready for review" : "Task created";
  const cta = isDrafting ? "Review & Approve →" : "Open →";
  const helpText = isDrafting
    ? "Awaiting your approval — open to read, comment, or edit before starting."
    : null;

  return (
    <button
      type="button"
      className={
        "issue-created-card" +
        (isDrafting ? " issue-created-card--awaiting-approval" : "")
      }
      onClick={openTask}
      data-testid="issue-created-card"
      data-task-id={taskId}
      data-lifecycle-state={state ?? ""}
      aria-label={`Open task ${taskId}: ${title}`}
      disabled={!taskId}
    >
      <span className="issue-created-card-icon" aria-hidden="true">
        📋
      </span>
      <span className="issue-created-card-body">
        <span className="issue-created-card-eyebrow">
          {eyebrow}
          {taskId ? (
            <span className="issue-created-card-id"> · {taskId}</span>
          ) : null}
        </span>
        <span className="issue-created-card-title">{title}</span>
        {helpText ? (
          <span className="issue-created-card-help">{helpText}</span>
        ) : (
          (owner || state) && (
            <span className="issue-created-card-meta">
              {owner ? <span>owner @{owner}</span> : null}
              {owner && state ? <span aria-hidden="true"> · </span> : null}
              {state ? <span>state {state}</span> : null}
            </span>
          )
        )}
      </span>
      <span className="issue-created-card-cta" aria-hidden="true">
        {cta}
      </span>
    </button>
  );
}
