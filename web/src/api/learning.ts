import { get } from "./client";

export type LearningType =
  | "pattern"
  | "pitfall"
  | "preference"
  | "architecture"
  | "tool"
  | "operational";

export type LearningSource =
  | "user-stated"
  | "observed"
  | "inferred"
  | "execution"
  | "synthesis"
  | "cross-agent"
  | "cross-model";

export interface TeamLearning {
  id: string;
  type: LearningType;
  key: string;
  insight: string;
  confidence: number;
  effective_confidence: number;
  source: LearningSource;
  trusted: boolean;
  scope: string;
  playbook_slug?: string;
  execution_id?: string;
  task_id?: string;
  files?: string[];
  entities?: string[];
  created_by: string;
  created_at: string;
  supersedes?: string;
}

export interface LearningSearchOptions {
  query?: string;
  scope?: string;
  type?: LearningType;
  source?: LearningSource;
  trusted?: boolean;
  playbook_slug?: string;
  file?: string;
  limit?: number;
}

export async function fetchTeamLearnings(
  opts: LearningSearchOptions = {},
): Promise<TeamLearning[]> {
  const params: Record<string, string | number | boolean> = {};
  if (opts.query) params.query = opts.query;
  if (opts.scope) params.scope = opts.scope;
  if (opts.type) params.type = opts.type;
  if (opts.source) params.source = opts.source;
  if (opts.trusted !== undefined) params.trusted = opts.trusted;
  if (opts.playbook_slug) params.playbook_slug = opts.playbook_slug;
  if (opts.file) params.file = opts.file;
  if (opts.limit !== undefined) params.limit = opts.limit;
  const res = await get<{ learnings?: TeamLearning[] }>("/learning/search", {
    ...params,
  });
  return Array.isArray(res?.learnings) ? res.learnings : [];
}
