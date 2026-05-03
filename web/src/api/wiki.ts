/**
 * Wiki API client — thin wrapper over the shared fetch helper in `client.ts`.
 */

import { get, post, sseURL } from "./client";

export interface WikiArticle {
  path: string;
  title: string;
  content: string;
  last_edited_by: string;
  last_edited_ts: string;
  /**
   * Short SHA of the most recent commit touching this article. Sent back
   * as `expected_sha` when the editor saves so the broker can detect
   * concurrent writes that landed after the editor opened. Empty for
   * brand-new articles that have no commit history yet.
   */
  commit_sha?: string;
  revisions: number;
  contributors: string[];
  backlinks: { path: string; title: string; author_slug: string }[];
  word_count: number;
  categories: string[];
  /** ISO-8601 timestamp of the last access by any reader (human or agent). Null if never accessed. */
  last_read?: string | null;
  /** Number of accesses from the web UI (human readers). Absent when zero. */
  human_read_count?: number;
  /** Number of accesses from agent MCP tool calls. Absent when zero. */
  agent_read_count?: number;
  /** Whole days since last_read; 0 if accessed today. */
  days_unread?: number;
  /** True when the article is a ghost placeholder stub (frontmatter ghost: true). */
  ghost?: boolean;
  /** True when a synthesis job is in-flight for this ghost article. Show a "generating..." indicator. Never true when ghost is false. */
  synthesis_queued?: boolean;
}

/**
 * Result envelope for a successful human wiki write.
 */
export interface WriteHumanOk {
  path: string;
  commit_sha: string;
  bytes_written: number;
}

/**
 * 409 Conflict payload: returned when another write landed between the
 * editor opening and the save. Carries the current article bytes so the
 * editor can prompt reload without a second fetch.
 */
export interface WriteHumanConflict {
  conflict: true;
  error: string;
  current_sha: string;
  current_content: string;
}

export type WriteHumanResult = WriteHumanOk | WriteHumanConflict;

/**
 * Submit a human-authored wiki write. The caller must pass the SHA of
 * the article version they opened (or '' for a new article); the broker
 * rejects the write with 409 when HEAD has moved past that SHA.
 *
 * Agents never hit this endpoint — it is HTTP-only, not exposed via MCP.
 */
export async function writeHumanArticle(params: {
  path: string;
  content: string;
  commitMessage: string;
  expectedSha: string;
}): Promise<WriteHumanResult> {
  try {
    const res = await post<WriteHumanOk>("/wiki/write-human", {
      path: params.path,
      content: params.content,
      commit_message: params.commitMessage,
      expected_sha: params.expectedSha,
    });
    return res;
  } catch (err: unknown) {
    // The shared post() helper surfaces non-2xx as Error(text). For 409
    // the body is a JSON envelope — try to parse it out.
    const message = err instanceof Error ? err.message : String(err);
    const parsed = tryParseConflict(message);
    if (parsed) return parsed;
    throw err;
  }
}

function tryParseConflict(text: string): WriteHumanConflict | null {
  try {
    const data = JSON.parse(text) as Partial<WriteHumanConflict> & {
      error?: string;
      current_sha?: string;
      current_content?: string;
    };
    if (
      typeof data.current_sha === "string" &&
      typeof data.current_content === "string"
    ) {
      return {
        conflict: true,
        error: data.error ?? "conflict",
        current_sha: data.current_sha,
        current_content: data.current_content,
      };
    }
  } catch {
    // not a JSON body; fall through
  }
  return null;
}

export interface WikiCatalogEntry {
  path: string;
  title: string;
  author_slug: string;
  last_edited_ts: string;
  group: string;
  /** ISO-8601 timestamp of the last access by any reader. Null if never accessed. */
  last_read?: string | null;
  human_read_count?: number;
  agent_read_count?: number;
  days_unread?: number;
  /** True when the entry is an archived tombstone. Only present with ?include_archived=true. */
  archived?: boolean;
}

/**
 * Dynamic section discovered from actual wiki content + the blueprint's
 * declared wiki_schema. Maps 1:1 to Go's `team.DiscoveredSection`.
 *
 * A section is "from_schema" when the active blueprint declared it in
 * wiki_schema.dirs. Otherwise it emerged organically from articles the
 * team wrote. Both shapes ship in the same list so the sidebar can
 * distinguish them visually.
 */
