/**
 * TaskDocument — Task detail surface.
 *
 * Chat-primary layout: the task's channel conversation owns the main
 * column; the secondary context (participants, description, activity,
 * sub-tasks) lives in the right rail (TaskContextRail).
 *
 * The header carries title + lifecycle pill + verification badge and the
 * lifecycle action toolbar (a Start affordance while parked/drafting, the
 * PR-style loop otherwise). The task's understanding lives in its title +
 * description (Details) — there is no spec document (core-loop R2).
 */

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { get } from "../../api/client";
import { postDecision, postTaskReject } from "../../api/lifecycle";
import {
  getOfficeTasks,
  type Task,
  type TaskDefinition as TaskDefinitionShape,
  type TaskDeliverable,
  type TaskVerification,
  type TaskVerificationResult,
  taskToLifecycleState,
} from "../../api/tasks";
import { formatTaskTitleForDisplay } from "../../lib/taskTitle";
import type { LifecycleState } from "../../lib/types/lifecycle";
import { LifecycleStatePill } from "./LifecycleStatePill";
import { OwnerPicker } from "./OwnerPicker";
import { ParentTaskBreadcrumb } from "./ParentTaskBreadcrumb";
import { TaskActionToolbar } from "./TaskActionToolbar";
import { TaskChannelChat } from "./TaskChannelChat";
import { TaskContextRail } from "./TaskContextRail";
import { VerificationBadge } from "./VerificationBadge";

// ── Types ──────────────────────────────────────────────────────────────

/**
 * Full Issue document payload.
 * Fetched from GET /tasks/<taskId>. Fields mirror the broker's `teamTask`
 * JSON shape (camelCase on the wire from the Go side).
 */
export interface TaskDocument {
  taskId: string;
  title: string;
  /** Plain-markdown description (task.details from the broker).
   * Linear-style: just the body — the whole brief is title + description. */
  description: string;
  lifecycleState: LifecycleState;
  channel: string;
  ownerSlug?: string;
  parentTaskId?: string;
  createdAt?: string;
  updatedAt?: string;
  /** Machine-checkable definition of done (U1). Absent on legacy tasks. */
  verification?: TaskVerification;
  /** Outcome of the most recent verification run. Absent until first run. */
  verificationResult?: TaskVerificationResult;
  /** Structured intake contract (R4). Absent until the CEO/human defines. */
  definition?: TaskDefinitionShape;
}

// ── Helpers ────────────────────────────────────────────────────────────

/** Read a string from an object field or its snake_case alias. */
function strField(
  r: Record<string, unknown>,
  camel: string,
  snake?: string,
): string | undefined {
  const v = r[camel];
  if (typeof v === "string") return v;
  if (snake) {
    const sv = r[snake];
    if (typeof sv === "string") return sv;
  }
  return undefined;
}

/** Narrow an unknown value to a record. */
function recordValue(value: unknown): Record<string, unknown> | undefined {
  return value && typeof value === "object"
    ? (value as Record<string, unknown>)
    : undefined;
}

/**
 * Narrow an unknown wire value into a TaskVerification. The broker emits
 * `{kind, spec?, required?}`; anything without a string `kind` is treated
 * as absent so a malformed payload degrades to "Unverified" rather than
 * crashing the document.
 */
function normalizeVerification(value: unknown): TaskVerification | undefined {
  const rec = recordValue(value);
  if (!rec || typeof rec.kind !== "string" || rec.kind === "") {
    return undefined;
  }
  return {
    kind: rec.kind,
    spec: typeof rec.spec === "string" ? rec.spec : undefined,
    required: typeof rec.required === "boolean" ? rec.required : undefined,
  };
}

/**
 * Narrow an unknown wire value into a TaskVerificationResult. `pass` is
 * the load-bearing field; a payload without a boolean `pass` is treated
 * as "no result yet".
 */
function normalizeVerificationResult(
  value: unknown,
): TaskVerificationResult | undefined {
  const rec = recordValue(value);
  if (!rec || typeof rec.pass !== "boolean") return undefined;
  return {
    pass: rec.pass,
    kind: typeof rec.kind === "string" ? rec.kind : "",
    detail: typeof rec.detail === "string" ? rec.detail : undefined,
    checked_at: typeof rec.checked_at === "string" ? rec.checked_at : "",
  };
}

/** Keep only non-empty strings from an unknown wire array. */
function stringArray(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  return value.filter(
    (item): item is string => typeof item === "string" && item !== "",
  );
}

