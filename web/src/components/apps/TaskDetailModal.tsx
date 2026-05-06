// biome-ignore-all lint/a11y/useKeyWithClickEvents: Pointer handler is paired with an existing modal, image, or routed-control keyboard path; preserving current interaction model.
import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";

import { getOfficeMembers, type OfficeMember } from "../../api/client";
import {
  reassignTask,
  type Task,
  type TaskMemoryWorkflow,
  type TaskMemoryWorkflowArtifact,
  type TaskMemoryWorkflowCitation,
  type TaskMemoryWorkflowPartialError,
  type TaskMemoryWorkflowStepState,
  type TaskStatusAction,
  updateTaskStatus,
} from "../../api/tasks";
import { useAgentStream } from "../../hooks/useAgentStream";
import { formatRelativeTime } from "../../lib/format";
import { keyedByOccurrence } from "../../lib/reactKeys";
import { StreamLineView } from "../messages/StreamLineView";
import { confirm } from "../ui/ConfirmDialog";

interface TaskDetailModalProps {
  task: Task;
  onClose: () => void;
  presentation?: "modal" | "page";
}

const HUMAN_SLUG = "human";

interface TaskMemoryWorkflowBadge {
  label: string;
  className: string;
  title: string;
}

const COMPLETE_MEMORY_STATUSES = new Set([
  "satisfied",
  "complete",
  "completed",
  "done",
]);
const ISSUE_MEMORY_STATUSES = new Set([
  "blocked",
  "error",
  "errored",
  "failed",
  "incomplete",
  "missing_artifacts",
  "partial_errors",
]);
const OVERRIDE_MEMORY_STATUSES = new Set(["overridden", "override"]);
const WORKFLOW_STEP_NAMES = ["lookup", "capture", "promote"] as const;

type WorkflowStepName = (typeof WORKFLOW_STEP_NAMES)[number];

function normalizeMemoryStatus(status?: string): string {
  return (status || "")
    .trim()
    .toLowerCase()
    .replace(/[\s-]+/g, "_");
}

function memoryWorkflowStep(
  workflow: TaskMemoryWorkflow,
  step: WorkflowStepName,
): TaskMemoryWorkflowStepState | undefined {
  return workflow[step];
}

function isMemoryStepSatisfied(step?: TaskMemoryWorkflowStepState): boolean {
  return (
    normalizeMemoryStatus(step?.status) === "satisfied" ||
    Boolean(step?.completed_at)
  );
}

function memoryWorkflowRequiredSteps(
  workflow: TaskMemoryWorkflow,
): WorkflowStepName[] {
  const fromContract = (workflow.required_steps ?? [])
    .map((step) => normalizeMemoryStatus(step))
    .filter((step): step is WorkflowStepName =>
      WORKFLOW_STEP_NAMES.includes(step as WorkflowStepName),
    );
  if (fromContract.length > 0) return fromContract;

  const fromStates = WORKFLOW_STEP_NAMES.filter(
    (step) => memoryWorkflowStep(workflow, step)?.required,
  );
  if (fromStates.length > 0) return fromStates;

  return workflow.required ? WORKFLOW_STEP_NAMES.slice() : [];
}

function memoryWorkflowStepCount(workflow: TaskMemoryWorkflow): {
  done: number;
  total: number;
} {
  const requiredSteps = memoryWorkflowRequiredSteps(workflow);
  const total = requiredSteps.length;
  const done = requiredSteps.filter((step) =>
    isMemoryStepSatisfied(memoryWorkflowStep(workflow, step)),
  ).length;
  return { done, total };
}

function memoryWorkflowArtifacts(
  workflow: TaskMemoryWorkflow,
): TaskMemoryWorkflowArtifact[] {
  return [...(workflow.captures ?? []), ...(workflow.promotions ?? [])];
}

function hasMissingMemoryArtifact(workflow: TaskMemoryWorkflow): boolean {
  return memoryWorkflowArtifacts(workflow).some((artifact) => artifact.missing);
}

