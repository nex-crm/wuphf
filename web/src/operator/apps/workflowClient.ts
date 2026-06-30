// Operator app workflow client — the DETERMINISTIC compile-and-freeze path.
//
// An app's automation is compiled ONCE from its real capabilities and frozen
// (POST .../workflow/compile), then the same saved plan runs every time
// (POST .../workflow/run). The LLM only ever runs at compile; runs are
// deterministic. GET .../workflow reads the frozen plan back. Mirrors the Go
// handlers in internal/team/broker_operator_workflow.go.

import { get, post } from "../../api/client";

/**
 * One step of a frozen workflow definition. `gated` means the step mutates an
 * external system, so a real run holds it for human approval. Mirrors the Go
 * action.WorkflowStepView.
 */
export interface WorkflowStepView {
  id: string;
  type: string;
  description?: string;
  platform?: string;
  action_id?: string;
  run_if?: string;
  template?: string;
  gated: boolean;
}

export interface AppWorkflow {
  compiled: boolean;
  workflow_key: string;
  title?: string;
  steps?: WorkflowStepView[];
  runs?: unknown[];
}

export interface WorkflowRunResult {
  ok: boolean;
  workflow_key: string;
  dry_run: boolean;
  run_id: string;
  status: string;
  steps: Record<string, unknown>;
}

/** One connectable account for a platform the workflow uses. */
export interface WorkflowConnection {
  key: string;
  name: string;
}

/** The platforms a frozen workflow calls + the operator's accounts for each. */
export interface WorkflowPlatformConnections {
  platform: string;
  connections: WorkflowConnection[];
  multiple: boolean;
}

export interface WorkflowConnectionsResult {
  platforms: WorkflowPlatformConnections[];
}

/** Per-platform connection choice the operator makes (platform -> connection key). */
export type ConnectionChoice = Record<string, string>;

/** Read the app's frozen workflow (or { compiled: false } if none yet). */
export async function getAppWorkflow(appId: string): Promise<AppWorkflow> {
  return get<AppWorkflow>(
    `/operator/apps/${encodeURIComponent(appId)}/workflow`,
  );
}

/**
 * Compile the app into a deterministic workflow: derive the plan from its real
 * capabilities, bind it once, and freeze it. Returns the frozen steps.
 */
export async function compileAppWorkflow(appId: string): Promise<AppWorkflow> {
  return post<AppWorkflow>(
    `/operator/apps/${encodeURIComponent(appId)}/workflow/compile`,
    {},
  );
}

/**
 * List the platforms the frozen workflow calls and the operator's active
 * accounts for each, so the UI can show a chooser when a platform has more than
 * one. Empty when the workflow has no external action steps.
 */
export async function getAppWorkflowConnections(
  appId: string,
): Promise<WorkflowConnectionsResult> {
  return get<WorkflowConnectionsResult>(
    `/operator/apps/${encodeURIComponent(appId)}/workflow/connections`,
  );
}

/**
 * Run the frozen workflow. Dry run by default: the operator previews exactly
 * what the deterministic plan does before anything mutates an external system.
 * `connections` carries the operator's per-platform account choice so a platform
 * with multiple accounts is disambiguated instead of erroring.
 */
export async function runAppWorkflow(
  appId: string,
  dryRun = true,
  connections?: ConnectionChoice,
): Promise<WorkflowRunResult> {
  return post<WorkflowRunResult>(
    `/operator/apps/${encodeURIComponent(appId)}/workflow/run`,
    { dry_run: dryRun, connections },
  );
}
