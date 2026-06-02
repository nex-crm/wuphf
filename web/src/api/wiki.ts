/**
 * Wiki API client — thin wrapper over the shared fetch helper in `client.ts`.
 */

import { del, get, post, sseURL } from "./client";

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

// ── Wiki file surface (GET /wiki/tree, GET /wiki/file) ──────────────────────

/**
 * One node in the wiki file tree. Mirrors Go's `team.TreeNode`.
 *
 * - `path` is repo-root-relative, slash-separated, and includes the `team/`
 *   prefix. For a page it is byte-identical to what /wiki/catalog emits.
 * - `children` is populated only for `dir` nodes. Apps and websites are
 *   leaves even though they are directories on disk.
 * - `ext` is populated only for `file` nodes (lowercase, with the dot).
 */
export interface WikiFSTreeNode {
  name: string;
  path: string;
  type: "dir" | "page" | "file" | "app" | "website";
  title: string;
  ext?: string;
  children?: WikiFSTreeNode[];
}

/**
 * GET /wiki/tree — the wiki directory + page + app/website tree. An
 * optional `subPath` (repo-root-relative, e.g. "team/people") scopes the
 * walk to a subtree; omit it for the whole `team/` tree. The response
 * shape is `{ "nodes": WikiFSTreeNode[] }`; this returns the nodes array.
 */
export async function fetchWikiTree(
  subPath?: string,
): Promise<WikiFSTreeNode[]> {
  const trimmed = subPath?.trim();
  const url = trimmed
    ? `/wiki/tree?path=${encodeURIComponent(trimmed)}`
    : "/wiki/tree";
  const res = await get<{ nodes: WikiFSTreeNode[] }>(url);
  return Array.isArray(res?.nodes) ? res.nodes : [];
}

/**
 * Build the GET /wiki/file URL for a repo-root-relative `path` (e.g.
 * "team/site/index.html"). Returned as a URL — not a fetch — because callers
 * use it directly as an `<img>` / `<iframe>` src, which cannot carry an
 * Authorization header. In direct-broker mode `sseURL` appends the auth token
 * as a query param (same pattern as the SSE stream); in proxy mode the cookie-
 * less same-origin `/api` prefix is used. We compose the `path` query onto the
 * base `sseURL("/wiki/file")` with the correct separator so the token param
 * (when present) is preserved.
 */
export function wikiFileUrl(path: string): string {
  const base = sseURL("/wiki/file");
  const sep = base.includes("?") ? "&" : "?";
  return `${base}${sep}path=${encodeURIComponent(path)}`;
}

/**
 * Build the embedded-app entry URL for an app/website wiki folder. `folderPath`
 * is a repo-root-relative directory under `team/` (e.g. "team/site/dashboard");
 * the broker serves the folder's `index.html` from GET /wiki/app/<folderPath>/index.html
 * with relative assets (./styles.css, ./app.js) resolving against the same path.
 *
 * Unlike wikiFileUrl, this URL carries NO auth token. The route is served on the
 * loopback origin and the app loads inside a sandboxed iframe WITHOUT
 * allow-same-origin (see WebsiteViewer); embedding the broker token in the URL
 * would hand a bearer credential to untrusted, agent-authored app code, which
 * could then exfiltrate it. We pick the same base as sseURL (proxy `/api` prefix
 * vs the direct broker origin) but strip the `?token=...` query the SSE helper
 * appends so only the bare path crosses into the sandbox.
 *
 * Each path segment is URL-encoded but the slash separators are preserved so the
 * broker still routes on the folder hierarchy.
 */
export function appUrl(folderPath: string): string {
  // sseURL gives us the right base (proxy vs direct broker) but appends the
  // auth token as `?token=...` in direct mode. Strip everything from the first
  // `?` so the token never rides into the sandboxed app.
  const baseWithToken = sseURL("/wiki/app");
  const base = baseWithToken.split("?")[0];

  const encoded = String(folderPath)
    .trim()
    .replace(/^\/+/, "")
    .replace(/\/+$/, "")
    .split("/")
    .map((segment) => encodeURIComponent(segment))
    .join("/");

  return `${base}/${encoded}/index.html`;
}

// ── Wiki page mutations (Slice 2 — create / move / rename / delete) ─────────
//
// These drive the drag-and-drop file tree in components/wiki/tree. Each maps to
// a broker endpoint that commits a single git change and, for move/rename,
// rewrites any wikilinks that pointed at the old path so references never break.
// `path`/`from`/`to` are always repo-root-relative with the `team/` prefix and a
// `.md` suffix, byte-identical to what /wiki/tree and /wiki/catalog emit.