function hasMemoryWorkflowOverride(workflow: TaskMemoryWorkflow): boolean {
  return Boolean(
    workflow.override ||
      OVERRIDE_MEMORY_STATUSES.has(normalizeMemoryStatus(workflow.status)),
  );
}

function hasVisibleMemoryWorkflow(
  workflow?: TaskMemoryWorkflow | null,
): workflow is TaskMemoryWorkflow {
  if (!workflow) return false;
  const status = normalizeMemoryStatus(workflow.status);
  const stepCount = memoryWorkflowStepCount(workflow);
  return Boolean(
    workflow.required ||
      (status && status !== "not_required") ||
      workflow.requirement_reason ||
      stepCount.done > 0 ||
      workflow.citations?.length ||
      memoryWorkflowArtifacts(workflow).length ||
      workflow.partial_errors?.length ||
      hasMemoryWorkflowOverride(workflow),
  );
}

function displayMemoryStatus(status: string): string {
  return status.replace(/_/g, " ");
}

// biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Existing cognitive complexity is baselined for a focused follow-up refactor.
export function taskMemoryWorkflowBadge(
  workflow?: TaskMemoryWorkflow | null,
): TaskMemoryWorkflowBadge | null {
  if (!hasVisibleMemoryWorkflow(workflow)) return null;

  const status = normalizeMemoryStatus(workflow.status);
  const { done, total } = memoryWorkflowStepCount(workflow);
  const errors = workflow.partial_errors?.length ?? 0;
  const reason = workflow.requirement_reason
    ? ` · ${workflow.requirement_reason}`
    : "";

  if (hasMemoryWorkflowOverride(workflow)) {
    const actor = workflow.override?.actor
      ? ` by @${workflow.override.actor}`
      : "";
    return {
      label: "memory override",
      className: "badge badge-yellow",
      title: `Memory workflow overridden${actor}${reason}`,
    };
  }

  if (
    errors > 0 ||
    hasMissingMemoryArtifact(workflow) ||
    ISSUE_MEMORY_STATUSES.has(status)
  ) {
    const issueText =
      errors > 0
        ? `${errors} partial ${errors === 1 ? "error" : "errors"}`
        : "an issue";
    return {
      label: "memory issue",
      className: "badge badge-yellow",
      title: `Memory workflow has ${issueText}${reason}`,
    };
  }

  if (
    COMPLETE_MEMORY_STATUSES.has(status) ||
    (workflow.required && done >= total)
  ) {
    return {
      label: "memory done",
      className: "badge badge-green",
      title: `Memory workflow complete${reason}`,
    };
  }

  if (workflow.required || done > 0) {
    return {
      label: `memory ${done}/${total}`,
      className: "badge badge-accent",
      title: `Memory workflow ${done}/${total} steps complete${reason}`,
    };
  }

  return {
    label: `memory ${displayMemoryStatus(status || "pending")}`,
    className: "badge badge-neutral",
    title: `Memory workflow ${displayMemoryStatus(status || "pending")}${reason}`,
  };
}

// biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Existing cognitive complexity is baselined for a focused follow-up refactor.
// biome-ignore lint/complexity/noExcessiveLinesPerFunction: Existing function length is baselined for a focused follow-up refactor.
export function TaskDetailModal({
  task,
  onClose,
  presentation = "modal",
}: TaskDetailModalProps) {
  const queryClient = useQueryClient();
  const { data: memberData } = useQuery({
    queryKey: ["office-members"],
    queryFn: getOfficeMembers,
    staleTime: 30_000,
  });

  const currentOwner = (task.owner ?? "").trim();
  const currentStatus = (task.status ?? "").trim().toLowerCase();
  const [selectedOwner, setSelectedOwner] = useState<string>(currentOwner);
  const [submitting, setSubmitting] = useState(false);
  const [statusBusy, setStatusBusy] = useState<TaskStatusAction | null>(null);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);
  const [overrideReason, setOverrideReason] = useState("");

  useEffect(() => {
    void task.id;
    setSelectedOwner((task.owner ?? "").trim());
    setErrorMsg(null);
    setOverrideReason("");
  }, [task.id, task.owner]);

  useEffect(() => {
    if (presentation !== "modal") return;
    function handleKey(e: KeyboardEvent) {
      if (e.key === "Escape") onClose();
    }
    document.addEventListener("keydown", handleKey);
    return () => document.removeEventListener("keydown", handleKey);
  }, [onClose, presentation]);

  const assignableMembers = useMemo<OfficeMember[]>(() => {
    const members = memberData?.members ?? [];
    return members.filter((m) => {
      const slug = m.slug?.trim().toLowerCase();
      return slug && slug !== "human" && slug !== "you";
    });
  }, [memberData]);

  async function runStatusAction(action: TaskStatusAction) {
    setStatusBusy(action);
    setErrorMsg(null);
    try {
      await updateTaskStatus(
        task.id,
        action,
        task.channel || "general",
        HUMAN_SLUG,
      );
      await queryClient.invalidateQueries({ queryKey: ["office-tasks"] });
      if (action === "cancel" || action === "complete") {
        onClose();
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : `${action} failed`;
      setErrorMsg(message);
    } finally {
      setStatusBusy(null);
    }
  }

  async function handleMemoryOverrideComplete() {
    const reason = overrideReason.trim();
    if (!reason) {
      setErrorMsg("Memory workflow override requires reason");
      return;
    }
    setStatusBusy("complete");
    setErrorMsg(null);
    try {
      await updateTaskStatus(
        task.id,
        "complete",
        task.channel || "general",
        HUMAN_SLUG,
        {
          memoryWorkflowOverride: true,
          memoryWorkflowOverrideActor: HUMAN_SLUG,
          memoryWorkflowOverrideReason: reason,
        },
      );
      await queryClient.invalidateQueries({ queryKey: ["office-tasks"] });
      onClose();
    } catch (err) {
      const message = err instanceof Error ? err.message : "Override failed";
      setErrorMsg(message);
    } finally {
      setStatusBusy(null);
    }
  }

  function handleStatusAction(action: TaskStatusAction) {
    if (action === "cancel") {
      confirm({
        title: "Mark task as won't do?",
        message: `"${task.title || task.id}" will move to the Won't Do column. Owners are notified.`,
        confirmLabel: "Won't do",
        danger: true,
        onConfirm: () => runStatusAction(action),
      });
      return;
    }
    void runStatusAction(action);
  }

  async function handleReassign() {
    const next = selectedOwner.trim();
    if (!next || next === currentOwner) return;
    setSubmitting(true);
    setErrorMsg(null);
    try {
      await reassignTask(task.id, next, task.channel || "general", HUMAN_SLUG);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["office-tasks"] }),
        queryClient.invalidateQueries({ queryKey: ["tasks"] }),
      ]);
      onClose();
    } catch (err) {
      const message = err instanceof Error ? err.message : "Reassign failed";
      setErrorMsg(message);
    } finally {
      setSubmitting(false);
    }
  }

  function handleOverlayClick(e: React.MouseEvent<HTMLDivElement>) {
    if (e.target === e.currentTarget) onClose();
  }

  const status = (task.status || "").replace(/_/g, " ");
  const reviewState = (task.review_state || "").replace(/_/g, " ");
  const description = task.description?.trim() || "";
  const details = task.details?.trim() || "";
  const memoryWorkflow = task.memory_workflow;
  const memoryWorkflowProgress = memoryWorkflow
    ? memoryWorkflowStepCount(memoryWorkflow)
    : { done: 0, total: 0 };
  const memoryWorkflowHasIssue = Boolean(
    memoryWorkflow &&
      ((memoryWorkflow.partial_errors?.length ?? 0) > 0 ||
        hasMissingMemoryArtifact(memoryWorkflow) ||
        ISSUE_MEMORY_STATUSES.has(
          normalizeMemoryStatus(memoryWorkflow.status),
        )),
  );
  const memoryWorkflowNeedsOverride = Boolean(
    memoryWorkflow?.required &&
      !hasMemoryWorkflowOverride(memoryWorkflow) &&
      (memoryWorkflowHasIssue ||
        (!COMPLETE_MEMORY_STATUSES.has(
          normalizeMemoryStatus(memoryWorkflow.status),
        ) &&
          memoryWorkflowProgress.done < memoryWorkflowProgress.total)),
  );

  const metaRows: Array<[string, string | null | undefined]> = [
    ["Owner", task.owner ? `@${task.owner}` : "(unassigned)"],
    ["Channel", task.channel ? `#${task.channel}` : "—"],
    ["Status", status || "—"],
    ["Review state", reviewState || null],
    ["Task type", task.task_type || null],
    ["Execution mode", task.execution_mode || null],
    ["Pipeline", task.pipeline_id || null],
    ["Pipeline stage", task.pipeline_stage || null],
    ["Worktree branch", task.worktree_branch || null],
    ["Worktree path", task.worktree_path || null],
    ["Source signal", task.source_signal_id || null],
    ["Source decision", task.source_decision_id || null],
    ["Thread", task.thread_id || null],
    ["Created by", task.created_by ? `@${task.created_by}` : null],
    ["Created", task.created_at ? formatRelativeTime(task.created_at) : null],
    ["Updated", task.updated_at ? formatRelativeTime(task.updated_at) : null],
    ["Due", task.due_at ? formatRelativeTime(task.due_at) : null],
    [
      "Follow up",
      task.follow_up_at ? formatRelativeTime(task.follow_up_at) : null,
    ],
    [
      "Reminder",
      task.reminder_at ? formatRelativeTime(task.reminder_at) : null,
    ],
    ["Recheck", task.recheck_at ? formatRelativeTime(task.recheck_at) : null],
  ];

  const dependsOn = task.depends_on ?? [];

  const ownerChanged =
    selectedOwner.trim() !== currentOwner && selectedOwner.trim() !== "";

  const terminalOwner =
    task.owner && task.owner !== HUMAN_SLUG && task.owner !== "you"
      ? task.owner
      : null;
  const closeLabel = presentation === "page" ? "Back to tasks" : "Close";
  const content = (
    <div
      className={`task-detail-modal card${presentation === "page" ? " task-detail-page-card" : ""}`}
    >
      <header className="task-detail-header">
        <div>
          <div className="task-detail-id">#{task.id}</div>
          <h2 className="task-detail-title">{task.title || "Untitled task"}</h2>
        </div>
        <button
          type="button"
          className="task-detail-close"
          onClick={onClose}
          aria-label={closeLabel}
          title={closeLabel}
        >
          {presentation === "page" ? "←" : "×"}
        </button>
      </header>

      <section className="task-detail-section">
        <div className="task-detail-label">Status</div>
        <div className="task-detail-status">
          <span
            className={`task-detail-status-badge status-${currentStatus || "open"}`}
          >
            {currentStatus ? currentStatus.replace(/_/g, " ") : "open"}
          </span>
          <div className="task-detail-status-actions">
            <StatusButton
              action="release"
              label="Release"
              busy={statusBusy}
              disabledFor={["open"]}
              currentStatus={currentStatus}
              onClick={handleStatusAction}
            />
            <StatusButton
              action="review"
              label="Mark review"
              busy={statusBusy}
              disabledFor={["review"]}
              currentStatus={currentStatus}
              onClick={handleStatusAction}
            />
            <StatusButton
              action="block"
              label="Block"
              busy={statusBusy}
              disabledFor={["blocked"]}
              currentStatus={currentStatus}
              onClick={handleStatusAction}
            />
            <StatusButton
              action="complete"
              label="Mark done"
              busy={statusBusy}
              disabledFor={["done"]}
              currentStatus={currentStatus}
              onClick={handleStatusAction}
              disabled={memoryWorkflowNeedsOverride}
            />
            <StatusButton
              action="cancel"
              label="Won't do"
              busy={statusBusy}
              disabledFor={["canceled", "cancelled"]}
              currentStatus={currentStatus}
              onClick={handleStatusAction}
              danger={true}
            />
          </div>
        </div>
        {memoryWorkflowNeedsOverride && (
          <div className="task-detail-override">
            <label
              className="task-detail-label"
              htmlFor={`memory-override-${task.id}`}
            >
              Override reason
            </label>
            <textarea
              id={`memory-override-${task.id}`}
              className="task-detail-textarea"
              value={overrideReason}
              onChange={(e) => setOverrideReason(e.target.value)}
              placeholder="Human reason required"
              rows={3}
            />
            <button
              type="button"
              className="btn btn-primary btn-sm"
              onClick={handleMemoryOverrideComplete}
              disabled={!overrideReason.trim() || statusBusy !== null}
            >
              {statusBusy === "complete" ? "..." : "Mark done with override"}
            </button>
          </div>
        )}
      </section>

      <section className="task-detail-section">
        <div className="task-detail-label">Ownership</div>
        <div className="task-detail-ownership">
          <div className="task-detail-owner-current">
            <span className="task-detail-owner-badge">
              {task.owner ? `@${task.owner}` : "(unassigned)"}
            </span>
            <span className="task-detail-hint">
              Reassigning posts to #{task.channel || "general"} and DMs both
              owners. CEO is cc'd.
            </span>
          </div>
          <div className="task-detail-owner-controls">
            <select
              className="task-detail-select"
              value={selectedOwner}
              onChange={(e) => setSelectedOwner(e.target.value)}
              disabled={submitting}
            >
              <option value="">(pick an owner)</option>
              {assignableMembers.map((m) => (
                <option key={m.slug} value={m.slug}>
                  {m.name ? `${m.name} — @${m.slug}` : `@${m.slug}`}
                </option>
              ))}
            </select>
            <button
              type="button"
              className="btn btn-primary btn-sm"
              onClick={handleReassign}
              disabled={!ownerChanged || submitting}
            >
              {submitting ? "Reassigning..." : "Reassign"}
            </button>
          </div>
          {errorMsg ? (
            <div className="task-detail-error">{errorMsg}</div>
          ) : null}
        </div>
      </section>

      {description || details ? (
        <section className="task-detail-section">
          {description ? (
            <>
              <div className="task-detail-label">Description</div>
              <div className="task-detail-body">{description}</div>
            </>
          ) : null}
          {details ? (
            <>
              <div
                className="task-detail-label"
                style={{ marginTop: description ? 12 : 0 }}
              >
                Details
              </div>
              <div className="task-detail-body">{details}</div>
            </>
          ) : null}
        </section>
      ) : null}

      {dependsOn.length > 0 && (
        <section className="task-detail-section">
          <div className="task-detail-label">Depends on</div>
          <ul className="task-detail-deps">
            {dependsOn.map((dep) => (
              <li key={dep}>#{dep}</li>
            ))}
          </ul>
        </section>
      )}

      {hasVisibleMemoryWorkflow(memoryWorkflow) && (
        <MemoryWorkflowSection workflow={memoryWorkflow} />
      )}

      {terminalOwner ? (
        <section className="task-detail-section task-detail-terminal-section">
          <div className="task-detail-label">Live stream</div>
          <TaskStreamSection slug={terminalOwner} taskId={task.id} />
        </section>
      ) : null}

      <section className="task-detail-section">
        <div className="task-detail-label">Metadata</div>
        <dl className="task-detail-meta">
          {metaRows
            .filter(([, value]) => value !== null && value !== "")
            .map(([key, value]) => (
              <div key={key} className="task-detail-meta-row">
                <dt>{key}</dt>
                <dd>{value}</dd>
              </div>
            ))}
        </dl>
      </section>
    </div>
  );

  if (presentation === "page") {
    return (
      <section className="task-detail-page" aria-label={`Task ${task.id}`}>
        {content}
      </section>
    );
  }

  return (
    <div
      className="task-detail-overlay"
      onClick={handleOverlayClick}
      role="dialog"
      aria-modal="true"
      aria-label={`Task ${task.id}`}
    >
      {content}
    </div>
  );
}