/** Narrow an unknown value to a non-empty string, else undefined. */
function nonEmptyString(value: unknown): string | undefined {
  return typeof value === "string" && value !== "" ? value : undefined;
}

/** Narrow an unknown wire array into well-formed deliverables. Entries
 *  without a non-empty string `name` are dropped. */
function normalizeDeliverables(value: unknown): TaskDeliverable[] {
  if (!Array.isArray(value)) return [];
  const out: TaskDeliverable[] = [];
  for (const item of value) {
    const d = recordValue(item);
    const name = d ? nonEmptyString(d.name) : undefined;
    if (!(d && name)) continue;
    out.push({ name, format: nonEmptyString(d.format) });
  }
  return out;
}

/**
 * Narrow an unknown wire value into a TaskDefinition (R4). `goal` is the
 * load-bearing field; a payload without a non-empty string goal is treated
 * as absent so a malformed definition degrades to the plain description.
 */
function normalizeTaskDefinition(
  value: unknown,
): TaskDefinitionShape | undefined {
  const rec = recordValue(value);
  const goal = rec ? nonEmptyString(rec.goal) : undefined;
  if (!(rec && goal)) {
    return undefined;
  }
  const deliverables = normalizeDeliverables(rec.deliverables);
  const criteria = stringArray(rec.success_criteria);
  const access = stringArray(rec.access_needed);
  return {
    goal,
    deliverables: deliverables.length > 0 ? deliverables : undefined,
    success_criteria: criteria.length > 0 ? criteria : undefined,
    access_needed: access.length > 0 ? access : undefined,
    defined_at: nonEmptyString(rec.defined_at),
  };
}

function resolveTaskId(
  packet: Record<string, unknown>,
  taskRecord: Record<string, unknown> | undefined,
  taskHint: Task | undefined,
): string {
  return (
    strField(packet, "taskId", "id") ??
    (taskRecord ? strField(taskRecord, "taskId", "id") : undefined) ??
    taskHint?.id ??
    ""
  );
}

function resolveTaskTitle(
  packet: Record<string, unknown>,
  taskRecord: Record<string, unknown> | undefined,
  taskHint: Task | undefined,
  taskId: string,
): string {
  const fallbackTitle = taskId || "(untitled)";
  return (
    strField(packet, "title") ??
    (taskRecord ? strField(taskRecord, "title") : undefined) ??
    taskHint?.title ??
    fallbackTitle
  );
}

function resolveTaskLifecycleState(
  packet: Record<string, unknown>,
  taskHint: Task | undefined,
): LifecycleState {
  const rawState = strField(packet, "lifecycleState", "lifecycle_state");
  return rawState
    ? (rawState as LifecycleState)
    : taskToLifecycleState(taskHint);
}

function resolveAliasedField(
  packet: Record<string, unknown>,
  taskRecord: Record<string, unknown> | undefined,
  camel: string,
  snake: string,
): string | undefined {
  return (
    strField(packet, camel, snake) ??
    (taskRecord ? strField(taskRecord, camel, snake) : undefined)
  );
}

function resolveTaskChannel(
  packet: Record<string, unknown>,
  taskRecord: Record<string, unknown> | undefined,
  taskHint: Task | undefined,
): string {
  const channel =
    resolveAliasedField(packet, taskRecord, "channel", "channel")?.trim() ||
    taskHint?.channel?.trim();
  if (!channel) {
    throw new Error("task channel is missing");
  }
  return channel;
}