export interface DiscoveredSection {
  slug: string;
  title: string;
  article_paths: string[];
  article_count: number;
  first_seen_ts: string;
  last_update_ts: string;
  from_schema: boolean;
}

export interface WikiSectionsUpdatedEvent {
  sections: DiscoveredSection[];
  timestamp: string;
}

export interface WikiHistoryCommit {
  sha: string;
  author_slug: string;
  msg: string;
  date: string;
}

export interface WikiEditLogEntry {
  who: string;
  action: "edited" | "created" | "updated" | "wrote";
  article_path: string;
  article_title: string;
  timestamp: string;
  commit_sha: string;
}

/**
 * Candidate wiki paths for a given hash slug. Wikilinks in briefs use
 * a bare slug (e.g. `[[nazz]]`), which routes to URL `#/wiki/nazz`. The
 * broker's `/wiki/article` endpoint requires a full `team/{group}/{slug}.md`
 * path, so the client resolves bare slugs by trying each standard group
 * directory in order before giving up.
 *
 * Full paths (`team/…`) and any input already containing a slash are
 * passed through unchanged (with a `.md` suffix added if missing). Bare
 * slugs fan out across the standard groups in priority order. The 404s
 * on misses are cheap and there's no coherence risk — the first match
 * wins.
 */
function candidatePaths(pathOrSlug: string): string[] {
  const trimmed = pathOrSlug.trim().replace(/^\/+/, "").replace(/\/+$/, "");
  if (!trimmed) return [];
  const withExt = trimmed.endsWith(".md") ? trimmed : `${trimmed}.md`;
  if (trimmed.startsWith("team/")) return [withExt];
  if (trimmed.includes("/")) return [`team/${withExt}`];
  const slug = withExt;
  return [
    `team/people/${slug}`,
    `team/companies/${slug}`,
    `team/playbooks/${slug}`,
    `team/decisions/${slug}`,
    `team/projects/${slug}`,
    `team/${slug}`,
  ];
}

export interface CompressArticleResponse {
  queued: boolean;
  in_flight: boolean;
  path: string;
}

/**
 * Asks the broker to compress the given article. The compressor is a
 * background goroutine; the response just acknowledges enqueue / debounce.
 *
 * - `queued: true` — a fresh compress job was scheduled. Toast: "Compressing…"
 * - `in_flight: true` — a job is already running for this article. Toast:
 *   "Already compressing, check back soon."
 *
 * The compressed article shows up after the worker commits; callers should
 * refetch the article on a subsequent navigation or via a manual refresh.
 */
export async function compressArticle(
  path: string,
): Promise<CompressArticleResponse> {
  // Normalize non-canonical paths (e.g. "people/nazz") to canonical form
  // (e.g. "team/people/nazz.md") so validateArticlePath on the server
  // doesn't reject paths that fetchArticle would resolve successfully.
  const canonical = candidatePaths(path)[0] ?? path;
  return post<CompressArticleResponse>(
    `/wiki/compress?path=${encodeURIComponent(canonical)}`,
  );
}

export async function fetchArticle(path: string): Promise<WikiArticle> {
  const tried: string[] = [];
  let lastError: unknown = null;
  for (const candidate of candidatePaths(path)) {
    tried.push(candidate);
    try {
      return await get<WikiArticle>(
        `/wiki/article?path=${encodeURIComponent(candidate)}&reader=web`,
      );
    } catch (err) {
      lastError = err;
      // Try next candidate. Real 404s and bare-slug misses look identical
      // from the client, so the first successful canonical path wins.
    }
  }
  if (lastError instanceof Error) throw lastError;
  throw new Error(`Article not found: ${tried[tried.length - 1] ?? path}`);
}

/**
 * GET /wiki/sections — the v1.3 dynamic-section IA. Returns blueprint-
 * declared sections (in blueprint order) followed by discovered
 * sections (alphabetical). Empty array on backend error so the sidebar
 * can fall back to the catalog-derived group set without blanking.
 */
export async function fetchSections(): Promise<DiscoveredSection[]> {
  try {
    const res = await get<{ sections: DiscoveredSection[] }>("/wiki/sections");
    return Array.isArray(res?.sections) ? res.sections : [];
  } catch {
    return [];
  }
}