function MemoryWorkflowSection({ workflow }: { workflow: TaskMemoryWorkflow }) {
  const badge = taskMemoryWorkflowBadge(workflow);
  const status = normalizeMemoryStatus(workflow.status);
  const stepRows = WORKFLOW_STEP_NAMES.map((step): [string, string | null] => [
    step[0].toUpperCase() + step.slice(1),
    formatWorkflowStep(memoryWorkflowStep(workflow, step)),
  ]);
  const rows: Array<[string, string | null | undefined]> = [
    ["Required", workflow.required ? "yes" : "no"],
    ["Status", status ? displayMemoryStatus(status) : null],
    ["Reason", workflow.requirement_reason || null],
    [
      "Required steps",
      memoryWorkflowRequiredSteps(workflow).join(", ") || null,
    ],
    ...stepRows,
    ["Completed", formatWorkflowTime(workflow.completed_at)],
    ["Updated", formatWorkflowTime(workflow.updated_at)],
  ];

  if (hasMemoryWorkflowOverride(workflow)) {
    rows.push(
      [
        "Override actor",
        workflow.override?.actor ? `@${workflow.override.actor}` : null,
      ],
      ["Override reason", workflow.override?.reason || null],
      ["Override time", formatWorkflowTime(workflow.override?.timestamp)],
    );
  }

  return (
    <section className="task-detail-section">
      <div className="task-detail-label">Memory workflow</div>
      {badge && (
        <div style={{ marginBottom: 10 }}>
          <span className={badge.className} title={badge.title}>
            {badge.label}
          </span>
        </div>
      )}
      <dl className="task-detail-meta">
        {rows
          .filter(
            ([, value]) =>
              value !== null && value !== undefined && value !== "",
          )
          .map(([key, value]) => (
            <div key={key} className="task-detail-meta-row">
              <dt>{key}</dt>
              <dd>{value}</dd>
            </div>
          ))}
      </dl>
      {workflow.citations && workflow.citations.length > 0 && (
        <DetailList
          label="Citations"
          items={workflow.citations.map(formatWorkflowCitation)}
        />
      )}
      {workflow.captures && workflow.captures.length > 0 && (
        <DetailList
          label="Captures"
          items={workflow.captures.map(formatWorkflowArtifact)}
        />
      )}
      {workflow.promotions && workflow.promotions.length > 0 && (
        <DetailList
          label="Promotions"
          items={workflow.promotions.map(formatWorkflowArtifact)}
        />
      )}
      {workflow.partial_errors && workflow.partial_errors.length > 0 && (
        <DetailList
          label="Partial errors"
          items={workflow.partial_errors.map(formatWorkflowError)}
        />
      )}
    </section>
  );
}