/** Normalize the raw API response into a clean TaskDocument. */
export function normalizeTaskDocument(
  raw: unknown,
  taskHint?: Task,
): TaskDocument {
  if (!raw || typeof raw !== "object") {
    throw new Error("invalid task document response");
  }
  const r = raw as Record<string, unknown>;
  const taskRecord = recordValue(r.task);

  // The broker returns tasks with snake_case keys at the top level;
  // /tasks/<id> returns the decision-packet shape. Normalise both
  // forms at the boundary so the document route can render direct
  // links and list-to-detail navigations consistently.
  const taskId = resolveTaskId(r, taskRecord, taskHint);
  const title = resolveTaskTitle(r, taskRecord, taskHint, taskId);
  const lifecycleState = resolveTaskLifecycleState(r, taskHint);

  // Linear-style description: the broker writes `details` on the task
  // record; legacy clients may still write `description`. The office-tasks
  // taskHint carries the same pair as a fallback for packet shapes that
  // don't wrap a task record.
  const description =
    (taskRecord ? strField(taskRecord, "details", "description") : undefined) ??
    taskHint?.details ??
    taskHint?.description ??
    "";

  // parent_issue_id can arrive on the wrapped task record, at the
  // packet top level, or only via the office-tasks taskHint. Check all
  // three so child issues correctly hide the Sub-issues tab and show
  // the parent breadcrumb regardless of which shape the broker returns.
  const parentTaskId =
    resolveAliasedField(r, taskRecord, "parentIssueId", "parent_issue_id") ??
    taskHint?.parent_issue_id;

  // Verification fields (U1) live on the task record (snake_case wire) or
  // at the packet top level; the office-tasks taskHint is the fallback.
  const verification =
    normalizeVerification(taskRecord?.verification ?? r.verification) ??
    taskHint?.verification;
  const verificationResult =
    normalizeVerificationResult(
      taskRecord?.verification_result ?? r.verification_result,
    ) ?? taskHint?.verification_result;

  // The structured intake contract (R4) rides the same paths as
  // verification: wrapped task record, packet top level, taskHint fallback.
  const definition =
    normalizeTaskDefinition(taskRecord?.definition ?? r.definition) ??
    taskHint?.definition;

  return {
    taskId,
    title,
    description,
    lifecycleState,
    channel: resolveTaskChannel(r, taskRecord, taskHint),
    ownerSlug:
      resolveAliasedField(r, taskRecord, "ownerSlug", "owner") ??
      taskHint?.owner,
    parentTaskId,
    createdAt:
      resolveAliasedField(r, taskRecord, "createdAt", "created_at") ??
      taskHint?.created_at,
    updatedAt:
      resolveAliasedField(r, taskRecord, "updatedAt", "updated_at") ??
      taskHint?.updated_at,
    verification,
    verificationResult,
    definition,
  };
}

async function fetchTaskDocument(taskId: string): Promise<TaskDocument> {
  // The broker exposes the full task at /tasks/<id>. TaskDocument is a
  // presentation projection; we re-use the same endpoint as the Decision
  // Packet (which GET /tasks/<id> already serves) and normalise at the
  // boundary.
  const [raw, tasksResponse] = await Promise.all([
    get<unknown>(`/tasks/${encodeURIComponent(taskId)}`),
    getOfficeTasks({ includeDone: true }).catch(() => undefined),
  ]);
  const taskHint = tasksResponse?.tasks.find((task) => task.id === taskId);
  return normalizeTaskDocument(raw, taskHint);
}

// ── Loading + error states ─────────────────────────────────────────────

function TaskDocumentSkeleton() {
  return (
    <div
      className="issue-document issue-document--loading"
      data-testid="issue-document-loading"
      aria-busy="true"
      aria-label="Loading task"
      role="status"
    >
      <div className="issue-doc-header issue-doc-header--sticky">
        <div className="issue-doc-skeleton issue-doc-skeleton--pill" />
        <div className="issue-doc-skeleton issue-doc-skeleton--title" />
      </div>
      <div className="issue-doc-body">
        {[0, 1, 2, 3].map((i) => (
          <div
            key={i}
            className="issue-doc-skeleton issue-doc-skeleton--block"
            style={{ width: `${70 + (i % 2) * 15}%` }}
          />
        ))}
      </div>
    </div>
  );
}

function TaskDocumentError({
  message,
  onRetry,
}: {
  message: string;
  onRetry: () => void;
}) {
  return (
    <div
      className="issue-document issue-document--error"
      data-testid="issue-document-error"
    >
      <div className="issue-doc-error-card" role="alert">
        <strong>Could not load task</strong>
        <p>{message}</p>
        <button type="button" className="issue-doc-retry-btn" onClick={onRetry}>
          Retry
        </button>
      </div>
    </div>
  );
}

// ── Lifecycle action buttons ───────────────────────────────────────────

/**
 * Start button for a PARKED task — the ONE place a start affordance
 * remains. Visible only during `drafting` (now: explicitly parked) state;
 * every other creation path lands tasks running, so there is nothing to
 * start anywhere else (the Approve & Start ceremony is retired).
 *
 * On click: POSTs to the existing approve endpoint (postDecision
 * "approve" — the broker maps pre-execution approve to Drafting→Running),
 * transitions optimistically to "Starting…", then refetches the task.
 * On error: inline error banner appears, button re-enables.
 *
 * A11y: aria-label, focus-visible outline, Enter/Space activatable via
 * the native <button> element.
 */
