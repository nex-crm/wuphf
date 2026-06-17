import { get, post } from "./client";

/** A repeated workflow shape spotted across an agent's tasks (mirrors the
 * broker's spottedWorkflow wire shape). */
export interface SpottedWorkflow {
  fingerprint: string;
  shape: string[];
  agent: string;
  task_ids: string[];
  count: number;
  title: string;
  frozen: boolean;
}

export function getSpottedWorkflows(): Promise<{
  workflows: SpottedWorkflow[];
}> {
  return get<{ workflows: SpottedWorkflow[] }>("/workflows/spotted");
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