function DetailList({ label, items }: { label: string; items: string[] }) {
  return (
    <div style={{ marginTop: 12 }}>
      <div className="task-detail-label" style={{ marginBottom: 6 }}>
        {label}
      </div>
      <ul className="task-detail-deps" style={{ display: "block" }}>
        {keyedByOccurrence(items, (item) => `${label}-${item}`).map(
          ({ key, value: item }) => (
            <li
              key={key}
              style={{
                marginBottom: 6,
                whiteSpace: "normal",
                wordBreak: "break-word",
              }}
            >
              {item}
            </li>
          ),
        )}
      </ul>
    </div>
  );
}

function formatWorkflowTime(value?: string): string | null {
  return value ? formatRelativeTime(value) : null;
}

function formatWorkflowStep(step?: TaskMemoryWorkflowStepState): string | null {
  if (!step) return null;
  const parts = [
    step.status
      ? displayMemoryStatus(normalizeMemoryStatus(step.status))
      : null,
    step.count !== null && step.count !== undefined && step.count > 0
      ? `${step.count} item${step.count === 1 ? "" : "s"}`
      : null,
    step.actor ? `@${step.actor}` : null,
    formatWorkflowTime(step.completed_at),
  ].filter(Boolean);
  return parts.join(" · ") || null;
}

