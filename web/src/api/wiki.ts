/**
 * Wiki API client — thin wrapper over the shared fetch helper in `client.ts`.
 * Falls back to local mock fixtures when Lane A's endpoints are not yet wired,
 * so the UI renders during development.
 */

import { get, sseURL } from './client'

export interface WikiArticle {
  path: string
  title: string
  content: string
  last_edited_by: string
  last_edited_ts: string
  revisions: number
  contributors: string[]
  backlinks: { path: string; title: string; author_slug: string }[]
  word_count: number
  categories: string[]
}

export interface WikiCatalogEntry {
  path: string
  title: string
  author_slug: string
  last_edited_ts: string
  group: string
}

export interface WikiHistoryCommit {
  sha: string
  author_slug: string
  msg: string
  date: string
}

export interface WikiEditLogEntry {
  who: string
  action: 'edited' | 'created' | 'updated' | 'wrote'
  article_path: string
  article_title: string
  timestamp: string
  commit_sha: string
}

export async function fetchArticle(path: string): Promise<WikiArticle> {
  try {
    return await get<WikiArticle>(`/wiki/article/${encodeURI(path)}`)
  } catch {
    return mockArticle(path)
  }
}

export async function fetchCatalog(): Promise<WikiCatalogEntry[]> {
  try {
    const res = await get<{ articles: WikiCatalogEntry[] }>('/wiki/list')
    return Array.isArray(res?.articles) ? res.articles : []
  } catch {
    return MOCK_CATALOG
  }
}

export async function fetchHistory(
  path: string,
): Promise<{ commits: WikiHistoryCommit[] }> {
  try {
    return await get<{ commits: WikiHistoryCommit[] }>(`/wiki/history/${encodeURI(path)}`)
  } catch {
    return { commits: mockArticle(path).contributors.map((slug, i) => ({
      sha: `mock${i}`,
      author_slug: slug,
      msg: `Edit ${i + 1} by ${slug}`,
      date: new Date(Date.now() - i * 86400000).toISOString(),
    })) }
  }
}

/**
 * Subscribe to broker SSE stream filtered for `wiki:write` events.
 * Returns an unsubscribe function. Falls back to a synthetic replay of
 * the mock edit log if the stream is unavailable.
 */