interface StartParkedTaskButtonProps {
  taskId: string;
  onApproved: () => void;
  /** Button label. Defaults to "Start". */
  label?: string;
}

export function StartParkedTaskButton({
  taskId,
  onApproved,
  label = "Start",
}: StartParkedTaskButtonProps) {
  const [approveError, setApproveError] = useState<string | null>(null);

  const approveMutation = useMutation({
    mutationFn: () => postDecision(taskId, "approve"),
    onSuccess: () => {
      setApproveError(null);
      onApproved();
    },
    onError: (err: unknown) => {
      const message =
        err instanceof Error ? err.message : "Failed to approve task.";
      setApproveError(message);
    },
  });

  const { isPending } = approveMutation;

  return (
    <div className="issue-approve-and-start" data-testid="start-parked-wrapper">
      {approveError ? (
        <div
          className="issue-approve-error"
          role="alert"
          data-testid="start-parked-error"
        >
          {approveError}
        </div>
      ) : null}
      <button
        type="button"
        className="btn btn-primary issue-approve-btn"
        disabled={isPending}
        onClick={() => approveMutation.mutate()}
        aria-label="Start this parked task"
        data-testid="start-parked"
      >
        {isPending ? "Starting…" : label}
      </button>
    </div>
  );
}

/**
 * Close Issue button. Visible in any non-terminal lifecycle state so
 * the human can shelve work that's no longer relevant (CEO went down
 * the wrong path, scope changed, user moved on). Posts a Reject via
 * the existing /tasks endpoint — reject is terminal; downstream blocks
 * stay blocked, packet records the reason, channel gets a "task closed"
 * broadcast via postTaskCancelNotificationsLocked.
 *
 * Two-step gesture: first click reveals a reason textarea + confirm.
 * Without that gate, a stray click on a hover-target permanently
 * closes the Issue with no chance to undo.
 */
interface CloseTaskButtonProps {
  taskId: string;
  onClosed: () => void;
}

export function CloseTaskButton({ taskId, onClosed }: CloseTaskButtonProps) {
  const [confirming, setConfirming] = useState(false);
  const [reason, setReason] = useState("");
  const [closeError, setCloseError] = useState<string | null>(null);

  const closeMutation = useMutation({
    mutationFn: (r: string) => postTaskReject(taskId, r),
    onSuccess: () => {
      setCloseError(null);
      setConfirming(false);
      setReason("");
      onClosed();
    },
    onError: (err: unknown) => {
      setCloseError(
        err instanceof Error ? err.message : "Failed to close task.",
      );
    },
  });

  const trimmed = reason.trim();
  const canSubmit = trimmed.length > 0 && !closeMutation.isPending;

  if (!confirming) {
    return (
      <button
        type="button"
        className="btn btn-ghost issue-close-btn"
        onClick={() => setConfirming(true)}
        aria-label="Close this task (terminal)"
        data-testid="close-issue"
      >
        Close task
      </button>
    );
  }

  return (
    <div
      className="issue-close-confirm"
      data-testid="close-issue-confirm"
      role="group"
      aria-label="Confirm close task"
    >
      <label className="issue-close-confirm-label" htmlFor="close-reason">
        Reason for closing (required)
      </label>
      <textarea
        id="close-reason"
        className="issue-close-confirm-input"
        value={reason}
        onChange={(e) => {
          setReason(e.target.value);
          if (closeError) setCloseError(null);
        }}
        placeholder="e.g. Scope changed, no longer needed, duplicate of …"
        rows={2}
        disabled={closeMutation.isPending}
        data-testid="close-issue-reason"
      />
      {closeError ? (
        <p
          className="issue-close-confirm-error"
          role="alert"
          data-testid="close-issue-error"
        >
          {closeError}
        </p>
      ) : null}
      <div className="issue-close-confirm-actions">
        <button
          type="button"
          className="btn btn-ghost"
          onClick={() => {
            setConfirming(false);
            setReason("");
            setCloseError(null);
          }}
          disabled={closeMutation.isPending}
        >
          Cancel
        </button>
        <button
          type="button"
          className="btn btn-danger"
          disabled={!canSubmit}
          onClick={() => closeMutation.mutate(trimmed)}
          data-testid="close-issue-confirm"
        >
          {closeMutation.isPending ? "Closing…" : "Close task"}
        </button>
      </div>
    </div>
  );
}