function formatWorkflowCitation(citation: TaskMemoryWorkflowCitation): string {
  const title =
    citation.title ||
    citation.path ||
    citation.source_url ||
    citation.source_id ||
    "citation";
  const parts = [title];
  if (citation.path && citation.path !== title) parts.push(citation.path);
  if (citation.source) parts.push(citation.source);
  if (citation.backend) parts.push(citation.backend);
  if (citation.stale) parts.push("stale");
  return parts.join(" · ");
}

function formatWorkflowArtifact(artifact: TaskMemoryWorkflowArtifact): string {
  const title =
    artifact.title ||
    artifact.path ||
    artifact.page_id ||
    artifact.promotion_id ||
    "artifact";
  const parts = [title];
  if (artifact.source) parts.unshift(artifact.source);
  if (artifact.skip_reason && artifact.skip_reason !== title)
    parts.push(artifact.skip_reason);
  if (artifact.path && artifact.path !== title) parts.push(artifact.path);
  if (artifact.state) parts.push(artifact.state);
  if (artifact.missing) parts.push("missing");
  return parts.join(" · ");
}

function formatWorkflowError(
  error: string | TaskMemoryWorkflowPartialError,
): string {
  if (typeof error === "string") return error;
  return (
    [error.step, error.code, error.message || error.detail]
      .filter(Boolean)
      .join(" · ") || "workflow error"
  );
}