/**
 * Subscribe to the shared broker `/events` SSE stream filtered to
 * `wiki:sections_updated` events. Returns an unsubscribe function.
 *
 * Named event pattern matches subscribeEditLog + subscribeEntityEvents.
 * Do NOT switch to onmessage — the broker only emits named events and
 * the default handler never fires for named payloads.
 */
export function subscribeSectionsUpdated(
  handler: (event: WikiSectionsUpdatedEvent) => void,
): () => void {
  let closed = false;
  let source: EventSource | null = null;
  let onEvent: ((ev: MessageEvent) => void) | null = null;

  try {
    const ES = (globalThis as { EventSource?: typeof EventSource }).EventSource;
    if (!ES)
      return () => {
        closed = true;
      };
    source = new ES(sseURL("/events"));
    onEvent = (ev: MessageEvent) => {
      if (closed) return;
      try {
        const data = JSON.parse(ev.data) as WikiSectionsUpdatedEvent;
        if (data && Array.isArray(data.sections)) {
          handler(data);
        }
      } catch {
        // ignore malformed events
      }
    };
    source.addEventListener("wiki:sections_updated", onEvent as EventListener);
  } catch {
    source = null;
  }

  return () => {
    closed = true;
    if (source && onEvent) {
      source.removeEventListener(
        "wiki:sections_updated",
        onEvent as EventListener,
      );
    }
    if (source) {
      source.close();
      source = null;
    }
  };
}

/**
 * Registered human identity surfaced by the broker at GET /humans. The
 * server grows this list as it observes new commits, so team installs
 * with multiple humans all show up without any client configuration.
 */
export interface HumanIdentity {
  name: string;
  email: string;
  slug: string;
}

/**
 * GET /humans — returns identities observed or probed server-side. The
 * byline component uses this to turn a commit author slug into the
 * human's real display name. Returns [] on any error so the UI falls
 * back to the slug-derived label without blanking.
 */
export async function fetchHumans(): Promise<HumanIdentity[]> {
  try {
    const res = await get<{ humans: HumanIdentity[] }>("/humans");
    return Array.isArray(res?.humans) ? res.humans : [];
  } catch {
    return [];
  }
}

export async function fetchCatalog(): Promise<WikiCatalogEntry[]> {
  try {
    const res = await get<{ articles: WikiCatalogEntry[] }>("/wiki/catalog");
    return Array.isArray(res?.articles) ? res.articles : [];
  } catch {
    return [];
  }
}

/**
 * One hit from `/wiki/search` — mirrors Go's `team.WikiSearchHit`.
 * The broker returns literal substring hits (no regex), capped at 100.
 */
export interface WikiSearchHit {
  path: string;
  line: number;
  snippet: string;
}

/**
 * GET /wiki/search?pattern=... — literal substring search across team/**.md.
 * Returns [] on any error so the SearchModal can render empty state without
 * blowing up.
 */
export async function searchWiki(pattern: string): Promise<WikiSearchHit[]> {
  const trimmed = pattern.trim();
  if (!trimmed) return [];
  try {
    const res = await get<{ hits: WikiSearchHit[] }>(
      `/wiki/search?pattern=${encodeURIComponent(trimmed)}`,
    );
    return Array.isArray(res?.hits) ? res.hits : [];
  } catch {
    return [];
  }
}

export interface WikiAuditEntry {
  sha: string;
  author_slug: string;
  timestamp: string;
  message: string;
  paths: string[];
}

export async function fetchAuditLog(
  params: { limit?: number; since?: string } = {},
): Promise<{ entries: WikiAuditEntry[]; total: number }> {
  const qs = new URLSearchParams();
  if (typeof params.limit === "number") qs.set("limit", String(params.limit));
  if (params.since) qs.set("since", params.since);
  const url = qs.toString() ? `/wiki/audit?${qs.toString()}` : "/wiki/audit";
  try {
    return await get<{ entries: WikiAuditEntry[]; total: number }>(url);
  } catch {
    return { entries: [], total: 0 };
  }
}

// ── Lint API ──────────────────────────────────────────────────────────────────