export function subscribeEditLog(
  handler: (entry: WikiEditLogEntry) => void,
): () => void {
  let closed = false
  let source: EventSource | null = null

  try {
    source = new EventSource(sseURL('/wiki/stream'))
    source.onmessage = (ev) => {
      if (closed) return
      try {
        const data = JSON.parse(ev.data) as Record<string, unknown>
        if (data && data.type === 'wiki:write') {
          handler(data.entry as WikiEditLogEntry)
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

// ── Mock fixtures — pulled from V3 preview content. ──

export const MOCK_CATALOG: WikiCatalogEntry[] = [
  { path: 'people/customer-x', title: 'Customer X', author_slug: 'ceo', last_edited_ts: new Date(Date.now() - 3 * 60 * 1000).toISOString(), group: 'people' },
  { path: 'people/nazz', title: 'Nazz (founder)', author_slug: 'pm', last_edited_ts: new Date(Date.now() - 2 * 3600 * 1000).toISOString(), group: 'people' },
  { path: 'people/sarah-chen', title: 'Sarah Chen', author_slug: 'ceo', last_edited_ts: new Date(Date.now() - 12 * 3600 * 1000).toISOString(), group: 'people' },
  { path: 'people/david-kim', title: 'David Kim', author_slug: 'cmo', last_edited_ts: new Date(Date.now() - 18 * 3600 * 1000).toISOString(), group: 'people' },
  { path: 'companies/acme-logistics', title: 'Acme Logistics', author_slug: 'cro', last_edited_ts: new Date(Date.now() - 26 * 3600 * 1000).toISOString(), group: 'companies' },
  { path: 'companies/meridian-freight', title: 'Meridian Freight', author_slug: 'cro', last_edited_ts: new Date(Date.now() - 48 * 3600 * 1000).toISOString(), group: 'companies' },
  { path: 'projects/customer-x-onboarding', title: 'Customer X — Onboarding', author_slug: 'pm', last_edited_ts: new Date(Date.now() - 4 * 3600 * 1000).toISOString(), group: 'projects' },
  { path: 'projects/q1-pilot-retrospective', title: 'Q1 Pilot Retrospective', author_slug: 'pm', last_edited_ts: new Date(Date.now() - 6 * 86400 * 1000).toISOString(), group: 'projects' },
  { path: 'playbooks/churn-prevention', title: 'Churn prevention', author_slug: 'cmo', last_edited_ts: new Date(Date.now() - 2 * 86400 * 1000).toISOString(), group: 'playbooks' },
  { path: 'playbooks/mid-market-onboarding', title: 'Mid-market onboarding', author_slug: 'pm', last_edited_ts: new Date(Date.now() - 9 * 86400 * 1000).toISOString(), group: 'playbooks' },
  { path: 'playbooks/pricing-negotiations', title: 'Pricing negotiations', author_slug: 'cro', last_edited_ts: new Date(Date.now() - 14 * 86400 * 1000).toISOString(), group: 'playbooks' },
  { path: 'decisions/2026-q1-pricing', title: '2026-Q1 pricing', author_slug: 'ceo', last_edited_ts: new Date(Date.now() - 31 * 86400 * 1000).toISOString(), group: 'decisions' },
  { path: 'decisions/migration-v1-1', title: 'Migration to v1.1', author_slug: 'be', last_edited_ts: new Date(Date.now() - 22 * 86400 * 1000).toISOString(), group: 'decisions' },
  { path: 'inbox/raw-customer-x-transcript', title: 'raw — Customer X call transcript', author_slug: 'pm', last_edited_ts: new Date(Date.now() - 6 * 3600 * 1000).toISOString(), group: 'inbox' },
]

export function mockArticle(path: string): WikiArticle {
  if (path === 'people/customer-x' || path === '' || path === 'customer-x') {
    return {
      path: 'people/customer-x',
      title: 'Customer X',
      content: MOCK_CUSTOMER_X_MD,
      last_edited_by: 'ceo',
      last_edited_ts: new Date(Date.now() - 3 * 60 * 1000).toISOString(),
      revisions: 47,
      contributors: ['ceo', 'pm', 'cro', 'cmo', 'designer', 'be'],
      backlinks: [
        { path: 'playbooks/churn-prevention', title: 'Playbook — Churn prevention', author_slug: 'cmo' },
        { path: 'projects/q1-pilot-retrospective', title: 'Q1 Pilot Retrospective', author_slug: 'pm' },
        { path: 'playbooks/pricing-negotiations', title: 'Pricing negotiations', author_slug: 'cro' },
        { path: 'people/sarah-chen', title: 'Sarah Chen', author_slug: 'ceo' },
      ],
      word_count: 2347,
      categories: ['Active pilot', 'Mid-market', 'Logistics', 'Q1 2026', 'North America', 'Sarah Chen'],
    }
  }
  // Generic fallback for unknown paths.
  const title = path.split('/').pop() || path
  return {
    path,
    title: title.charAt(0).toUpperCase() + title.slice(1).replace(/-/g, ' '),
    content: `*Article not found in mock fixtures.*\n\nThe API endpoint for \`${path}\` is not yet wired. When Lane A completes its endpoints this view will populate with real content.\n`,
    last_edited_by: 'pm',
    last_edited_ts: new Date().toISOString(),
    revisions: 1,
    contributors: ['pm'],
    backlinks: [],
    word_count: 42,
    categories: [],
  }
}

const MOCK_CUSTOMER_X_MD = `**Customer X** is a mid-market logistics company running a 47-person operations team out of Cincinnati. They came to us through our [[projects/q1-outbound|Q1 outbound pipeline]] after [[people/sarah-chen|Sarah Chen]] (Director of Ops) saw a demo at the March logistics summit. Signed the pilot three weeks later.

Sarah is the champion. Her boss is Mike Reyes, VP Operations, who has seen the product twice but doesn't engage directly. The procurement process went through their legal team ([[templates/msa|MSA template we haven't written yet]]) — contract took nine days, which is fast for this segment.

## What they want

Their primary pain is route optimization at the dispatcher level: six dispatchers, each manually rebuilding route schedules every morning in spreadsheets. They've looked at three competitors; the deal-breaker with each was onboarding friction, not feature gaps. That confirms the [[playbooks/onboarding-wedge|onboarding-wedge thesis]] we developed in Q4.

### Stated goals

- Cut dispatcher morning prep from 2 hours to 20 minutes
- Reduce route reshuffle thrash (currently 4-7 reshuffles/day per dispatcher)
- Surface exception patterns to Sarah weekly (their "ops review" meeting)

### Unstated goals (inferred)

Sarah wants this to be a visible win for her team before her Q3 performance review. See [[playbooks/churn-prevention|Churn prevention]].

## Open issues

Two things are currently blocking expansion past the pilot seat count:

- **Training data boundary.** Their ops data is classified as internal-sensitive; they need a signed addendum specifying no cross-tenant training. Legal has a template we're close to finalizing, see [[templates/data-handling|data handling addendum]].
- **Pricing model for dispatcher seats vs viewer seats.** Sarah asked about read-only seats at 30% of dispatcher price. Depends on [[decisions/q2-pricing|Q2 pricing review]].

## Next steps

Sarah's next check-in is on \`2026-05-02\`. CEO will send a renewal-prep email two weeks before. If the addendum lands by mid-April we can expand seats at the same meeting. If not, we renew as-is and expand at Q3.
`

export const MOCK_EDIT_LOG: WikiEditLogEntry[] = [
  { who: 'CEO', action: 'edited', article_path: 'people/customer-x', article_title: 'Customer X', timestamp: new Date().toISOString(), commit_sha: '9a0f113' },
  { who: 'PM', action: 'updated', article_path: 'playbooks/churn-prevention', article_title: 'Playbook — Churn', timestamp: new Date(Date.now() - 2 * 60 * 1000).toISOString(), commit_sha: 'b1d5e22' },
  { who: 'Designer', action: 'created', article_path: 'brand/voice', article_title: 'Brand Voice', timestamp: new Date(Date.now() - 5 * 60 * 1000).toISOString(), commit_sha: '3f9a21b' },
  { who: 'Eng-1', action: 'wrote', article_path: 'tech/broker-architecture', article_title: 'Tech — broker architecture', timestamp: new Date(Date.now() - 12 * 60 * 1000).toISOString(), commit_sha: '7c2e881' },
]