/** Result envelope for POST /wiki/page/create. */
export interface CreatePageResult {
  path: string;
  commit_sha: string;
}

/**
 * Result envelope for POST /wiki/page/move. `references_rewritten` is the count
 * of wikilinks the broker repointed at the new path; `rewritten_paths` lists the
 * articles it touched so the UI can surface "Rewrote N links" with detail.
 */
export interface MovePageResult {
  to: string;
  commit_sha: string;
  references_rewritten: number;
  rewritten_paths: string[];
}

/** Result envelope for POST /wiki/page/rename. */
export interface RenamePageResult {
  to: string;
  commit_sha: string;
  references_rewritten: number;
}

/** Result envelope for DELETE /wiki/page. */
export interface DeletePageResult {
  path: string;
  commit_sha: string;
}

/** Result envelope for POST /wiki/upload. */
export interface UploadFileResult {
  path: string;
  commit_sha: string;
}

/**
 * Upload a file into a wiki folder via multipart POST /wiki/upload. `dir` is
 * a repo-root-relative directory under `team/` (e.g. "team/assets"); `file` is
 * the browser File from a drop or picker. The broker derives a safe basename,
 * blocks executable extensions, writes under `team/<dir>/<name>` with collision
 * suffixing, and commits the change as the human, returning the canonical path
 * plus commit SHA.
 *
 * This cannot reuse the shared `post()` helper: multipart bodies must NOT carry
 * an explicit `Content-Type` header (the browser sets the boundary), whereas
 * `post()` hardcodes `application/json`. We build the URL with `sseURL` so the
 * auth token rides as a query param in direct-broker mode (the same tokened-URL
 * pattern as `wikiFileUrl`), and the cookieless same-origin `/api` prefix in
 * proxy mode.
 */
export async function uploadWikiFile(
  dir: string,
  file: File,
): Promise<UploadFileResult> {
  const form = new FormData();
  form.append("dir", dir);
  form.append("file", file, file.name);

  const res = await fetch(sseURL("/wiki/upload"), {
    method: "POST",
    body: form,
  });
  if (!res.ok) {
    const text = (await res.text().catch(() => "")).trim();
    let message = text || `Upload failed with status ${res.status}`;
    try {
      const parsed = JSON.parse(text) as { error?: unknown };
      if (typeof parsed.error === "string" && parsed.error) {
        message = parsed.error;
      }
    } catch {
      // Non-JSON body (e.g. a 413 from MaxBytesReader); keep the raw text.
    }
    throw new Error(message);
  }
  return (await res.json()) as UploadFileResult;
}

/**
 * Create a new wiki page at `path`. The broker commits a stub (or the supplied
 * `content`) and returns the canonical path + commit SHA. `title` seeds the H1
 * when `content` is omitted.
 */
export async function createPage(params: {
  path: string;
  title?: string;
  content?: string;
}): Promise<CreatePageResult> {
  const body: Record<string, string> = { path: params.path };
  if (params.title !== undefined) body.title = params.title;
  if (params.content !== undefined) body.content = params.content;
  return post<CreatePageResult>("/wiki/page/create", body);
}

/**
 * Move a page from `from` to `to` (e.g. dropping it into a different folder).
 * The broker rewrites any wikilinks pointing at the old path and reports how
 * many it rewrote so the caller can confirm the cascade to the user.
 */
export async function movePage(params: {
  from: string;
  to: string;
}): Promise<MovePageResult> {
  return post<MovePageResult>("/wiki/page/move", {
    from: params.from,
    to: params.to,
  });
}

/**
 * Rename the leaf of `path` to `newName` (no extension, no slashes — the broker
 * appends `.md`). Like move, this rewrites inbound wikilinks.
 */
export async function renamePage(params: {
  path: string;
  newName: string;
}): Promise<RenamePageResult> {
  return post<RenamePageResult>("/wiki/page/rename", {
    path: params.path,
    newName: params.newName,
  });
}

/**
 * Delete the page at `path`. State-changing and irreversible from the UI's
 * perspective — callers MUST confirm with the user before invoking this.
 */
