/**
 * Skills API — extracted from client.ts to keep that module under the
 * repo file-size budget. The shared HTTP helpers (get/post/put) are imported
 * from ./client; that import is safe despite the re-export cycle (client.ts
 * does `export * from "./skills"`) because get/post/put are hoisted function
 * declarations, initialized before the re-export line evaluates.
 */

import { trackOn } from "../lib/analytics";
import type { SkillSimilarRef } from "./client";
import { get, post, put } from "./client";

// ── Skills ──

export type SkillStatus = "active" | "proposed" | "archived" | "disabled";

export interface SkillMetadata {
  wuphf?: {
    source_articles?: string[];
  };
}

export type OwnerAgents = string[];

export interface Skill {
  name: string;
  title?: string;
  description?: string;
  source?: string;
  content?: string;
  trigger?: string;
  parameters?: unknown;
  status?: SkillStatus;
  created_by?: string;
  created_at?: string;
  updated_at?: string;
  /** Per-agent scoping (PR 7). Empty/missing = lead-routable shared skill. */
  owner_agents?: OwnerAgents;
  /** Set by the similarity gate when this skill resembles another (legacy records). */
  similar_to_existing?: SkillSimilarRef;
  metadata?: SkillMetadata;
}

export type SkillsListScope = "active" | "all";

export function getSkills() {
  return get<{ skills: Skill[] }>("/skills");
}

/**
 * Fetch the skill catalog. With scope="all" the legacy /skills endpoint
 * accepts include_archived + include_disabled flags (PR 7 task #18) so the
 * Skills app can render every section (Pending / Active / Disabled /
 * Archived) from a single query — keeping body content intact for the
 * SidePanel preview and the enhance-existing patchSkill flow.
 *
 * scope="active" returns the legacy default (active + proposed + disabled,
 * archived hidden) for callers that don't need the archived bucket.
 */
export function getSkillsList(scope: SkillsListScope = "all") {
  const params: Record<string, string> = {};
  if (scope === "all") {
    params.include_archived = "true";
    params.include_disabled = "true";
  }
  return get<{ skills: Skill[] }>("/skills", params);
}

export interface DisableSkillResponse {
  skill?: Skill;
}

export function disableSkill(name: string): Promise<DisableSkillResponse> {
  return trackOn(
    post<DisableSkillResponse>(
      `/skills/${encodeURIComponent(name)}/disable`,
      {},
    ),
    "skill_state_changed",
    { action: "disable", scope: "global" },
  );
}

export interface EnableSkillResponse {
  skill?: Skill;
}

export function enableSkill(name: string): Promise<EnableSkillResponse> {
  return trackOn(
    post<EnableSkillResponse>(`/skills/${encodeURIComponent(name)}/enable`, {}),
    "skill_state_changed",
    { action: "enable", scope: "global" },
  );
}

export interface SkillOwnerToggleResponse {
  skill?: Skill;
}

/**
 * Assign a specific skill to a specific agent. Adds the agent slug to
 * the skill's owner_agents list (idempotent). Only OwnerAgents members
 * see the skill in their AVAILABLE SKILLS prompt block and can invoke
 * it via team_skill_run.
 */
export function enableSkillForAgent(
  name: string,
  agent: string,
): Promise<SkillOwnerToggleResponse> {
  return trackOn(
    post<SkillOwnerToggleResponse>(
      `/skills/${encodeURIComponent(name)}/enable-for`,
      { agent },
    ),
    "skill_state_changed",
    { action: "enable_for_agent", scope: "agent" },
  );
}

/** Remove an agent from the skill's owner_agents list (idempotent). */
export function disableSkillForAgent(
  name: string,
  agent: string,
): Promise<SkillOwnerToggleResponse> {
  return trackOn(
    post<SkillOwnerToggleResponse>(
      `/skills/${encodeURIComponent(name)}/disable-for`,
      { agent },
    ),
    "skill_state_changed",
    { action: "disable_for_agent", scope: "agent" },
  );
}

export interface RestoreArchivedSkillResponse {
  skill?: Skill;
}

export function restoreArchivedSkill(
  name: string,
): Promise<RestoreArchivedSkillResponse> {
  return trackOn(
    post<RestoreArchivedSkillResponse>(
      `/skills/${encodeURIComponent(name)}/restore`,
      {},
    ),
    "skill_state_changed",
    { action: "restore", scope: "global" },
  );
}

