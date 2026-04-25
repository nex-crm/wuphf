/**
 * Notebook API client — thin wrapper over `client.ts` following the same
 * shape as `api/wiki.ts`. Uses the live broker by default. Mock fixtures are
 * opt-in with `VITE_NOTEBOOK_MOCK=true` for isolated UI work.
 */

import {
  MOCK_AGENTS,
  MOCK_REVIEWS,
  mockAgentEntries,
  mockEntry,
  mockReview,
} from "./__fixtures__/notebook-mock";
import * as client from "./client";

// ── Types ────────────────────────────────────────────────────────

export type NotebookEntryStatus =
  | "draft"
  | "in-review"
  | "changes-requested"
  | "promoted"
  | "discarded";

export type ReviewState =
  | "pending"
  | "in-review"
  | "changes-requested"
  | "approved"
  | "rejected"
  | "expired"
  | "archived";

/** Lightweight summary used in bookshelf rows + sidebar lists. */
export interface NotebookEntrySummary {
  entry_slug: string;
  title: string;
  last_edited_ts: string;
  status: NotebookEntryStatus;
}

export interface NotebookAgentSummary {
  agent_slug: string;
  name: string;
  role: string;
  entries: NotebookEntrySummary[];
  total: number;
  promoted_count: number;
  last_updated_ts: string;
}

export interface PromotedBackLink {
  section: string;
  promoted_to_path: string;
  promoted_by_slug: string;
  promoted_ts: string;
}

export interface NotebookEntry {
  agent_slug: string;
  entry_slug: string;
  title: string;
  /** "Thursday, April 20th · working draft" — display subtitle below entry title. */
  subtitle?: string;
  body_md: string;
  last_edited_ts: string;
  revisions: number;
  status: NotebookEntryStatus;
  file_path: string;
  reviewer_slug: string;
  /** When the wiki has content promoted from this entry, show a back-callout. */
  promoted_back?: PromotedBackLink;
  /** Set once this entry itself lands in the wiki. */
  promoted_to_path?: string;
}

export interface ReviewComment {
  id: string;
  author_slug: string;
  body_md: string;
  ts: string;
}

interface BackendReviewComment {
  id?: string;
  author_slug?: string;
  body?: string;
  body_md?: string;
  created_at?: string;
  ts?: string;
}

export interface ReviewItem {
  id: string;
  agent_slug: string;
  entry_slug: string;
  entry_title: string;
  proposed_wiki_path: string;
  excerpt: string;
  reviewer_slug: string;
  state: ReviewState;
  submitted_ts: string;
  updated_ts: string;
  comments: ReviewComment[];
}

interface BackendPromotion {
  id?: string;
  state?: string;
  source_slug?: string;
  source_path?: string;
  target_path?: string;
  rationale?: string;
  reviewer_slug?: string;
  created_at?: string;
  updated_at?: string;
  comments?: BackendReviewComment[];
}

export interface NotebookCatalogSummary {
  agents: NotebookAgentSummary[];
  total_agents: number;
  total_entries: number;
  pending_promotion: number;
}

export interface NotebookEvent {
  type: "notebook:write" | "review:state_change";
  agent_slug?: string;
  entry_slug?: string;
  review_id?: string;
  who?: string;
  timestamp?: string;
}

/**
 * One hit from `/notebook/search`. The broker reuses the wiki hit shape,
 * and we tag it with the agent slug the caller searched so the UI can
 * render `<agent> · <entry>` without re-parsing the path.
 */
export interface NotebookSearchHit {
  path: string;
  line: number;
  snippet: string;
  agent_slug: string;
}

/**
 * GET /notebook/search?slug=&q=... — literal substring search inside one
 * agent's notebook. Returns [] on any error so the SearchModal can fall
 * back to empty state without breaking.
 */
