/**
 * Sources API client — the immutable source layer the Karpathy-style wiki is
 * compiled FROM. Thin wrapper over the shared fetch helper in `client.ts`.
 *
 * Backend surface (see internal/team/broker_sources.go + broker_compile.go):
 *   GET  /sources/list            -> { sources: SourceMetadata[] }
 *   GET  /sources/read?kind=&id=  -> SourceRecord (404 when missing)
 *   POST /sources/ingest          -> { id, path, sha }  (kind doc|url|note)
 *   POST /wiki/compile            -> CompileResult
 */

import { get, post } from "./client";

/**
 * Where a source record came from. `task`/`decision`/`chat` are captured by
 * internal office hooks; `doc`/`url`/`note` are explicit ingests. Mirrors Go's
 * `team.SourceKind`.
 */
export type SourceKind = "task" | "decision" | "chat" | "doc" | "url" | "note";

/** Every valid source kind, in the order the browser groups them. */
export const SOURCE_KINDS: readonly SourceKind[] = [
  "task",
  "decision",
  "chat",
  "doc",
  "url",
  "note",
];

/** The subset a client may POST to /sources/ingest. */
export type IngestSourceKind = "doc" | "url" | "note";

/** Human-readable label for each kind (used by the browser + citation popover). */
export const SOURCE_KIND_LABELS: Record<SourceKind, string> = {
  task: "Task",
  decision: "Decision",
  chat: "Chat",
  doc: "Document",
  url: "URL",
  note: "Note",
};

const SOURCE_KIND_SET = new Set<string>(SOURCE_KINDS);

/** Narrow an arbitrary string to a known SourceKind. */
export function isSourceKind(value: string): value is SourceKind {
  return SOURCE_KIND_SET.has(value);
}

/**
 * Derive the source kind from a source id. Compiled-article citation markers
 * encode the kind as the id's prefix before the first "-" (e.g. "task-wup-12"
 * -> "task", "chat-general-2026-06-25" -> "chat"). Returns null when the
 * prefix is not a known kind so callers can render a graceful fallback.
 */
export function kindFromSourceId(id: string): SourceKind | null {
  const trimmed = id.trim();
  const dash = trimmed.indexOf("-");
  if (dash <= 0) return null;
  const prefix = trimmed.slice(0, dash);
  return isSourceKind(prefix) ? prefix : null;
}

/** List-payload shape: every SourceRecord field except the Content body. */
export interface SourceMetadata {
  id: string;
  kind: SourceKind;
  title: string;
  origin?: string;
  captured_at: string;
  content_hash: string;
}

/** Full record including the (potentially large) Content body. */
export interface SourceRecord extends SourceMetadata {
  content: string;
}

/**
 * GET /sources/list — metadata for every captured source, newest first.
 * Throws on backend error so the browser can show an honest failure state
 * (matching fetchCatalogStrict's deliberate non-swallowing contract).
 */
export async function listSources(): Promise<SourceMetadata[]> {
  const res = await get<{ sources: SourceMetadata[] }>("/sources/list");
  return Array.isArray(res?.sources) ? res.sources : [];
}

/**
 * GET /sources/read — the full record (including content) for one source.
 * Propagates the broker's 404 (ApiError, status 404) so callers can show a
 * "source not found" state rather than blanking.
 */
export async function readSource(
  kind: SourceKind,
  id: string,
): Promise<SourceRecord> {
  return get<SourceRecord>("/sources/read", { kind, id });
}

export interface IngestSourceInput {
  kind: IngestSourceKind;
  title: string;
  origin?: string;
  content: string;
}

export interface IngestSourceResult {
  id: string;
  path: string;
  sha: string;
}

/** POST /sources/ingest — capture one explicit-ingest source (doc|url|note). */
export async function ingestSource(
  input: IngestSourceInput,
): Promise<IngestSourceResult> {
  return post<IngestSourceResult>("/sources/ingest", {
    kind: input.kind,
    title: input.title,
    origin: input.origin ?? "",
    content: input.content,
  });
}

/**
 * Tally returned by POST /wiki/compile. Mirrors Go's `team.CompileResult`.
 * `errors` collects non-fatal per-source / per-page failures.
 */
export interface CompileResult {
  pages_written: number;
  concepts: number;
  sources_read: number;
  errors?: string[];
}

/**
 * POST /wiki/compile — run the deterministic compile engine over the live
 * source layer, writing Wikipedia-shaped cited articles into the wiki. The
 * call can take a while (two narrow LLM seams per concept); callers should
 * show a pending state while it runs.
 */
export async function compileWiki(): Promise<CompileResult> {
  return post<CompileResult>("/wiki/compile", null);
}