/**
 * One finding from the daily lint run.
 * Mirrors internal/team.LintFinding exactly.
 */
export interface LintFinding {
  severity: "critical" | "warning" | "info";
  type:
    | "contradictions"
    | "orphans"
    | "stale"
    | "missing_crossrefs"
    | "dedup_review";
  entity_slug?: string;
  fact_ids?: string[];
  summary: string;
  /**
   * Only present on contradictions findings. Three entries:
   * ["Fact A (id: …): …", "Fact B (id: …): …", "Both"]
   */
  resolve_actions?: string[];
}

export interface LintReport {
  date: string;
  findings: LintFinding[];
}

/**
 * POST /wiki/lint/run — triggers all 5 lint checks and returns the report.
 */
export async function runLint(): Promise<LintReport> {
  return await post<LintReport>("/wiki/lint/run", null);
}

/**
 * POST /wiki/lint/resolve — resolves a contradiction finding.
 *
 * The caller echoes the full LintFinding it received from /wiki/lint/run so
 * the broker can resolve without re-running or persisting structured findings.
 */
export async function resolveContradiction(
  args: {
    report_date: string;
    finding_idx: number;
    finding: LintFinding;
    winner: "A" | "B" | "Both";
  },
  options: { signal?: AbortSignal } = {},
): Promise<{ commit_sha: string; message: string }> {
  return await post<{ commit_sha: string; message: string }>(
    "/wiki/lint/resolve",
    args,
    options,
  );
}

export async function fetchHistory(
  path: string,
): Promise<{ commits: WikiHistoryCommit[] }> {
  try {
    return await get<{ commits: WikiHistoryCommit[] }>(
      `/wiki/history/${encodeURI(path)}`,
    );
  } catch {
    return { commits: [] };
  }
}

/**
 * Subscribe to the shared broker `/events` SSE stream filtered to
 * `wiki:write` events. Returns an unsubscribe function that tears down
 * the underlying EventSource.
 *
 * Previously this subscribed to `/wiki/stream` — a path that never
 * existed on the broker. Every call 404'd silently and live edit-log
 * updates were dead in production. Matches the `api/entity.ts` pattern:
 * broker emits named SSE events (`event: wiki:write\ndata: ...`) so we
 * use `addEventListener('wiki:write', ...)` not `onmessage`.
 */
export function subscribeEditLog(
  handler: (entry: WikiEditLogEntry) => void,
): () => void {
  let closed = false;
  let source: EventSource | null = null;
  let onWrite: ((ev: MessageEvent) => void) | null = null;

  try {
    const ES = (globalThis as { EventSource?: typeof EventSource }).EventSource;
    if (!ES)
      return () => {
        closed = true;
      };
    source = new ES(sseURL("/events"));
    onWrite = (ev: MessageEvent) => {
      if (closed) return;
      try {
        const data = JSON.parse(ev.data) as Record<string, unknown>;
        // Broker ships `{path, commit_sha, author_slug, timestamp}` on
        // wiki:write. The edit-log UI's WikiEditLogEntry contract uses
        // `who`/`action`/`article_path`/`article_title`, so normalize
        // here rather than leaving undefined fields that crash
        // downstream consumers (e.g. EditLogFooter's
        // entry.who.toLowerCase()).
        const raw = (data.entry ?? data) as Record<string, unknown>;
        const path = String(raw.article_path ?? raw.path ?? "");
        const entry: WikiEditLogEntry = {
          who: String(raw.who ?? raw.author_slug ?? "unknown"),
          action: (raw.action as WikiEditLogEntry["action"]) ?? "edited",
          article_path: path,
          article_title:
            (raw.article_title as string) ??
            (path.split("/").pop() ?? path).replace(/\.md$/, ""),
          timestamp: String(raw.timestamp ?? new Date().toISOString()),
          commit_sha: String(raw.commit_sha ?? ""),
        };
        handler(entry);
      } catch {
        // ignore malformed events
      }
    };
    source.addEventListener("wiki:write", onWrite as EventListener);
  } catch {
    source = null;
  }

  return () => {
    closed = true;
    if (source && onWrite) {
      source.removeEventListener("wiki:write", onWrite as EventListener);
    }
    if (source) {
      source.close();
      source = null;
    }
  };
}