// ── Main component ─────────────────────────────────────────────────────

interface TaskDocumentProps {
  taskId: string;
  /** Skip fetch and render with these data directly. Used by tests + screenshots. */
  initialDocument?: TaskDocument;
}

/**
 * TaskDocument renders a single task: header (pill + verification badge +
 * title + owner + lifecycle actions), the task channel chat as the main
 * column, and the context rail (participants, details, activity,
 * sub-tasks).
 *
 * Props:
 *   taskId — the task ID to fetch. Drives the query key.
 *   initialDocument — if provided, skips fetch; used in tests.
 */
export function TaskDocument({ taskId, initialDocument }: TaskDocumentProps) {
  const queryClient = useQueryClient();

  const query = useQuery<TaskDocument>({
    queryKey: ["issue", taskId],
    queryFn: () => fetchTaskDocument(taskId),
    initialData: initialDocument,
    staleTime: 5_000,
    enabled: !initialDocument,
  });

  const doc = query.data;
  const isDrafting = doc?.lifecycleState === "drafting";

  if (query.isPending && !initialDocument) {
    return <TaskDocumentSkeleton />;
  }

  if (query.isError && !doc) {
    return (
      <TaskDocumentError
        message={
          query.error instanceof Error
            ? query.error.message
            : "Network or broker error."
        }
        onRetry={() => void query.refetch()}
      />
    );
  }

  if (!doc) {
    return <TaskDocumentSkeleton />;
  }

  return (
    <div
      className="issue-document"
      data-testid="issue-document"
      data-task-id={taskId}
      data-lifecycle-state={doc.lifecycleState}
    >
      {/* Compact header: breadcrumb, then a single tight row (pill + title),
       *  then a meta/action row (owner left, lifecycle actions right). Kept
       *  deliberately short so the chat below gets the vertical space. */}
      <header className="issue-doc-header issue-doc-header--sticky issue-doc-header--compact">
        {doc.parentTaskId ? (
          <ParentTaskBreadcrumb parentTaskId={doc.parentTaskId} />
        ) : null}
        <div className="issue-doc-header-row">
          <LifecycleStatePill state={doc.lifecycleState} />
          <VerificationBadge
            verification={doc.verification}
            result={doc.verificationResult}
          />
          <h2 className="issue-doc-title">
            {formatTaskTitleForDisplay(doc.title)}
          </h2>
        </div>
        <div className="issue-doc-meta-row" data-testid="issue-doc-button-row">
          <OwnerPicker
            taskId={taskId}
            channel={doc.channel}
            currentOwner={doc.ownerSlug}
            onChanged={() => {
              void queryClient.invalidateQueries({
                queryKey: ["issue", taskId],
              });
              void queryClient.invalidateQueries({ queryKey: ["issues"] });
              void queryClient.invalidateQueries({
                queryKey: ["issue-children"],
              });
            }}
          />
          {/* Lifecycle actions (Start when parked/drafting, the
           *  PR-style loop otherwise). Right-aligned on the same row as the
           *  owner so the header stays two tight rows instead of four. */}
          <TaskActionToolbar
            taskId={taskId}
            channel={doc.channel}
            lifecycleState={doc.lifecycleState}
            onAfterAction={() => {
              void queryClient.invalidateQueries({
                queryKey: ["issue", taskId],
              });
              void queryClient.invalidateQueries({ queryKey: ["issues"] });
              void queryClient.invalidateQueries({
                queryKey: ["lifecycle"],
              });
              void queryClient.invalidateQueries({
                queryKey: ["lifecycle", "inbox-items"],
              });
            }}
          />
        </div>
      </header>

      {/* Body: chat-primary. The task's channel (the conversation where the
       *  owner, CEO, and Librarian collaborate) owns the main column at full
       *  scale; the secondary context — participants, description, activity,
       *  sub-tasks — lives in the right rail a glance away. */}
      <div className="issue-doc-body issue-doc-body--split">
        <main className="issue-doc-chat" aria-label="Chat">
          <div className="issue-doc-chat-header">Chat</div>
          <TaskChannelChat channel={doc.channel} />
        </main>

        <TaskContextRail
          taskId={taskId}
          channel={doc.channel}
          description={doc.description}
          isDrafting={isDrafting}
          showSubTasks={!doc.parentTaskId}
          verification={doc.verification}
          definition={doc.definition}
        />
      </div>
    </div>
  );
}
