import { get, post } from "./client";

// Mirrors team.governorStatus (internal/team/governor.go). All "*SinceCheckpoint"
// fields measure spend since the human last reviewed (Continue / construction).
export type GovernorReason = "" | "manual" | "stop" | "budget" | "turns";

export interface GovernorStatus {
  paused: boolean;
  reason: GovernorReason;
  pausedAt?: string;
  turnsSinceCheckpoint: number;
  tokensSinceCheckpoint: number;
  costSinceCheckpoint: number;
  maxTokens: number;
  maxCostUsd: number;
  maxTurns: number;
  disabled: boolean;
}

export type GovernorAction = "pause" | "stop" | "resume" | "resume_more";

export interface GovernorActionOptions {
  slug?: string;
  addTokens?: number;
  addCostUsd?: number;
}

export const GOVERNOR_QUERY_KEY = ["governor"] as const;

export function getGovernor(): Promise<GovernorStatus> {
  return get<GovernorStatus>("/governor");
}

export function postGovernor(
  action: GovernorAction,
  options: GovernorActionOptions = {},
): Promise<GovernorStatus> {
  return post<GovernorStatus>("/governor", { action, ...options });
}