export interface ArchiveSkillResponse {
  ok?: boolean;
  skill?: Skill;
}

export function archiveSkill(name: string): Promise<ArchiveSkillResponse> {
  return trackOn(
    post<ArchiveSkillResponse>(
      `/skills/${encodeURIComponent(name)}/archive`,
      {},
    ),
    "skill_state_changed",
    { action: "archive", scope: "global" },
  );
}

export interface InvokeSkillResult {
  channel?: string;
  skill?: Skill;
  task_id?: string;
}

export function invokeSkill(
  name: string,
  params?: Record<string, unknown>,
): Promise<InvokeSkillResult> {
  return post<InvokeSkillResult>(
    `/skills/${encodeURIComponent(name)}/invoke`,
    params ?? {},
  );
}

// ── Skill compile (PR 1a wiki-skill-compile) ──

export interface CompileError {
  slug: string;
  reason: string;
}

export interface CompileResult {
  scanned: number;
  matched: number;
  proposed: number;
  deduped: number;
  rejected_by_guard: number;
  errors: CompileError[];
  duration_ms: number;
  trigger: string;
}

export interface CompileQueued {
  queued: true;
}

export interface CompileSkipped {
  skipped: string;
}

export type CompileResponse = CompileResult | CompileQueued | CompileSkipped;

export function compileSkills(opts?: {
  dry_run?: boolean;
  scope_path?: string;
}) {
  return post<CompileResponse>("/skills/compile", opts ?? {});
}

export interface SkillCompileStats {
  last_run_at?: string;
  total_runs?: number;
  total_proposed?: number;
  total_deduped?: number;
  total_rejected_by_guard?: number;
  [key: string]: unknown;
}

export function getSkillCompileStats() {
  return get<SkillCompileStats>("/skills/compile/stats");
}

export interface ApproveSkillResponse {
  skill?: Skill;
}

export function approveSkill(name: string): Promise<ApproveSkillResponse> {
  return trackOn(
    post<ApproveSkillResponse>(
      `/skills/${encodeURIComponent(name)}/approve`,
      {},
    ),
    "skill_state_changed",
    { action: "approve", scope: "global" },
  );
}

export interface RejectSkillResponse {
  ok: boolean;
  undo_token: string;
  skill_name: string;
  expires_in: number;
}

export function rejectSkill(
  name: string,
  reason?: string,
): Promise<RejectSkillResponse> {
  return trackOn(
    post<RejectSkillResponse>(
      `/skills/${encodeURIComponent(name)}/reject`,
      reason ? { reason } : {},
    ),
    "skill_state_changed",
    { action: "reject", scope: "global" },
  );
}

export interface UndoRejectSkillResponse {
  skill?: Skill;
}

export function undoRejectSkill(
  undoToken: string,
): Promise<UndoRejectSkillResponse> {
  return post<UndoRejectSkillResponse>(`/skills/reject/undo`, {
    undo_token: undoToken,
  });
}

export interface PatchSkillRequest {
  old_string: string;
  new_string: string;
  replace_all?: boolean;
}

export interface PatchSkillResponse {
  skill?: Skill;
}

/**
 * Edit-tool style find/replace patch against a skill's body.
 * Used by the enhance-existing flow (PR 7 task #14) to fold a candidate
 * proposal into an existing skill without losing provenance.
 */
export function patchSkill(
  name: string,
  body: PatchSkillRequest,
): Promise<PatchSkillResponse> {
  return post<PatchSkillResponse>(
    `/skills/${encodeURIComponent(name)}/patch`,
    body,
  );
}

export interface EditSkillContentResponse {
  skill?: Skill;
}

/**
 * Full SKILL.md body replacement. Caller passes the entire rendered
 * SKILL.md (frontmatter + body). The broker re-parses, re-runs the
 * safety scan with the original creator's trust level, and rewrites the
 * wiki article. Used by the full-screen skill detail editor.
 */
export function editSkillContent(
  name: string,
  content: string,
): Promise<EditSkillContentResponse> {
  return trackOn(
    put<EditSkillContentResponse>(`/skills/${encodeURIComponent(name)}`, {
      content,
    }),
    "skill_state_changed",
    { action: "edit", scope: "global" },
  );
}
