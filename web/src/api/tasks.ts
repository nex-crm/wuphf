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

export interface Task {
  id: string;
  title: string;
  description?: string;
  details?: string;
  status: string;
  owner?: string;
  created_by?: string;
  channel?: string;
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
}

export interface CreateTaskInput {
  title: string;
  assignee: string;
  details?: string;
  task_type?: string;
  execution_mode?: string;
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