export async function searchNotebook(
  agentSlug: string,
  pattern: string,
): Promise<NotebookSearchHit[]> {
  const trimmed = pattern.trim();
  if (!(trimmed && agentSlug)) return [];
  try {
    const res = await client.get<{
      hits: Array<{ path: string; line: number; snippet: string }>;
    }>(
      `/notebook/search?slug=${encodeURIComponent(agentSlug)}&q=${encodeURIComponent(trimmed)}`,
    );
    const hits = Array.isArray(res?.hits) ? res.hits : [];
    return hits.map((h) => ({ ...h, agent_slug: agentSlug }));
  } catch {
    return [];
  }
}

// ── Env toggle ───────────────────────────────────────────────────

function shouldUseMocks(): boolean {
  const v = (import.meta.env.VITE_NOTEBOOK_MOCK ?? "false") as string;
  return v === "true";
}

function entrySlugFromPath(path: string): string {
  return path.replace(/^.*\//, "").replace(/\.md$/i, "");
}

function fallbackTitleFromSlug(slug: string): string {
  return slug.replace(/[-_]+/g, " ").replace(/\b\w/g, (m) => m.toUpperCase());
}

function titleFromMarkdown(markdown: string, fallbackSlug: string): string {
  for (const rawLine of markdown.split(/\r?\n/)) {
    const line = rawLine.trim();
    if (line.startsWith("# ")) return line.replace(/^#\s+/, "").trim();
  }
  return fallbackTitleFromSlug(fallbackSlug);
}

function excerptFromMarkdown(markdown: string, fallback = ""): string {
  const lines = markdown
    .replace(/^---\s*[\s\S]*?\n---\s*/m, "")
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter((line) => line && !line.startsWith("#"));
  const text = (lines.join(" ") || fallback).replace(/\s+/g, " ").trim();
  return text.length > 220 ? `${text.slice(0, 217)}...` : text;
}

function normalizeReviewState(state: unknown): ReviewState {
  switch (state) {
    case "pending":
    case "in-review":
    case "changes-requested":
    case "approved":
    case "rejected":
    case "expired":
    case "archived":
      return state;
    default:
      return "archived";
  }
}

function statusFromReviewState(state: ReviewState): NotebookEntryStatus {
  switch (state) {
    case "pending":
    case "in-review":
      return "in-review";
    case "changes-requested":
      return "changes-requested";
    case "approved":
    case "archived":
      return "promoted";
    case "rejected":
    case "expired":
      return "discarded";
    default:
      return "draft";
  }
}

function normalizeReviewItem(raw: ReviewItem | BackendPromotion): ReviewItem {
  const record = raw as ReviewItem &
    BackendPromotion & { source_body_md?: string };
  const sourcePath = record.source_path ?? "";
  const entrySlug = record.entry_slug || entrySlugFromPath(sourcePath);
  const body = record.source_body_md ?? "";
  const state = normalizeReviewState(record.state);
  const submitted = record.submitted_ts ?? record.created_at ?? "";
  const updated = record.updated_ts ?? record.updated_at ?? submitted;
  return {
    id: record.id ?? "",
    agent_slug: record.agent_slug ?? record.source_slug ?? "",
    entry_slug: entrySlug,
    entry_title:
      record.entry_title ?? titleFromMarkdown(body, entrySlug || "review"),
    proposed_wiki_path: record.proposed_wiki_path ?? record.target_path ?? "",
    excerpt:
      record.excerpt ?? excerptFromMarkdown(body, record.rationale ?? ""),
    reviewer_slug: record.reviewer_slug ?? "",
    state,
    submitted_ts: submitted,
    updated_ts: updated,
    comments: (
      (record.comments ?? []) as Array<ReviewComment | BackendReviewComment>
    ).map((c) => {
      const comment = c as ReviewComment & BackendReviewComment;
      return {
        id: comment.id ?? "",
        author_slug: comment.author_slug ?? "",
        body_md: comment.body_md ?? comment.body ?? "",
        ts: comment.ts ?? comment.created_at ?? updated,
      };
    }),
  };
}

// ── Catalog ──────────────────────────────────────────────────────

export async function fetchCatalog(): Promise<NotebookCatalogSummary> {
  if (!shouldUseMocks()) {
    // Real backend: propagate errors so the empty-state / error-state UI
    // surfaces real problems instead of masking them with mock fixtures.
    // Swapping to mocks silently was hiding a missing /notebook/catalog
    // endpoint for weeks in internal demos.
    return await client.get<NotebookCatalogSummary>("/notebook/catalog");
  }
  const agents = MOCK_AGENTS;
  const total_entries = agents.reduce((sum, a) => sum + a.total, 0);
  const pending_promotion = MOCK_REVIEWS.filter(
    (r) =>
      r.state === "pending" ||
      r.state === "in-review" ||
      r.state === "changes-requested",
  ).length;
  return {
    agents,
    total_agents: agents.length,
    total_entries,
    pending_promotion,
  };
}

export async function fetchAgentEntries(
  agentSlug: string,
): Promise<{ agent: NotebookAgentSummary | null; entries: NotebookEntry[] }> {
  if (!shouldUseMocks()) {
    // Backend exposes list-by-slug; synthesize the agent header client-side
    // from the catalog so one route missing doesn't blank the page.
    const raw = await client.get<{
      entries: Array<{
        path: string;
        title: string;
        modified: string;
        size_bytes: number;
      }>;
    }>(`/notebook/list?slug=${encodeURIComponent(agentSlug)}`);
    const catalog = await client
      .get<NotebookCatalogSummary>("/notebook/catalog")
      .catch(() => null);
    const agent =
      catalog?.agents.find((a) => a.agent_slug === agentSlug) ?? null;
    const reviews = await fetchReviews().catch(() => [] as ReviewItem[]);
    const entries: NotebookEntry[] = await Promise.all(
      (raw.entries ?? []).map(async (e) => {
        const entry_slug = entrySlugFromPath(e.path);
        const review = reviews.find(
          (r) => r.agent_slug === agentSlug && r.entry_slug === entry_slug,
        );
        const catalogEntry = agent?.entries.find(
          (candidate) => candidate.entry_slug === entry_slug,
        );
        const body_md = await client
          .getText("/notebook/read", { slug: agentSlug, path: e.path })
          .catch(() => "");
        return {
          agent_slug: agentSlug,
          entry_slug,
          title: e.title || titleFromMarkdown(body_md, entry_slug),
          last_edited_ts: e.modified,
          revisions: 1,
          body_md,
          status: review
            ? statusFromReviewState(review.state)
            : (catalogEntry?.status ?? "draft"),
          file_path: e.path,
          reviewer_slug: review?.reviewer_slug ?? "",
        };
      }),
    );
    return { agent, entries };
  }
  const agent = MOCK_AGENTS.find((a) => a.agent_slug === agentSlug) ?? null;
  return { agent, entries: mockAgentEntries(agentSlug) };
}

export async function fetchEntry(
  agentSlug: string,
  entrySlug: string,
): Promise<NotebookEntry | null> {
  if (!shouldUseMocks()) {
    // Backend doesn't expose a single-entry endpoint yet, but the list +
    // read pair is enough for now: list gives the metadata, read returns
    // body bytes. Throw on genuine errors instead of falling through to
    // mocks (same silent-fallback fix as fetchCatalog).
    const path = `agents/${agentSlug}/notebook/${entrySlug}.md`;
    const body = await client.getText("/notebook/read", {
      slug: agentSlug,
      path,
    });
    // Fetch list so we can fill title + last_edited_ts for the header.
    const list = await client
      .get<{
        entries: Array<{ path: string; title: string; modified: string }>;
      }>(`/notebook/list?slug=${encodeURIComponent(agentSlug)}`)
      .catch(() => ({
        entries: [] as Array<{ path: string; title: string; modified: string }>,
      }));
    const reviews = await fetchReviews().catch(() => [] as ReviewItem[]);
    const review = reviews.find(
      (r) => r.agent_slug === agentSlug && r.entry_slug === entrySlug,
    );
    const meta = list.entries.find((e) => e.path === path);
    return {
      agent_slug: agentSlug,
      entry_slug: entrySlug,
      title: meta?.title ?? titleFromMarkdown(body, entrySlug),
      last_edited_ts: meta?.modified ?? new Date().toISOString(),
      revisions: 1,
      body_md: body,
      status: review ? statusFromReviewState(review.state) : "draft",
      file_path: path,
      reviewer_slug: review?.reviewer_slug ?? "",
    };
  }
  return mockEntry(agentSlug, entrySlug);
}

// ── Reviews ──────────────────────────────────────────────────────

export async function fetchReviews(): Promise<ReviewItem[]> {
  if (!shouldUseMocks()) {
    // Propagate errors — silent fallback to mocks was masking a real-backend
    // bug where /review/list was 503 because the ReviewLog never initialized.
    const res = await client.get<{
      reviews: Array<ReviewItem | BackendPromotion>;
    }>("/review/list?scope=all");
    return Array.isArray(res?.reviews)
      ? res.reviews.map(normalizeReviewItem)
      : [];
  }
  return MOCK_REVIEWS;
}

export async function fetchReview(id: string): Promise<ReviewItem | null> {
  if (!shouldUseMocks()) {
    const raw = await client.get<ReviewItem | BackendPromotion>(
      `/review/${encodeURIComponent(id)}`,
    );
    return normalizeReviewItem(raw);
  }
  return mockReview(id);
}

// ── Mutations (/review/*, /notebook/promote). ────────────────────

export async function promoteEntry(
  agentSlug: string,
  entrySlug: string,
  opts: {
    proposed_wiki_path?: string;
    reviewer_slug?: string;
    rationale?: string;
  } = {},
): Promise<ReviewItem | null> {
  if (!shouldUseMocks()) {
    // Backend returns { promotion_id, reviewer_slug, state, human_only }.
    // Fetch the full ReviewItem shape via the detail endpoint so UI gets
    // the populated comment thread + timestamps in one call flow.
    const target =
      opts.proposed_wiki_path ?? `team/drafts/${agentSlug}-${entrySlug}.md`;
    const submitted = await client.post<{
      promotion_id: string;
      reviewer_slug: string;
      state: ReviewState;
      human_only: boolean;
    }>("/notebook/promote", {
      my_slug: agentSlug,
      source_path: `agents/${agentSlug}/notebook/${entrySlug}.md`,
      target_wiki_path: target,
      rationale: opts.rationale ?? "Ready for team wiki review.",
      reviewer_slug: opts.reviewer_slug,
    });
    if (submitted?.promotion_id) {
      const full = await fetchReview(submitted.promotion_id).catch(() => null);
      if (full) return full;
      return normalizeReviewItem({
        id: submitted.promotion_id,
        source_slug: agentSlug,
        source_path: `agents/${agentSlug}/notebook/${entrySlug}.md`,
        target_path: target,
        reviewer_slug: submitted.reviewer_slug,
        state: submitted.state,
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
        comments: [],
      });
    }
    return null;
  }
  // Mock: return a synthetic pending review card so the UI can transition.
  const entry = mockEntry(agentSlug, entrySlug);
  if (!entry) return null;
  return {
    id: `mock-${Date.now()}`,
    agent_slug: agentSlug,
    entry_slug: entrySlug,
    entry_title: entry.title,
    proposed_wiki_path:
      opts.proposed_wiki_path ??
      `team/drafts/${entry.agent_slug}-${entry.entry_slug}.md`,
    excerpt: entry.body_md.slice(0, 200),
    reviewer_slug: opts.reviewer_slug ?? entry.reviewer_slug,
    state: "pending",
    submitted_ts: new Date().toISOString(),
    updated_ts: new Date().toISOString(),
    comments: [],
  };
}

export async function updateReviewState(
  id: string,
  state: ReviewState,
  opts: { actor_slug?: string; rationale?: string } = {},
): Promise<ReviewItem | null> {
  if (!shouldUseMocks()) {
    // Backend exposes state-specific verbs, not a generic /state POST.
    // actor_slug empty = human action (web UI); non-empty = agent slug.
    const verbMap: Record<ReviewState, string | null> = {
      approved: "approve",
      "changes-requested": "request-changes",
      rejected: "reject",
      pending: null,
      "in-review": "resubmit",
      expired: null,
      archived: null,
    };
    const verb = verbMap[state];
    if (verb) {
      await client.post<unknown>(`/review/${encodeURIComponent(id)}/${verb}`, {
        actor_slug: opts.actor_slug ?? "",
        rationale: opts.rationale ?? "",
      });
      return await fetchReview(id);
    }
    return await fetchReview(id);
  }
  // Mock: mutate in-memory fixture so re-fetch reflects the change this
  // session (tests spy on the function; they don't rely on this persistence).
  const r = MOCK_REVIEWS.find((x) => x.id === id);
  if (!r) return null;
  r.state = state;
  r.updated_ts = new Date().toISOString();
  return r;
}

export async function postReviewComment(
  id: string,
  body_md: string,
  author_slug: string,
): Promise<ReviewItem | null> {
  if (!shouldUseMocks()) {
    await client.post<unknown>(`/review/${encodeURIComponent(id)}/comment`, {
      actor_slug: author_slug,
      body: body_md,
    });
    return await fetchReview(id);
  }
  const r = MOCK_REVIEWS.find((x) => x.id === id);
  if (!r) return null;
  r.comments.push({
    id: `c-${Date.now()}`,
    author_slug,
    body_md,
    ts: new Date().toISOString(),
  });
  r.updated_ts = new Date().toISOString();
  return r;
}

// ── SSE ──────────────────────────────────────────────────────────

/**
 * Subscribe to broker notebook + review events on the shared `/events`
 * SSE stream. Returns an unsubscribe function that tears down the
 * underlying EventSource.
 *
 * Previously this subscribed to `/notebooks/stream` — a path that never
 * existed on the broker. Live notebook and review updates were dead in
 * production. Matches `api/entity.ts`: broker emits named events
 * (`event: notebook:write\ndata: ...`, `event: review:state_change\ndata: ...`)
 * so we use `addEventListener` not `onmessage`.
 *
 * Event payloads (as emitted by broker.handleEvents):
 *   notebook:write       — `{ slug, path, commit_sha, ts, ... }`
 *   review:state_change  — `{ id, old_state, new_state, actor_slug, timestamp }`
 *
 * Handler normalizes both into the NotebookEvent discriminated union so
 * downstream components can switch on `type`.
 */
export function subscribeNotebookEvents(
  handler: (ev: NotebookEvent) => void,
): () => void {
  let closed = false;
  let source: EventSource | null = null;
  const listeners: Array<[string, EventListener]> = [];

  try {
    const ES = (globalThis as { EventSource?: typeof EventSource }).EventSource;
    if (!ES)
      return () => {
        closed = true;
      };
    source = new ES(client.sseURL("/events"));

    const onNotebook = (ev: MessageEvent) => {
      if (closed) return;
      try {
        const data = JSON.parse(ev.data) as Record<string, unknown>;
        handler({
          type: "notebook:write",
          ...data,
        } as unknown as NotebookEvent);
      } catch {
        // ignore malformed events
      }
    };
    const onReview = (ev: MessageEvent) => {
      if (closed) return;
      try {
        const data = JSON.parse(ev.data) as Record<string, unknown>;
        handler({
          type: "review:state_change",
          ...data,
        } as unknown as NotebookEvent);
      } catch {
        // ignore malformed events
      }
    };
    source.addEventListener("notebook:write", onNotebook as EventListener);
    source.addEventListener("review:state_change", onReview as EventListener);
    listeners.push(["notebook:write", onNotebook as EventListener]);
    listeners.push(["review:state_change", onReview as EventListener]);
  } catch {
    source = null;
  }

  return () => {
    closed = true;
    if (source) {
      for (const [name, fn] of listeners) source.removeEventListener(name, fn);
      source.close();
      source = null;
    }
  };
}
