import { get, post } from "./client";

/** A workflow shape spotted in office activity — either recurring across an
 * agent's tasks or run end-to-end once to a final outcome (mirrors the broker's
 * spottedWorkflow wire shape). */
export interface SpottedWorkflow {
  fingerprint: string;
  shape: string[];
  agent: string;
  task_ids: string[];
  count: number;
  /** The terminal outcome-producing step (e.g. compose_digest, slack_send)
   * that proves the run finished something. Empty when surfaced on recurrence
   * alone. */
  outcome?: string;
  spec_id: string;
  title: string;
  frozen: boolean;
}

/** An auto-drafted improvement overlay. id + reason are surfaced; the whole
 * object is passed back verbatim to improveWorkflow. */
export type WorkflowProposal = { id: string; reason?: string } & Record<
  string,
  unknown
>;

export function getProposals(
  specId: string,
): Promise<{ runs: number; proposals: WorkflowProposal[] }> {
  return post<{ runs: number; proposals: WorkflowProposal[] }>(
    "/workflows/proposals",
    { spec_id: specId },
  );
}

export interface ImproveResult {
  version: string;
  review: { accepted: boolean; shipcheck: { passed: boolean } };
}

export function improveWorkflow(
  specId: string,
  overlay: unknown,
): Promise<ImproveResult> {
  return post<ImproveResult>("/workflows/improve", {
    spec_id: specId,
    overlay,
  });
}

export function getSpottedWorkflows(): Promise<{
  workflows: SpottedWorkflow[];
}> {
  return get<{ workflows: SpottedWorkflow[] }>("/workflows/spotted");
}

/** One step of a run's audit trail (mirrors workflow.AuditEntry). */
export interface AuditEntry {
  event: string;
  from: string;
  to?: string;
  actions?: string[];
  /** When set, the step was not applied: duplicate | no_transition |
   * guard_failed | action_failed. */
  skipped?: string;
}

/** The deterministic output of executing a contract (mirrors
 * workflow.RunResult). */
export interface RunResult {
  state_seq: string[];
  actions_fired: string[];
  audit: AuditEntry[];
  final_state: string;
  deduped: number;
  /** Artifacts the actions produced on a real run (e.g. the composed digest +
   * email_count). Empty for the pure shipcheck replay. */
  outputs?: Record<string, unknown>;
}

/** A recorded execution of a frozen workflow (mirrors workflow.RunRecord). */
export interface RunRecord {
  spec_id: string;
  version?: string;
  at?: string;
  trigger: string;
  result: RunResult;
}

export function runWorkflow(specId: string): Promise<{ run: RunRecord }> {
  return post<{ run: RunRecord }>("/workflows/run", { spec_id: specId });
}

/** Kick off a new task from a workflow run: the run's outcome becomes the task
 * context, plus the operator's own start prompt. Returns the created task id +
 * channel so the UI can link to it. */
export function runToTask(
  specId: string,
  prompt: string,
  run: RunRecord,
): Promise<{ task_id: string; channel: string; title: string }> {
  return post<{ task_id: string; channel: string; title: string }>(
    "/workflows/run-to-task",
    { spec_id: specId, prompt, run },
  );
}

/** How a detected workflow believes it should fire (mirrors
 * workflow.ExtractedTrigger). Proposal metadata, not part of the contract. */
export interface ExtractedTrigger {
  kind: "manual" | "schedule" | "webhook" | "context";
  interval_minutes?: number;
  rationale?: string;
}

/** A workflow the completion-time extractor judged real, surfaced with a
 * recurrence count (mirrors team.ExtractedWorkflow). Carries an executable
 * contract the operator can freeze. */
export interface ExtractedWorkflow {
  fingerprint: string;
  name: string;
  confidence: number;
  trigger: ExtractedTrigger;
  recurrence: number;
  task_ids: string[];
  spec?: WorkflowSpec;
}

/** GET /workflows/extracted — the proactive "press this into a workflow" feed,
 * populated as tasks complete (the completion sweep + on-demand extract). */
export function getExtractedWorkflows(): Promise<{
  workflows: ExtractedWorkflow[];
}> {
  return get<{ workflows: ExtractedWorkflow[] }>("/workflows/extracted");
}

/** A node in the workflow contract's state machine. */
export interface WorkflowState {
  id: string;
  label?: string;
}

export interface WorkflowEvent {
  id: string;
  label?: string;
}

export interface WorkflowAction {
  id: string;
  kind: "deterministic" | "llm" | "external";
  description?: string;
  platform?: string;
  action_id?: string;
  /** Integration-read fields (additive): provider call args + response
   * projection authored by the workflow-builder agent. */
  params?: Record<string, unknown>;
  result_path?: string;
  expose?: string[];
}

export interface WorkflowTransition {
  from: string;
  to: string;
  on: string;
  guard?: string;
  actions?: string[];
}

/** The frozen workflow contract (mirrors workflow.Spec, fields the graph needs). */
export interface WorkflowSpec {
  version: string;
  id: string;
  goal: string;
  operator: string;
  states: WorkflowState[];
  initial: string;
  terminal?: string[];
  events: WorkflowEvent[];
  transitions: WorkflowTransition[];
  actions: WorkflowAction[];
}

/** How a frozen workflow fires (mirrors workflowTrigger). Exactly four kinds. */
export interface WorkflowTrigger {
  kind: "manual" | "schedule" | "webhook" | "context";
  label: string;
  enabled?: boolean;
  interval_minutes?: number;
  next_run?: string;
}

export function getWorkflowSpec(
  specId: string,
): Promise<{ spec: WorkflowSpec; triggers: WorkflowTrigger[] }> {
  return get<{ spec: WorkflowSpec; triggers: WorkflowTrigger[] }>(
    `/workflows/spec?spec_id=${encodeURIComponent(specId)}`,
  );
}

export function getWorkflowRuns(
  specId: string,
): Promise<{ runs: RunRecord[] }> {
  return get<{ runs: RunRecord[] }>(
    `/workflows/runs?spec_id=${encodeURIComponent(specId)}`,
  );
}

/** Ensure the per-workflow conversation channel exists (idempotent) and return
 * its slug. This is where the operator chats with @workflow-builder about THIS
 * contract from the Workflows page. */
export function ensureWorkflowChannel(
  specId: string,
): Promise<{ channel: string }> {
  return post<{ channel: string }>("/workflows/channel", { spec_id: specId });
}

export interface ShipcheckCheck {
  name: string;
  pass: boolean;
  detail?: string;
}

export interface ShipcheckReport {
  spec_id: string;
  passed: boolean;
  checks: ShipcheckCheck[];
}

export interface FreezeResult {
  skill: { id: string; name: string; title: string };
  spec_id: string;
  shipcheck: ShipcheckReport;
  created: boolean;
}

export interface DraftResult {
  spec: unknown;
  shipcheck: ShipcheckReport;
}

export function draftWorkflow(fingerprint: string): Promise<DraftResult> {
  return post<DraftResult>("/workflows/draft", { fingerprint });
}

export function freezeWorkflow(
  fingerprint: string,
  spec?: unknown,
): Promise<FreezeResult> {
  return post<FreezeResult>(
    "/workflows/freeze",
    spec === undefined ? { fingerprint } : { fingerprint, spec },
  );
}
