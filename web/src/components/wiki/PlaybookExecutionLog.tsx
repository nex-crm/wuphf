import { useCallback, useEffect, useState } from 'react'
import {
  fetchPlaybookExecutions,
  subscribePlaybookEvents,
  type PlaybookExecution,
} from '../../api/playbook'
import { formatAgentName } from '../../lib/agentName'

interface PlaybookExecutionLogProps {
  slug: string
}

const INITIAL_LIMIT = 10

/**
 * Collapsible execution-log panel rendered on playbook article pages.
 * Newest-first, capped at INITIAL_LIMIT by default — the full log is
 * available in `team/playbooks/{slug}.executions.jsonl` for auditing.
 */
export default function PlaybookExecutionLog({ slug }: PlaybookExecutionLogProps) {
  const [entries, setEntries] = useState<PlaybookExecution[]>([])
  const [loading, setLoading] = useState(true)
  const [expanded, setExpanded] = useState(false)
  const [showAll, setShowAll] = useState(false)

  const load = useCallback(() => {
    void fetchPlaybookExecutions(slug).then((rows) => setEntries(rows))
  }, [slug])

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    fetchPlaybookExecutions(slug)
      .then((rows) => {
        if (cancelled) return
        setEntries(rows)
      })
      .catch(() => {
        if (!cancelled) setEntries([])
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [slug])

  useEffect(() => {
    const unsubscribe = subscribePlaybookEvents(slug, () => {
      load()
    })
    return unsubscribe
  }, [slug, load])

  const visible = showAll ? entries : entries.slice(0, INITIAL_LIMIT)

  return (
    <section
      className="wk-playbook-executions"
      aria-labelledby="wk-playbook-executions-heading"
      data-testid="wk-playbook-executions"
    >
      <button
        type="button"
        className="wk-playbook-executions__toggle"
        aria-expanded={expanded}
        onClick={() => setExpanded((v) => !v)}
      >
        <h2 id="wk-playbook-executions-heading">
          Execution log
          <span className="wk-playbook-executions__count">
            {' '}({entries.length})
          </span>
        </h2>
        <span aria-hidden="true" className="wk-playbook-executions__chev">
          {expanded ? '▾' : '▸'}
        </span>
      </button>
      {expanded && (
        <div className="wk-playbook-executions__body">
          {loading ? (
            <p className="wk-playbook-executions__loading">loading executions…</p>
          ) : entries.length === 0 ? (
            <p className="wk-playbook-executions__empty">
              No executions recorded yet. Agents will log outcomes here as they run the playbook.
            </p>
          ) : (
            <>
              <ol className="wk-playbook-executions__list">
                {visible.map((e) => (
                  <li key={e.id} className={`wk-playbook-execution wk-playbook-execution--${e.outcome}`}>
                    <span className={`wk-playbook-execution__pill wk-playbook-execution__pill--${e.outcome}`}>
                      {e.outcome}
                    </span>
                    <div className="wk-playbook-execution__body">
                      <p className="wk-playbook-execution__summary">{e.summary}</p>
                      {e.notes && (
                        <p className="wk-playbook-execution__notes">{e.notes}</p>
                      )}
                      <span className="wk-playbook-execution__meta">
                        {formatAgentName(e.recorded_by)}
                        {' · '}
                        <time dateTime={e.created_at}>{formatShortTs(e.created_at)}</time>
                      </span>
                    </div>
                  </li>
                ))}
              </ol>
              {entries.length > INITIAL_LIMIT && (
                <button
                  type="button"
                  className="wk-playbook-executions__more"
                  onClick={() => setShowAll((v) => !v)}
                >
                  {showAll
                    ? 'show recent only'
                    : `show all (${entries.length - INITIAL_LIMIT} more)`}
                </button>
              )}
            </>
          )}
        </div>
      )}
    </section>
  )
}

function formatShortTs(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toISOString().slice(0, 10)
}
