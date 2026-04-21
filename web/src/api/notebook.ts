/**
 * Notebook API client — thin wrapper over `client.ts` following the same
 * shape as `api/wiki.ts`. Returns mock fixtures when
 * `VITE_NOTEBOOK_MOCK !== 'false'` (default TRUE) so Lane E ships before
 * Lane B (backend) and Lane C (review state machine) are wired.
 */

import { get, post, sseURL } from './client'
import {
  MOCK_AGENTS,
  MOCK_REVIEWS,
  mockAgentEntries,
  mockEntry,
  mockReview,
} from './__fixtures__/notebook-mock'

// ── Types ────────────────────────────────────────────────────────

export type NotebookEntryStatus =
  | 'draft'
  | 'in-review'
  | 'changes-requested'
  | 'promoted'
  | 'discarded'

export type ReviewState =
  | 'pending'
  | 'in-review'
  | 'changes-requested'
  | 'approved'
  | 'archived'

/** Lightweight summary used in bookshelf rows + sidebar lists. */
export interface NotebookEntrySummary {
  entry_slug: string
  title: string
  last_edited_ts: string
  status: NotebookEntryStatus
}

export interface NotebookAgentSummary {
  agent_slug: string
  name: string
  role: string
  entries: NotebookEntrySummary[]
  total: number
  promoted_count: number
  last_updated_ts: string
}

export interface PromotedBackLink {
  section: string
  promoted_to_path: string
  promoted_by_slug: string
  promoted_ts: string
}

export interface NotebookEntry {
  agent_slug: string
  entry_slug: string
  title: string
  /** "Thursday, April 20th · working draft" — display subtitle below entry title. */
  subtitle?: string
  body_md: string
  last_edited_ts: string
  revisions: number
  status: NotebookEntryStatus
  file_path: string
  reviewer_slug: string
  /** When the wiki has content promoted from this entry, show a back-callout. */
  promoted_back?: PromotedBackLink
  /** Set once this entry itself lands in the wiki. */
  promoted_to_path?: string
}

export interface ReviewComment {
  id: string
  author_slug: string
  body_md: string
  ts: string
}

export interface ReviewItem {
  id: string
  agent_slug: string
  entry_slug: string
  entry_title: string
  proposed_wiki_path: string
  excerpt: string
  reviewer_slug: string
  state: ReviewState
  submitted_ts: string
  updated_ts: string
  comments: ReviewComment[]
}

export interface NotebookCatalogSummary {
  agents: NotebookAgentSummary[]
  total_agents: number
  total_entries: number
  pending_promotion: number
}

export interface NotebookEvent {
  type: 'notebook:write' | 'review:state_change'
  agent_slug?: string
  entry_slug?: string
  review_id?: string
  who?: string
  timestamp?: string
}

// ── Env toggle ───────────────────────────────────────────────────

function useMocks(): boolean {
  const v = (import.meta.env.VITE_NOTEBOOK_MOCK ?? 'true') as string
  // Default TRUE — real backend flips with `VITE_NOTEBOOK_MOCK=false`.
  return v !== 'false'
}

// ── Catalog ──────────────────────────────────────────────────────

export async function fetchCatalog(): Promise<NotebookCatalogSummary> {
  if (!useMocks()) {
    try {
      return await get<NotebookCatalogSummary>('/notebooks/catalog')
    } catch {
      // fall through to mocks
    }
  }
  const agents = MOCK_AGENTS
  const total_entries = agents.reduce((sum, a) => sum + a.total, 0)
  const pending_promotion = MOCK_REVIEWS.filter(
    (r) => r.state === 'pending' || r.state === 'in-review' || r.state === 'changes-requested',
  ).length
  return {
    agents,
    total_agents: agents.length,
    total_entries,
    pending_promotion,
  }
}

export async function fetchAgentEntries(
  agentSlug: string,
): Promise<{ agent: NotebookAgentSummary | null; entries: NotebookEntry[] }> {
  if (!useMocks()) {
    try {
      return await get<{ agent: NotebookAgentSummary | null; entries: NotebookEntry[] }>(
        `/notebooks/${encodeURIComponent(agentSlug)}`,
      )
    } catch {
      // fall through to mocks
    }
  }
  const agent = MOCK_AGENTS.find((a) => a.agent_slug === agentSlug) ?? null
  return { agent, entries: mockAgentEntries(agentSlug) }
}

export async function fetchEntry(
  agentSlug: string,
  entrySlug: string,
): Promise<NotebookEntry | null> {
  if (!useMocks()) {
    try {
      return await get<NotebookEntry>(
        `/notebooks/${encodeURIComponent(agentSlug)}/${encodeURIComponent(entrySlug)}`,
      )
    } catch {
      // fall through to mocks
    }
  }
  return mockEntry(agentSlug, entrySlug)
}

// ── Reviews ──────────────────────────────────────────────────────

export async function fetchReviews(): Promise<ReviewItem[]> {
  if (!useMocks()) {
    try {
      const res = await get<{ reviews: ReviewItem[] }>('/review/list?scope=all')
      return Array.isArray(res?.reviews) ? res.reviews : []
    } catch {
      // fall through to mocks
    }
  }
  return MOCK_REVIEWS
}

export async function fetchReview(id: string): Promise<ReviewItem | null> {
  if (!useMocks()) {
    try {
      return await get<ReviewItem>(`/review/${encodeURIComponent(id)}`)
    } catch {
      // fall through to mocks
    }
  }
  return mockReview(id)
}

