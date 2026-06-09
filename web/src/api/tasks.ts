import { isLifecycleState, type LifecycleState } from "../lib/types/lifecycle";
import { get, post } from "./client";

export interface TaskMemoryWorkflowCitation {
  backend?: string;
  source?: string;
  source_id?: string;
  path?: string;
  page_id?: string;
  chunk_id?: string;
  source_url?: string;
  line_start?: number;
  line_end?: number;
  title?: string;
  snippet?: string;
  score?: number;
  stale?: boolean;
  retrieved_at?: string;
}

export interface TaskMemoryWorkflowArtifact {
  backend?: string;
  source?: string;
  path?: string;
  page_id?: string;
  promotion_id?: string;
  entity_kind?: string;
  entity_slug?: string;
  playbook_slug?: string;
  title?: string;
  skip_reason?: string;
  snippet?: string;
  commit_sha?: string;
  state?: string;
  recorded_at?: string;
  updated_at?: string;
  missing?: boolean;
}

export interface TaskMemoryWorkflowStepState {
  required?: boolean;
  status?: string;
  actor?: string;
  query?: string;
  completed_at?: string;
  updated_at?: string;
  count?: number;
}

export interface TaskMemoryWorkflowOverride {
  actor: string;
  reason: string;
  timestamp: string;
}

export interface TaskMemoryWorkflowPartialError {
  step?: string;
  code?: string;
  message?: string;
  detail?: string;
  timestamp?: string;
}

export interface TaskMemoryWorkflow {
  required: boolean;
  status?: string;
  requirement_reason?: string;
  required_steps?: string[];
  lookup?: TaskMemoryWorkflowStepState;
  capture?: TaskMemoryWorkflowStepState;
  promote?: TaskMemoryWorkflowStepState;
  citations?: TaskMemoryWorkflowCitation[];
  captures?: TaskMemoryWorkflowArtifact[];
  promotions?: TaskMemoryWorkflowArtifact[];
  override?: TaskMemoryWorkflowOverride;
  partial_errors?: Array<string | TaskMemoryWorkflowPartialError>;
  created_at?: string;
  updated_at?: string;
  completed_at?: string;
}

export interface TaskDraftSpec {
  goal?: string;
  context?: string;
  approach?: string;
  acceptance?: string;
  drafted_at?: string;
}

/**
 * Machine-checkable definition of done on a task (U1.1, broker
 * task_verification.go). Mirrors the Go `TaskVerification` wire shape.
 * `kind` is one of "command" | "artifact" | "url" | "none", kept as
 * `string` (same convention as `lifecycle_state`) so an unknown kind
 * from a newer broker doesn't fail parsing.
 */
export interface TaskVerification {
  kind: string;
  /** Kind-specific: shell command, artifact path/glob, or http(s) URL. */
  spec?: string;
  /** When true the broker blocks complete/approve until the check passes. */
  required?: boolean;
}

/**
 * Stamped outcome of the most recent verification run. Mirrors the Go
 * `TaskVerificationResult` wire shape.
 */
export interface TaskVerificationResult {
  pass: boolean;
  kind: string;
  /** Check output tail / failure detail — the proof. */
  detail?: string;
  /** RFC3339 timestamp of the run. */
  checked_at: string;
}

export interface Task {
  id: string;
  title: string;
  description?: string;
  details?: string;
  status: string;
  owner?: string;
  created_by?: string;
  channel?: string;
  /**
   * True for broker-managed system tasks (the Backup & Migration task that
   * owns #general, and the archived tasks that legacy channels/DMs were
   * folded into). Business tasks are false/absent. Used to keep system-owned
   * channels directly readable rather than redirecting them to an archived
   * system task.
   */
  system?: boolean;
  thread_id?: string;
  task_type?: string;
  pipeline_id?: string;
  pipeline_stage?: string;
  /**
   * Broker lifecycle position. Optional on the wire because not every
   * Task source emits it yet; consumers should fall back to `status`.
   * Kept as `string` (not the LifecycleState union) so an unknown value
   * from a newer broker doesn't silently fail JSON parsing.
   */
  lifecycle_state?: string;
  execution_mode?: string;
  /** Per-task LLM runtime set in the new-task composer (model lives on the
   * task, not the agent). */
  provider?: string;
  model?: string;
  /** Model-specific reasoning-effort level set in the new-task composer. */
  effort?: string;
  review_state?: string;
  source_signal_id?: string;
  source_decision_id?: string;
  worktree_path?: string;
  worktree_branch?: string;
  depends_on?: string[];
  parent_issue_id?: string;
  blocked?: boolean;
  acked_at?: string;
  due_at?: string;
  follow_up_at?: string;
  reminder_at?: string;
  recheck_at?: string;
  created_at?: string;
  updated_at?: string;
  issue_draft_spec?: TaskDraftSpec;
  memory_workflow?: TaskMemoryWorkflow;
  /** Machine-checkable definition of done (U1). Absent on legacy tasks. */
  verification?: TaskVerification;
  /** Outcome of the most recent verification run. Absent until first run. */
  verification_result?: TaskVerificationResult;
}