export async function deletePage(path: string): Promise<DeletePageResult> {
  return del<DeletePageResult>(`/wiki/page?path=${encodeURIComponent(path)}`);
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
  /**
   * Whitespace-delimited word count of the article body (frontmatter included).
   * Drives the `verbose` badge in the catalog grid alongside `prune_score`.
   */
  word_count?: number;
  /**
   * Derived prune signal — `(word_count * days_unread) / readWeight`. Higher
   * means more verbose AND staler AND less read. Top-decile entries get a
   * `verbose` badge; PR 4 will surface a one-click compress action driven
   * by this score.
   */
  prune_score?: number;
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

function parseCatalogResponse(res: {
  articles: WikiCatalogEntry[];
}): WikiCatalogEntry[] {
  return Array.isArray(res?.articles) ? res.articles : [];
}

export async function fetchCatalogStrict(): Promise<WikiCatalogEntry[]> {
  const res = await get<{ articles: WikiCatalogEntry[] }>("/wiki/catalog");
  return parseCatalogResponse(res);
}

export async function fetchCatalog(): Promise<WikiCatalogEntry[]> {
  try {
    return await fetchCatalogStrict();
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

// ── Maintenance assistant API ────────────────────────────────────────────────

export type WikiMaintenanceAction =
  | "summarize"
  | "add_citation"
  | "extract_facts"
  | "resolve_contradiction"
  | "split_long_page"
  | "link_related"
  | "refresh_stale";

export interface WikiMaintenanceEvidence {
  kind: "wiki_article" | "fact" | "lint_finding" | "edit_log";
  label: string;
  path?: string;
  snippet?: string;
}

export interface WikiMaintenanceFactProposal {
  subject: string;
  predicate: string;
  object: string;
  confidence: number;
  source_line?: number;
}

export interface WikiMaintenanceDiff {
  proposed_content?: string;
  added?: string[];
  removed?: string[];
}

export interface WikiMaintenanceSuggestion {
  action: WikiMaintenanceAction;
  title: string;
  description?: string;
  diff?: WikiMaintenanceDiff;
  facts?: WikiMaintenanceFactProposal[];
  evidence?: WikiMaintenanceEvidence[];
  /** Present when action === "resolve_contradiction" — wires into the existing modal. */
  lint_finding?: LintFinding;
  lint_report_date?: string;
  lint_finding_idx?: number;
  expected_sha?: string;
  /** True when no suggestion was warranted; UI shows a friendly empty state. */
  skipped?: boolean;
  skipped_reason?: string;
}

/**
 * POST /wiki/maintenance/suggest — compute a suggestion for one (action, path)
 * pair. The broker never auto-writes; the suggestion is ephemeral and the
 * caller must apply it through the normal /wiki/write-human path on accept.
 */
export async function fetchMaintenanceSuggestion(
  action: WikiMaintenanceAction,
  path: string,
  options: { signal?: AbortSignal } = {},
): Promise<WikiMaintenanceSuggestion> {
  return await post<WikiMaintenanceSuggestion>(
    "/wiki/maintenance/suggest",
    { action, path },
    options,
  );
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
 * Result envelope for GET /wiki/diff?path=&sha=. `diff` is the raw unified
 * git diff for the article at the given commit `sha` (the change that commit
 * introduced). The version-history UI renders it line-by-line, colouring
 * added/removed lines. Unlike fetchHistory this does NOT swallow errors —
 * the caller surfaces a "couldn't load this version's diff" state so the
 * selection feedback is honest rather than silently blank.
 */
export interface WikiDiffResult {
  diff: string;
  sha: string;
  path: string;
}

export async function fetchWikiDiff(
  path: string,
  sha: string,
): Promise<WikiDiffResult> {
  return get<WikiDiffResult>("/wiki/diff", { path, sha });
}

/**
 * Result envelope for POST /wiki/restore. `commit_sha` is the NEW commit the
 * broker created to bring the article's content back to the restored version
 * (a forward commit, never a history rewrite). The caller passes it up via
 * onRestored so the article view refetches the now-current body.
 */
export interface RestoreVersionResult {
  path: string;
  commit_sha: string;
}

/**
 * Restore an article to a prior `sha`. State-changing and not reversible from
 * the UI without another restore — callers MUST confirm with the user before
 * invoking this (the broker writes a fresh commit that overwrites HEAD's body
 * with the historical version). Mirrors the page-mutation client pattern: a
 * thin POST with a typed envelope, errors propagated to the caller.
 */
export async function restoreWikiVersion(
  path: string,
  sha: string,
): Promise<RestoreVersionResult> {
  return post<RestoreVersionResult>("/wiki/restore", { path, sha });
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