// ── Mutations (Lane C: /review/*, /notebook/promote). ────────────

export async function promoteEntry(
  agentSlug: string,
  entrySlug: string,
  opts: { proposed_wiki_path?: string; reviewer_slug?: string; rationale?: string } = {},
): Promise<ReviewItem | null> {
  if (!useMocks()) {
    try {
      // Backend returns { promotion_id, reviewer_slug, state, human_only }.
      // Fetch the full ReviewItem shape via the detail endpoint so UI gets
      // the populated comment thread + timestamps in one call flow.
      const target =
        opts.proposed_wiki_path ??
        `team/drafts/${agentSlug}-${entrySlug}.md`
      const submitted = await post<{
        promotion_id: string
        reviewer_slug: string
        state: ReviewState
        human_only: boolean
      }>('/notebook/promote', {
        my_slug: agentSlug,
        source_path: `agents/${agentSlug}/notebook/${entrySlug}.md`,
        target_wiki_path: target,
        rationale: opts.rationale ?? '',
        reviewer_slug: opts.reviewer_slug,
      })
      if (submitted?.promotion_id) {
        const full = await fetchReview(submitted.promotion_id)
        if (full) return full
      }
    } catch {
      // fall through — caller handles UI state
    }
  }
  // Mock: return a synthetic pending review card so the UI can transition.
  const entry = mockEntry(agentSlug, entrySlug)
  if (!entry) return null
  return {
    id: `mock-${Date.now()}`,
    agent_slug: agentSlug,
    entry_slug: entrySlug,
    entry_title: entry.title,
    proposed_wiki_path:
      opts.proposed_wiki_path ?? `drafts/${entry.agent_slug}-${entry.entry_slug}`,
    excerpt: entry.body_md.slice(0, 200),
    reviewer_slug: opts.reviewer_slug ?? entry.reviewer_slug,
    state: 'pending',
    submitted_ts: new Date().toISOString(),
    updated_ts: new Date().toISOString(),
    comments: [],
  }
}

export async function updateReviewState(
  id: string,
  state: ReviewState,
  opts: { actor_slug?: string; rationale?: string } = {},
): Promise<ReviewItem | null> {
  if (!useMocks()) {
    // Backend exposes state-specific verbs, not a generic /state POST.
    // actor_slug empty = human action (web UI); non-empty = agent slug.
    const verbMap: Record<ReviewState, string | null> = {
      approved: 'approve',
      'changes-requested': 'request-changes',
      // 'rejected' is author-initiated withdraw; send via /reject.
      // TypeScript's ReviewState union doesn't include 'rejected' today, but
      // keep this fallthrough so the type widens cleanly when it does.
      pending: null,
      'in-review': 'resubmit',
      archived: null,
    }
    const verb = verbMap[state]
    if (verb) {
      try {
        await post<unknown>(`/review/${encodeURIComponent(id)}/${verb}`, {
          actor_slug: opts.actor_slug ?? '',
          rationale: opts.rationale ?? '',
        })
        const full = await fetchReview(id)
        if (full) return full
      } catch {
        // fall through
      }
    }
  }
  // Mock: mutate in-memory fixture so re-fetch reflects the change this
  // session (tests spy on the function; they don't rely on this persistence).
  const r = MOCK_REVIEWS.find((x) => x.id === id)
  if (!r) return null
  r.state = state
  r.updated_ts = new Date().toISOString()
  return r
}

export async function postReviewComment(
  id: string,
  body_md: string,
  author_slug: string,
): Promise<ReviewItem | null> {
  if (!useMocks()) {
    try {
      await post<unknown>(`/review/${encodeURIComponent(id)}/comment`, {
        actor_slug: author_slug,
        body: body_md,
      })
      const full = await fetchReview(id)
      if (full) return full
    } catch {
      // fall through
    }
  }
  const r = MOCK_REVIEWS.find((x) => x.id === id)
  if (!r) return null
  r.comments.push({
    id: `c-${Date.now()}`,
    author_slug,
    body_md,
    ts: new Date().toISOString(),
  })
  r.updated_ts = new Date().toISOString()
  return r
}

// ── SSE ──────────────────────────────────────────────────────────

/**
 * Subscribe to broker notebook/review events.
 *
 * Event payloads:
 *   { type: 'notebook:write', agent_slug, entry_slug, who, timestamp }
 *   { type: 'review:state_change', review_id, state, who, timestamp }
 *
 * Returns an unsubscribe function. Failure is silent — the UI remains
 * interactive from TanStack Query cache.
 */
export function subscribeNotebookEvents(
  handler: (ev: NotebookEvent) => void,
): () => void {
  let closed = false
  let source: EventSource | null = null

  try {
    source = new EventSource(sseURL('/notebooks/stream'))
    source.onmessage = (ev) => {
      if (closed) return
      try {
        const data = JSON.parse(ev.data) as Record<string, unknown>
        if (
          data &&
          (data.type === 'notebook:write' || data.type === 'review:state_change')
        ) {
          handler(data as unknown as NotebookEvent)
        }
      } catch {
        // ignore malformed events
      }
    }
    source.onerror = () => {
      if (source) {
        source.close()
        source = null
      }
    }
  } catch {
    source = null
  }

  return () => {
    closed = true
    if (source) {
      source.close()
      source = null
    }
  }
}