/**
 * Resolve a Task's typed LifecycleState from its (legacy) pipeline_stage /
 * lifecycle_state / status fields. Single source of truth so the board and the
 * task-detail surface can't drift — the detail copy previously omitted the
 * `archived` case and rendered an "intake" pill for archived tasks. `task.
 * lifecycle_state` is read off the typed field (no cast); it's `string` on the
 * wire so an unknown value from a newer broker falls through to the status map
 * rather than failing.
 */
export function taskToLifecycleState(task: Task | undefined): LifecycleState {
  if (task?.pipeline_stage === "draft") return "drafting";
  if (task?.pipeline_stage === "plan") return "planning";
  const ls = task?.lifecycle_state;
  if (ls && isLifecycleState(ls)) return ls;
  switch (task?.status) {
    case "open":
      return "intake";
    case "in_progress":
      return "running";
    case "done":
      return "approved";
    case "blocked":
      return "blocked_on_pr_merge";
    case "review":
      return "review";
    case "rejected":
      return "rejected";
    case "archived":
      return "archived";
    default:
      return "intake";
  }
}

export interface CreateTaskInput {
  title: string;
  assignee: string;
  details?: string;
  task_type?: string;
  execution_mode?: string;
  /**
   * Per-task LLM runtime. The model/provider is a property of the task, not
   * the agent: dispatch prefers these over the owner's binding. Effort is the
   * model-specific reasoning level. Omit any to inherit the owner's binding /
   * the install default.
   */
  provider?: string;
  model?: string;
  effort?: string;
  /**
   * When true, create the task ASSIGNED but parked in the backlog
   * (non-executable) instead of dispatching the owner now. The composer's
   * "Backlog" action sets this; "Start now" leaves it false.
   */
  park?: boolean;
  /**
   * Plan mode (Phase 5): when true, the owner plans autonomously before
   * executing — it writes a plan to its notebook and waits for "Approve &
   * Start". When false, the task runs immediately with no plan/approval gate.
   * The composer's "Plan first" toggle defaults ON and always sends a value.
   */
  plan_first?: boolean;
  depends_on?: string[];
}

export interface CreateTasksResponse {
  tasks: Task[];
}

export interface TaskListResponse {
  channel?: string;
  tasks: Task[];
}

export interface TaskResponse {
  task: Task;
}

export function createTasks(
  tasks: CreateTaskInput[],
  opts?: { channel?: string; createdBy?: string },
): Promise<CreateTasksResponse> {
  return post<CreateTasksResponse>("/task-plan", {
    channel: opts?.channel || "general",
    created_by: opts?.createdBy || "human",
    tasks,
  });
}

export function reassignTask(
  taskId: string,
  newOwner: string,
  channel: string,
  actor = "human",
) {
  return post<TaskResponse>("/tasks", {
    action: "reassign",
    id: taskId,
    owner: newOwner,
    channel: channel || "general",
    created_by: actor,
  });
}

export type TaskStatusAction =
  | "release"
  | "review"
  | "block"
  | "complete"
  | "cancel"
  | "resume"
  | "submit_for_review"
  | "request_changes"
  | "approve"
  | "archive";

export interface UpdateTaskStatusOptions {
  memoryWorkflowOverride?: boolean;
  memoryWorkflowOverrideActor?: string;
  memoryWorkflowOverrideReason?: string;
  overrideReason?: string;
}

export function updateTaskStatus(
  taskId: string,
  action: TaskStatusAction,
  channel: string,
  actor = "human",
  options?: UpdateTaskStatusOptions,
) {
  const body: Record<string, string | boolean> = {
    action,
    id: taskId,
    channel: channel || "general",
    created_by: actor,
  };
  if (options?.memoryWorkflowOverride) {
    body.memory_workflow_override = true;
    body.memory_workflow_override_actor =
      options.memoryWorkflowOverrideActor || actor;
  }
  const overrideReason =
    options?.memoryWorkflowOverrideReason || options?.overrideReason;
  if (overrideReason) {
    body.memory_workflow_override_reason = overrideReason;
    body.override_reason = overrideReason;
  }
  return post<TaskResponse>("/tasks", body);
}

