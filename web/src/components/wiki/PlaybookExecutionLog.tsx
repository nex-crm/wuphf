import { useCallback, useEffect, useState } from 'react'
import {
  fetchPlaybookExecutions,
  fetchSynthesisStatus,
  subscribePlaybookEvents,
  subscribePlaybookSynthesizedEvents,
  synthesizeNow,
  type PlaybookExecution,
  type PlaybookSynthesisStatus,
} from '../../api/playbook'
import { formatAgentName } from '../../lib/agentName'

interface PlaybookExecutionLogProps {
  slug: string
}

const INITIAL_LIMIT = 10

type SynthState = 'idle' | 'pending' | 'success' | 'error'

/**
 * Collapsible execution-log panel rendered on playbook article pages.
 * Newest-first, capped at INITIAL_LIMIT by default — the full log is
 * available in `team/playbooks/{slug}.executions.jsonl` for auditing.
 *
 * Also hosts the compounding-intelligence surface:
 *   - "Last synthesis" badge summarising archivist activity.
 *   - "Re-synthesize" button that triggers POST /playbook/synthesize.
 * The playbook article reloads automatically via the existing wiki:write
 * SSE event when synthesis commits; this component listens for
 * playbook:synthesized specifically to refresh its own status strip.
 */
export default function PlaybookExecutionLog({ slug }: PlaybookExecutionLogProps) {
  const [entries, setEntries] = useState<PlaybookExecution[]>([])
  const [loading, setLoading] = useState(true)
  const [expanded, setExpanded] = useState(false)
  const [showAll, setShowAll] = useState(false)
  const [status, setStatus] = useState<PlaybookSynthesisStatus | null>(null)
  const [synthState, setSynthState] = useState<SynthState>('idle')

  const load = useCallback(() => {
    void fetchPlaybookExecutions(slug).then((rows) => setEntries(rows))
  }, [slug])

  const loadStatus = useCallback(() => {
    void fetchSynthesisStatus(slug).then((s) => setStatus(s))
  }, [slug])

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    Promise.all([fetchPlaybookExecutions(slug), fetchSynthesisStatus(slug)])
      .then(([rows, s]) => {
        if (cancelled) return
        setEntries(rows)
        setStatus(s)
      })
      .catch(() => {
        if (!cancelled) {
          setEntries([])
          setStatus(null)
        }
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [slug])

  useEffect(() => {
    const unsubscribeExec = subscribePlaybookEvents(slug, () => {
      load()
      loadStatus()
    })
    const unsubscribeSynth = subscribePlaybookSynthesizedEvents(slug, () => {
      loadStatus()
      setSynthState((prev) => (prev === 'pending' ? 'success' : prev))
    })
    return () => {
      unsubscribeExec()
      unsubscribeSynth()
    }
  }, [slug, load, loadStatus])

  const visible = showAll ? entries : entries.slice(0, INITIAL_LIMIT)

  const handleResynthesize = async () => {
    setSynthState('pending')
    const result = await synthesizeNow(slug)
    if (!result) {
      setSynthState('error')
      return
    }
    // success transitions on the playbook:synthesized event; fall back to a
    // timer so the button doesn't get stuck if the SSE stream is dropped.
    window.setTimeout(() => {
      setSynthState((prev) => (prev === 'pending' ? 'success' : prev))
    }, 8000)
  }

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
          <SynthesisFooter
            status={status}
            synthState={synthState}
            onResynthesize={handleResynthesize}
          />
        </div>
      )}
    </section>
  )
}

interface SynthesisFooterProps {
  status: PlaybookSynthesisStatus | null
  synthState: SynthState
  onResynthesize: () => void
}

function SynthesisFooter({ status, synthState, onResynthesize }: SynthesisFooterProps) {
  const lastLabel = status?.last_synthesized_ts
    ? `Last synthesis: ${formatRelativeTs(status.last_synthesized_ts)} · ${status.execution_count} execution${status.execution_count === 1 ? '' : 's'}`
    : 'No synthesis yet — learnings will be added after the first archivist run.'
  const pendingLabel =
    status && status.executions_since_last_synthesis > 0
      ? `${status.executions_since_last_synthesis} new execution${status.executions_since_last_synthesis === 1 ? '' : 's'} since last synthesis`
      : null

  const buttonDisabled = synthState === 'pending'
  let buttonLabel = 'Re-synthesize'
  if (synthState === 'pending') buttonLabel = 'Synthesizing…'
  if (synthState === 'success') buttonLabel = 'Synthesized ✓'
  if (synthState === 'error') buttonLabel = 'Retry synthesis'

  return (
    <div
      className="wk-playbook-synthesis"
      data-testid="wk-playbook-synthesis"
      data-state={synthState}
    >
      <div className="wk-playbook-synthesis__status">
        <span className="wk-playbook-synthesis__badge">{lastLabel}</span>
        {pendingLabel && (
          <span className="wk-playbook-synthesis__pending">{pendingLabel}</span>
        )}
      </div>
      <button
        type="button"
        className="wk-playbook-synthesis__button"
        onClick={onResynthesize}
        disabled={buttonDisabled}
        data-testid="wk-playbook-synthesis-button"
      >
        {buttonLabel}
      </button>
    </div>
  )
}

function formatShortTs(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toISOString().slice(0, 10)
}

function formatRelativeTs(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  const diffMs = Date.now() - d.getTime()
  if (diffMs < 0) return 'just now'
  const mins = Math.floor(diffMs / 60000)
  if (mins < 1) return 'just now'
  if (mins < 60) return `${mins}m ago`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours}h ago`
  const days = Math.floor(hours / 24)
  if (days < 14) return `${days}d ago`
  return d.toISOString().slice(0, 10)
}
