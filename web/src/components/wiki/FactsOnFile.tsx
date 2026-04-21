import { useCallback, useEffect, useState } from 'react'
import PixelAvatar from './PixelAvatar'
import {
  fetchFacts,
  subscribeEntityEvents,
  type EntityKind,
  type Fact,
} from '../../api/entity'
import { formatAgentName } from '../../lib/agentName'

interface FactsOnFileProps {
  kind: EntityKind
  slug: string
}

const INITIAL_LIMIT = 50

export default function FactsOnFile({ kind, slug }: FactsOnFileProps) {
  const [facts, setFacts] = useState<Fact[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [showAll, setShowAll] = useState(false)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError(null)
    fetchFacts(kind, slug)
      .then((rows) => {
        if (cancelled) return
        setFacts(rows)
      })
      .catch((err: unknown) => {
        if (cancelled) return
        setError(err instanceof Error ? err.message : 'Failed to load facts')
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [kind, slug])

  const handleFact = useCallback((ev: { fact_id: string; recorded_by: string; timestamp: string }) => {
    setFacts((prev) => {
      // Skip if we already have this id (shouldn't happen, but the SSE
      // stream can replay on reconnect in theory).
      if (prev.some((f) => f.id === ev.fact_id)) return prev
      // Prepend an optimistic row. Refetch in parallel so the real row
      // (with text + source_path) replaces this shortly.
      const optimistic: Fact = {
        id: ev.fact_id,
        kind,
        slug,
        text: '…',
        recorded_by: ev.recorded_by,
        created_at: ev.timestamp,
      }
      return [optimistic, ...prev]
    })
    // Refetch to resolve the optimistic row with full fact text. Fire and
    // forget — errors here are visible in the next render cycle.
    void fetchFacts(kind, slug)
      .then((rows) => setFacts(rows))
      .catch(() => {
        // Keep the optimistic row; surfacing a second error on top of the
        // initial fetch would flood the UI.
      })
  }, [kind, slug])

  useEffect(() => {
    const unsubscribe = subscribeEntityEvents(
      kind,
      slug,
      handleFact,
      () => {
        // Brief synthesis doesn't change the facts list itself, but refetch
        // anyway in case the synthesis raced with a batch of new facts.
        void fetchFacts(kind, slug).then(setFacts).catch(() => {})
      },
    )
    return unsubscribe
  }, [kind, slug, handleFact])

  const visibleFacts = showAll ? facts : facts.slice(0, INITIAL_LIMIT)

  return (
    <section
      className="wk-facts-list"
      aria-labelledby="wk-facts-heading"
      data-testid="wk-facts-on-file"
    >
      <h2 id="wk-facts-heading">Facts on file</h2>
      {loading ? (
        <p className="wk-facts-loading">loading facts…</p>
      ) : error ? (
        <p className="wk-facts-error">{error}</p>
      ) : facts.length === 0 ? (
        <p className="wk-facts-empty">
          0 facts recorded yet. Agents will add facts as they work.
        </p>
      ) : (
        <>
          <ol className="wk-facts-items">
            {visibleFacts.map((f) => (
              <li key={f.id} className="wk-facts-item">
                <PixelAvatar slug={f.recorded_by} size={14} />
                <div className="wk-facts-body">
                  <span className="wk-facts-text">{f.text}</span>
                  <span className="wk-facts-meta">
                    {formatAgentName(f.recorded_by)}
                    {' · '}
                    <time dateTime={f.created_at}>{formatShortTs(f.created_at)}</time>
                    {isWikiSource(f.source_path) && (
                      <>
                        {' · '}
                        <a
                          className="wk-facts-source"
                          href={`#/wiki/${f.source_path}`}
                          data-wikilink="true"
                        >
                          {sourceLabel(f.source_path as string)}
                        </a>
                      </>
                    )}
                  </span>
                </div>
              </li>
            ))}
          </ol>
          {facts.length > INITIAL_LIMIT && (
            <button
              type="button"
              className="wk-facts-showall"
              onClick={() => setShowAll((v) => !v)}
            >
              {showAll
                ? 'show recent only'
                : `show all (${facts.length - INITIAL_LIMIT} more)`}
            </button>
          )}
        </>
      )}
    </section>
  )
}

function isWikiSource(path?: string): path is string {
  if (!path) return false
  return path.startsWith('agents/') || path.startsWith('team/')
}

function sourceLabel(path: string): string {
  const base = path.replace(/\.md$/, '')
  const tail = base.split('/').slice(-2).join('/')
  return tail || base
}

function formatShortTs(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toISOString().slice(0, 10)
}
