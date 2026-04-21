import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  fetchBriefs,
  requestBriefSynthesis,
  subscribeEntityEvents,
  type BriefSummary,
  type EntityKind,
} from '../../api/entity'

interface EntityBriefBarProps {
  kind: EntityKind
  slug: string
  /**
   * Called after a successful synthesis arrives over SSE so the parent can
   * refetch article body + sources. Optional so the bar still works on its
   * own in isolation.
   */
  onSynthesized?: () => void
}

type BarState = 'idle' | 'synthesizing'

export default function EntityBriefBar({ kind, slug, onSynthesized }: EntityBriefBarProps) {
  const [brief, setBrief] = useState<BriefSummary | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [state, setState] = useState<BarState>('idle')
  const [pendingOverride, setPendingOverride] = useState<number | null>(null)

  const loadBrief = useCallback(async () => {
    try {
      const rows = await fetchBriefs()
      const match = rows.find((r) => r.kind === kind && r.slug === slug) ?? null
      setBrief(match)
      setPendingOverride(null)
      setError(null)
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to load brief status')
    } finally {
      setLoading(false)
    }
  }, [kind, slug])

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    ;(async () => {
      try {
        const rows = await fetchBriefs()
        if (cancelled) return
        const match = rows.find((r) => r.kind === kind && r.slug === slug) ?? null
        setBrief(match)
        setPendingOverride(null)
      } catch (err: unknown) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : 'Failed to load brief status')
        }
      } finally {
        if (!cancelled) setLoading(false)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [kind, slug])

  useEffect(() => {
    const unsubscribe = subscribeEntityEvents(
      kind,
      slug,
      () => {
        // New fact for this entity — bump pending without refetching.
        setPendingOverride((prev) => {
          const base = prev ?? brief?.pending_delta ?? 0
          return base + 1
        })
      },
      () => {
        // Brief was synthesized — clear in-flight state, refetch status,
        // notify parent so article body + sources refresh.
        setState('idle')
        void loadBrief()
        if (onSynthesized) onSynthesized()
      },
    )
    return unsubscribe
  }, [kind, slug, loadBrief, onSynthesized, brief?.pending_delta])

  const handleRefresh = useCallback(async () => {
    setState('synthesizing')
    setError(null)
    try {
      await requestBriefSynthesis({ entity_kind: kind, entity_slug: slug })
      // Wait for SSE to flip state back to idle. If SSE never arrives the
      // button stays "Synthesizing…" — that is deliberate: a user who sees
      // the label hang will reload and see the fresh brief on next render.
    } catch (err: unknown) {
      setState('idle')
      setError(err instanceof Error ? err.message : 'Synthesis request failed')
    }
  }, [kind, slug])

  const pending = useMemo(() => {
    if (pendingOverride !== null) return pendingOverride
    return brief?.pending_delta ?? 0
  }, [pendingOverride, brief?.pending_delta])

  if (loading) return null

  // If backend returned no row for this entity we still render — the brief
  // just hasn't been synthesized yet. Facts-on-file handles the empty case
  // for the body.
  const synthesizedTs = brief?.last_synthesized_ts ?? ''
  const relativeSynth = synthesizedTs ? formatRelativeTime(synthesizedTs) : 'never'
  const hasPending = pending > 0
  const cls = hasPending
    ? 'wk-entity-brief-bar wk-entity-brief-bar--pending'
    : 'wk-entity-brief-bar wk-entity-brief-bar--clean'

  return (
    <div
      className={cls}
      role="status"
      aria-live="polite"
      data-testid="wk-entity-brief-bar"
    >
      <span className="wk-entity-brief-bar__label">
        {hasPending ? (
          <>
            <strong>{pending}</strong> new {pending === 1 ? 'fact' : 'facts'} since last synthesis
          </>
        ) : (
          <>Brief synthesized {relativeSynth}. 0 new facts since.</>
        )}
      </span>
      {hasPending && (
        <button
          type="button"
          className="wk-entity-brief-bar__action"
          onClick={handleRefresh}
          disabled={state === 'synthesizing'}
        >
          {state === 'synthesizing' ? 'Synthesizing…' : 'Refresh brief'}
        </button>
      )}
      {error && <span className="wk-entity-brief-bar__error">{error}</span>}
    </div>
  )
}

function formatRelativeTime(iso: string): string {
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return iso
  const diffMs = Date.now() - t
  if (diffMs < 0) return 'just now'
  const sec = Math.floor(diffMs / 1000)
  if (sec < 60) return 'just now'
  const min = Math.floor(sec / 60)
  if (min < 60) return `${min} minute${min === 1 ? '' : 's'} ago`
  const hr = Math.floor(min / 60)
  if (hr < 24) return `${hr} hour${hr === 1 ? '' : 's'} ago`
  const day = Math.floor(hr / 24)
  if (day < 30) return `${day} day${day === 1 ? '' : 's'} ago`
  const mo = Math.floor(day / 30)
  if (mo < 12) return `${mo} month${mo === 1 ? '' : 's'} ago`
  const yr = Math.floor(day / 365)
  return `${yr} year${yr === 1 ? '' : 's'} ago`
}