interface StatusButtonProps {
  action: TaskStatusAction;
  label: string;
  busy: TaskStatusAction | null;
  disabledFor: string[];
  currentStatus: string;
  onClick: (action: TaskStatusAction) => void;
  disabled?: boolean;
  danger?: boolean;
}

function StatusButton({
  action,
  label,
  busy,
  disabledFor,
  currentStatus,
  onClick,
  disabled,
  danger,
}: StatusButtonProps) {
  const isCurrent = disabledFor.includes(currentStatus);
  const isBusy = busy === action;
  const anyBusy = busy !== null;
  const className =
    "btn btn-sm " +
    (danger ? "btn-ghost task-detail-status-btn-danger" : "btn-ghost");
  const buttonLabel = isBusy ? "..." : label;
  return (
    <button
      type="button"
      className={className}
      onClick={() => onClick(action)}
      disabled={disabled || isCurrent || anyBusy}
      title={isCurrent ? "Task is already in this state" : undefined}
    >
      {buttonLabel}
    </button>
  );
}

function TaskStreamSection({ slug, taskId }: { slug: string; taskId: string }) {
  const { lines, connected } = useAgentStream(slug, taskId);
  const scrollRef = useRef<HTMLDivElement>(null);

  // biome-ignore lint/correctness/useExhaustiveDependencies: re-run on every new line so the log auto-scrolls.
  useEffect(() => {
    const el = scrollRef.current;
    if (el) {
      el.scrollTop = el.scrollHeight;
    }
  }, [lines.length]);

  return (
    <div>
      <div className="agent-stream-status">
        <span
          className={`status-dot ${connected ? "active pulse" : "lurking"}`}
        />
        {connected ? "Connected" : "Disconnected"}
      </div>
      <div className="agent-stream-log" ref={scrollRef}>
        {lines.length === 0 ? (
          <div className="agent-stream-empty">
            No stream output for this task yet
          </div>
        ) : (
          lines.map((line) => (
            <StreamLineView key={line.id} line={line} compact={true} />
          ))
        )}
      </div>
    </div>
  );
}
