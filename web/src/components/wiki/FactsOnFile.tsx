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
              <li
                key={f.id}
                className="wk-facts-item"
                data-fact-type={f.type ?? 'observation'}
                data-superseded={isSuperseded(f) ? 'true' : undefined}
              >
                <PixelAvatar slug={f.recorded_by} size={14} />
                <div className="wk-facts-body">
                  <span className="wk-facts-text">{f.text}</span>
                  {f.triplet && (
                    <span className="wk-facts-triplet" aria-label="Typed triplet">
                      <code>{f.triplet.subject}</code>
                      {' — '}
                      <code>{f.triplet.predicate}</code>
                      {' → '}
                      <code>{f.triplet.object}</code>
                    </span>
                  )}
                  <span className="wk-facts-meta">
                    {f.type && <span className="wk-facts-type">{f.type}</span>}
                    {typeof f.confidence === 'number' && (
                      <>
                        {f.type && ' · '}
                        <span
                          className="wk-facts-confidence"
                          aria-label={`Confidence ${(f.confidence * 100).toFixed(0)} percent`}
                        >
                          {f.confidence.toFixed(2)}
                        </span>
                      </>
                    )}
                    {(f.type || typeof f.confidence === 'number') && ' · '}
                    {formatAgentName(f.recorded_by)}
                    {' · '}
                    <time dateTime={f.created_at}>{formatShortTs(f.created_at)}</time>
                    {formatValidity(f) && (
                      <>
                        {' · '}
                        <span className="wk-facts-validity">{formatValidity(f)}</span>
                      </>
                    )}
                    {f.reinforced_at && (
                      <>
                        {' · '}
                        <span
                          className="wk-facts-reinforced"
                          aria-label={`Reinforced ${formatShortTs(f.reinforced_at)}`}
                        >
                          reinforced {formatShortTs(f.reinforced_at)}
                        </span>
                      </>
                    )}
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
                    {f.supersedes && f.supersedes.length > 0 && (
                      <>
                        {' · '}
                        <span
                          className="wk-facts-supersedes"
                          aria-label={`Supersedes ${f.supersedes.length} prior fact${f.supersedes.length === 1 ? '' : 's'}`}
                        >
                          supersedes {f.supersedes.length} prior
                        </span>
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

/** Checks whether a source_path resolves to a wiki-renderable location.
 *  Schema §3 three-layer architecture: wiki/artifacts/ (Layer 1, raw),
 *  team/ (Layer 2, briefs), wiki/facts/ (Layer 2, fact log),
 *  wiki/insights/ (Layer 2, insights), wiki/playbooks/ (Layer 2, playbooks).
 *  agents/ is the legacy v1.2 per-agent notebook path and is retained for
 *  backwards compatibility with existing fact rows. */
function isWikiSource(path?: string): path is string {
  if (!path) return false
  return (
    path.startsWith('wiki/artifacts/') ||
    path.startsWith('team/') ||
    path.startsWith('wiki/facts/') ||
    path.startsWith('wiki/insights/') ||
    path.startsWith('wiki/playbooks/') ||
    path.startsWith('agents/') // legacy v1.2 per-agent notebook path
  )
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

/** A fact is superseded when its temporal validity has ended.
 *  Schema §8.2 — valid_until being set means a newer fact has taken its place.
 *  A fact that HAS a supersedes list is the NEWER fact (it replaced others);
 *  the supersedes list alone does NOT make this fact superseded. */
function isSuperseded(f: Fact): boolean {
  return Boolean(f.valid_until)
}

function formatValidity(f: Fact): string | null {
  if (!f.valid_from && !f.valid_until) return null
  const from = f.valid_from ? formatShortTs(f.valid_from) : null
  const until = f.valid_until ? formatShortTs(f.valid_until) : null
  if (from && until) return `valid ${from} → ${until}`
  if (until) return `valid until ${until}`
  if (from) return `valid from ${from}`
  return null
}