export function getTasks(
  channel: string,
  opts?: { includeDone?: boolean; status?: string; mySlug?: string },
) {
  const params: Record<string, string> = {
    viewer_slug: "human",
    channel: channel || "general",
  };
  if (opts?.includeDone) params.include_done = "true";
  if (opts?.status) params.status = opts.status;
  if (opts?.mySlug) params.my_slug = opts.mySlug;
  return get<TaskListResponse>("/tasks", params);
}

/** List sub-tasks of a parent Task. Uses the broker's
 *  parent_issue_id query filter (Slice 4 follow-up). Returns an empty
 *  list when the parent has no children. */
// ── Per-Task Activity feed ──────────────────────────────────────────────

export type TaskActivityEventKind =
  | "lifecycle"
  | "comment"
  | "action"
  | "request"
  | "sub_issue";

export type TaskActivityRequestStatus = "open" | "answered" | "canceled";

export interface TaskActivityLifecycle {
  from?: string;
  to?: string;
}

export interface TaskActivityRequest {
  request_id: string;
  status: TaskActivityRequestStatus;
  question?: string;
  choice_id?: string;
  choice_text?: string;
  custom_text?: string;
  answered_at?: string;
  blocking?: boolean;
}

export interface TaskActivitySubIssue {
  sub_issue_id: string;
  title?: string;
}

export interface TaskActivityEvent {
  id: string;
  kind: TaskActivityEventKind;
  timestamp: string;
  actor?: string;
  summary?: string;
  detail?: string;
  lifecycle?: TaskActivityLifecycle;
  request?: TaskActivityRequest;
  sub_issue?: TaskActivitySubIssue;
}

export interface TaskActivityResponse {
  task_id: string;
  events: TaskActivityEvent[];
}

/**
 * Fetch the per-Task activity feed: lifecycle transitions, comments,
 * requests (with resolution), sub-task creations. Sorted oldest first
 * by the broker; the FE can reverse if it wants newest-on-top.
 */
export function getTaskActivity(taskId: string) {
  return get<TaskActivityResponse>(
    `/tasks/${encodeURIComponent(taskId)}/activity`,
  );
}

export function getSubTasks(parentTaskId: string) {
  return get<TaskListResponse>("/tasks", {
    viewer_slug: "human",
    all_channels: "true",
    include_done: "true",
    parent_issue_id: parentTaskId,
  });
}

/** Create a sub-task under a parent Task. Sub-tasks have the same
 *  shape as Tasks: title, details, optional owner. Defaults
 *  task_type=issue so the row lands on the Tasks board with the same
 *  lifecycle. */
export function createSubTask(opts: {
  parentTaskId: string;
  title: string;
  channel: string;
  details?: string;
  owner?: string;
}) {
  return post<TaskResponse>("/tasks", {
    action: "create",
    channel: opts.channel || "general",
    title: opts.title,
    details: opts.details || "",
    owner: opts.owner || "",
    created_by: "human",
    task_type: "issue",
    parent_issue_id: opts.parentTaskId,
  });
}

/** Reopen a closed Task (rejected/cancelled/approved) back to drafting
 *  so the human can re-approve to restart work. The broker preserves
 *  the title/details/owner and just resets the lifecycle. */
export function reopenTask(taskId: string, channel: string) {
  return post<TaskResponse>("/tasks", {
    action: "reopen",
    id: taskId,
    channel: channel || "general",
    created_by: "human",
  });
}

export function getOfficeTasks(opts?: {
  includeDone?: boolean;
  status?: string;
  mySlug?: string;
}) {
  const params: Record<string, string> = {
    viewer_slug: "human",
    all_channels: "true",
  };
  if (opts?.includeDone) params.include_done = "true";
  if (opts?.status) params.status = opts.status;
  if (opts?.mySlug) params.my_slug = opts.mySlug;
  return get<TaskListResponse>("/tasks", params);
}

export interface TaskLogSummary {
  taskId: string;
  agentSlug: string;
  toolCallCount: number;
  firstToolAt?: number;
  lastToolAt?: number;
  hasError?: boolean;
  sizeBytes: number;
}

export interface TaskLogEntry {
  task_id: string;
  agent_slug: string;
  tool_name: string;
  params?: Record<string, unknown>;
  result?: string;
  error?: string;
  started_at?: number;
  completed_at?: number;
}

export function listAgentLogTasks(opts?: {
  limit?: number;
  agentSlug?: string;
}) {
  const params: Record<string, string> = {};
  if (opts?.limit) params.limit = String(opts.limit);
  if (opts?.agentSlug) params.agent = opts.agentSlug;
  return get<{ tasks: TaskLogSummary[] }>("/agent-logs", params);
}

export function getAgentLogEntries(taskId: string) {
  return get<{ task: string; entries: TaskLogEntry[] }>("/agent-logs", {
    task: taskId,
  });
}
