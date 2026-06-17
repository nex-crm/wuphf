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

export interface FreezeResult {
  skill: { id: string; name: string; title: string };
  created: boolean;
}

export function freezeWorkflow(fingerprint: string): Promise<FreezeResult> {
  return post<FreezeResult>("/workflows/freeze", { fingerprint });
}
