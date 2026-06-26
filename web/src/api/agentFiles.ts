/**
 * Agent instruction-file API client — view + edit an agent's SOUL / IDENTITY /
 * OPERATIONS / TOOLS files and the office-wide USER.md.
 *
 * These hit the dedicated /agent-files/* broker endpoints (NOT /wiki/*): the
 * files live in the same git repo as the wiki but use a strict path allowlist
 * (agents/{slug}/{canonical}.md + office/USER.md) and never enter the team
 * article index. The write envelope is byte-compatible with the wiki editor's
 * `writeHumanArticle` so AgentInstructionsSection can reuse WikiEditor's
 * draft/conflict/SHA state machine unchanged.
 */

import { get, post } from "./client";
import { tryParseConflict, type WriteHumanResult } from "./wiki";

/** Canonical per-agent instruction files, in prompt-precedence order. */
export const AGENT_INSTRUCTION_FILES = [
  "SOUL",
  "IDENTITY",
  "OPERATIONS",
  "TOOLS",
] as const;

export type AgentInstructionFile = (typeof AGENT_INSTRUCTION_FILES)[number];

/** Office-wide human-context file (one per office). */
export const OFFICE_USER_FILE_PATH = "office/USER.md";

/** Repo-relative path for one of an agent's instruction files. */
export function agentFilePath(
  slug: string,
  name: AgentInstructionFile,
): string {
  return `agents/${slug}/${name}.md`;
}

/**
 * Response from GET /agent-files/read. `exists` is false when the file has not
 * been committed to disk yet — `content` then carries the deterministic seed so
 * the editor opens with real text, and the first save creates the file.
 */
export interface AgentFileResponse {
  path: string;
  content: string;
  sha: string;
  exists: boolean;
}

/** Read one agent instruction file (or the office USER file). */
export async function readAgentFile(path: string): Promise<AgentFileResponse> {
  return get<AgentFileResponse>("/agent-files/read", { path });
}

/**
 * Save a human edit to one agent instruction file. Mirrors `writeHumanArticle`:
 * the caller passes the per-file SHA they opened against (or '' for a file with
 * no history); the broker rejects with 409 when HEAD moved past it, returning
 * the current bytes so the editor can prompt re-apply.
 */
export async function writeAgentFile(params: {
  path: string;
  content: string;
  commitMessage: string;
  expectedSha: string;
}): Promise<WriteHumanResult> {
  try {
    return await post<{
      path: string;
      commit_sha: string;
      bytes_written: number;
    }>("/agent-files/write", {
      path: params.path,
      content: params.content,
      commit_message: params.commitMessage,
      expected_sha: params.expectedSha,
    });
  } catch (err: unknown) {
    // The shared post() helper surfaces non-2xx as Error(text). For 409 the
    // body is a JSON conflict envelope — parse it out so the editor can show
    // the reload prompt instead of a generic error.
    const message = err instanceof Error ? err.message : String(err);
    const parsed = tryParseConflict(message);
    if (parsed) return parsed;
    throw err;
  }
}

/** Files the broker will author with the LLM (prose only; IDENTITY/TOOLS are
 *  factual and excluded). Gates the "Generate with AI" affordance. */
export function isAIGeneratableFile(label: string): boolean {
  return label === "SOUL" || label === "OPERATIONS" || label === "USER";
}

/**
 * Ask the broker to author a richer DRAFT of one prose instruction file with
 * the LLM. Returns the draft markdown — NOT committed; the caller opens the
 * editor with it so the human reviews and saves (or discards). Throws on any
 * failure (the file already exists, so there is nothing to fall back to).
 */
export async function generateAgentFile(
  path: string,
  hint = "",
): Promise<{ path: string; content: string }> {
  return post<{ path: string; content: string }>("/agent-files/generate", {
    path,
    hint,
  });
}
