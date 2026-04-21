/**
 * Playbook API client — v1.3 compounding-intelligence compiler surface.
 *
 * Thin wrapper over `client.ts`. Mirrors `entity.ts` shape because the
 * playbook surface rides on the same markdown/git substrate and uses the
 * same broker-level SSE stream.
 *
 * SSE: `/events` emits named events. We use `addEventListener` on the
 * one playbook event name so article views do not re-parse every broker
 * message.
 */

import { get, sseURL } from './client'

// ── Types ────────────────────────────────────────────────────────

export type PlaybookOutcome = 'success' | 'partial' | 'aborted'

export interface PlaybookSummary {
  slug: string
  title: string
  source_path: string
  skill_path: string
  skill_exists: boolean
  execution_log_path: string
  execution_count: number
  runnable_by_agents: string[]
}

export interface PlaybookExecution {
  id: string
  slug: string
  outcome: PlaybookOutcome
  summary: string
  notes?: string
  recorded_by: string
  created_at: string
}

export interface PlaybookExecutionRecordedEvent {
  slug: string
  path: string
  commit_sha: string
  recorded_by: string
  timestamp: string
}

// ── HTTP ─────────────────────────────────────────────────────────

/** `GET /playbook/list` — every source playbook + its compiled skill status. */
export async function fetchPlaybooks(): Promise<PlaybookSummary[]> {
  try {
    const res = await get<{ playbooks?: PlaybookSummary[] } | PlaybookSummary[]>(
      '/playbook/list',
    )
    if (Array.isArray(res)) return res
    return Array.isArray(res?.playbooks) ? res.playbooks : []
  } catch {
    return []
  }
}

/** `GET /playbook/executions?slug=` — newest-first execution log. */
export async function fetchPlaybookExecutions(
  slug: string,
): Promise<PlaybookExecution[]> {
  try {
    const res = await get<{ executions: PlaybookExecution[] }>(
      `/playbook/executions?slug=${encodeURIComponent(slug)}`,
    )
    return Array.isArray(res?.executions) ? res.executions : []
  } catch {
    return []
  }
}

// ── SSE ──────────────────────────────────────────────────────────

/**
 * Subscribe to `playbook:execution_recorded` events filtered to one slug.
 * Returns an unsubscribe function that tears down the underlying
 * EventSource. Follows the same shape as `subscribeEntityEvents` in
 * `entity.ts` — do not regress the `/events` + named-listener pattern
 * (see PR #182).
 */
export function subscribePlaybookEvents(
  slug: string,
  onExecutionRecorded: (ev: PlaybookExecutionRecordedEvent) => void,
): () => void {
  let closed = false
  let source: EventSource | null = null

  const handler = (ev: MessageEvent) => {
    if (closed) return
    try {
      const data = JSON.parse(ev.data) as PlaybookExecutionRecordedEvent
      if (data && data.slug === slug) {
        onExecutionRecorded(data)
      }
    } catch {
      // ignore malformed events
    }
  }

  try {
    const ES = (globalThis as { EventSource?: typeof EventSource }).EventSource
    if (!ES) return () => {}
    source = new ES(sseURL('/events'))
    source.addEventListener(
      'playbook:execution_recorded',
      handler as EventListener,
    )
    source.onerror = () => {
      // Keep the source open — EventSource auto-reconnects on transient
      // network blips. Closing here would drop live updates.
    }
  } catch {
    source = null
  }

  return () => {
    closed = true
    if (source) {
      source.removeEventListener(
        'playbook:execution_recorded',
        handler as EventListener,
      )
      source.close()
      source = null
    }
  }
}

/** Detect whether a wiki path is a source playbook article. Matches the
 *  backend regex `team/playbooks/{slug}.md` (with optional `team/` prefix
 *  and optional `.md` suffix, to match dev/mock shapes). */
const PLAYBOOK_PATH_RE = /^(?:team\/)?playbooks\/([a-z0-9][a-z0-9-]*)(?:\.md)?$/

export function detectPlaybook(path: string): { slug: string } | null {
  const m = path.match(PLAYBOOK_PATH_RE)
  if (!m) return null
  return { slug: m[1] }
}
