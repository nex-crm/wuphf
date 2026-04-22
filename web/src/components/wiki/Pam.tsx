import { useCallback, useEffect, useRef, useState } from 'react'
import { PixelAvatar } from '../ui/PixelAvatar'
import {
  listPamActions,
  subscribePamEvents,
  type PamActionDescriptor,
  type PamActionEvent,
} from '../../api/pam'
import { buildPamMenu, type PamMenuEntry } from '../../lib/pamActions'
import '../../styles/pam.css'

interface PamProps {
  articlePath: string
}

type Status =
  | { kind: 'idle' }
  | { kind: 'running'; label: string }
  | { kind: 'done'; label: string }
  | { kind: 'failed'; message: string }

const STATUS_CLEAR_MS = 4000

/**
 * Pam — the wiki archivist, perched on top of the article column. Click Pam
 * to open her desk menu (served from GET /pam/actions so the registry stays
 * server-defined). Selecting an action POSTs to /pam/action; the dispatcher
 * spawns Pam's sub-process and fans results back via /events so we update
 * the status line without polling.
 */
export default function Pam({ articlePath }: PamProps) {
  const [menu, setMenu] = useState<PamMenuEntry[] | null>(null)
  const [menuOpen, setMenuOpen] = useState(false)
  const [status, setStatus] = useState<Status>({ kind: 'idle' })
  const [activeJobId, setActiveJobId] = useState<number | null>(null)

  const wrapRef = useRef<HTMLDivElement>(null)
  const statusTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  // Fetch the action registry once on mount. Failure falls through to an
  // empty menu — Pam still renders so the character is visible.
  useEffect(() => {
    let cancelled = false
    listPamActions()
      .then((res) => {
        if (cancelled) return
        const descriptors: PamActionDescriptor[] = res.actions ?? []
        setMenu(buildPamMenu(descriptors))
      })
      .catch(() => {
        if (cancelled) return
        setMenu([])
      })
    return () => {
      cancelled = true
    }
  }, [])

  // Subscribe to Pam's SSE progress events. We filter by the job id we
  // started so overlapping actions from other surfaces don't flash status
  // here. The status line auto-clears on success after STATUS_CLEAR_MS.
  useEffect(() => {
    const unsub = subscribePamEvents((evt: PamActionEvent) => {
      if (evt.kind === 'started') {
        if (activeJobId !== null && evt.job_id === activeJobId) {
          setStatus({ kind: 'running', label: labelFor(evt.action, menu) })
        }
        return
      }
      if (evt.kind === 'done') {
        if (activeJobId !== null && evt.job_id === activeJobId) {
          setStatus({ kind: 'done', label: labelFor(evt.action, menu) })
          setActiveJobId(null)
          scheduleClear()
        }
        return
      }
      if (evt.kind === 'failed') {
        if (activeJobId !== null && evt.job_id === activeJobId) {
          setStatus({ kind: 'failed', message: evt.error || 'Pam could not finish.' })
          setActiveJobId(null)
          scheduleClear()
        }
      }
    })
    return () => {
      unsub()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeJobId, menu])

  // Close menu on outside click so it doesn't linger when the user moves
  // on. Keep it simple: single global listener, cleaned up on unmount.
  useEffect(() => {
    if (!menuOpen) return
    function onDoc(e: MouseEvent) {
      if (!wrapRef.current) return
      if (!wrapRef.current.contains(e.target as Node)) setMenuOpen(false)
    }
    document.addEventListener('mousedown', onDoc)
    return () => document.removeEventListener('mousedown', onDoc)
  }, [menuOpen])

  useEffect(() => {
    return () => {
      if (statusTimerRef.current) clearTimeout(statusTimerRef.current)
    }
  }, [])

  const scheduleClear = useCallback(() => {
    if (statusTimerRef.current) clearTimeout(statusTimerRef.current)
    statusTimerRef.current = setTimeout(() => {
      setStatus({ kind: 'idle' })
    }, STATUS_CLEAR_MS)
  }, [])

  const runAction = useCallback(
    async (entry: PamMenuEntry) => {
      setMenuOpen(false)
      setStatus({ kind: 'running', label: entry.label })
      try {
        const { job_id } = await entry.run({ articlePath })
        setActiveJobId(job_id)
      } catch (err) {
        const msg = err instanceof Error ? err.message : 'Pam could not start.'
        setStatus({ kind: 'failed', message: msg })
        setActiveJobId(null)
        scheduleClear()
      }
    },
    [articlePath, scheduleClear],
  )

  const busy = status.kind === 'running'

  return (
    <div ref={wrapRef} className="pam-wrap" data-testid="pam-wrap">
      <button
        type="button"
        className="pam-button"
        data-busy={busy ? 'true' : 'false'}
        aria-haspopup="menu"
        aria-expanded={menuOpen}
        aria-label="Pam the Archivist"
        title="Pam — click for options"
        onClick={() => setMenuOpen((v) => !v)}
      >
        <PixelAvatar slug="pam" size={56} className="pam-avatar" />
      </button>
      <div className="pam-desk" aria-hidden="true" />

      {menuOpen && (
        <div className="pam-menu" role="menu" aria-label="Pam's actions">
          <div className="pam-menu-header">Pam can help with</div>
          {menu === null ? (
            <div className="pam-menu-empty">Loading…</div>
          ) : menu.length === 0 ? (
            <div className="pam-menu-empty">No actions available.</div>
          ) : (
            menu.map((entry) => (
              <button
                key={entry.id}
                type="button"
                role="menuitem"
                className="pam-menu-item"
                disabled={busy}
                onClick={() => {
                  void runAction(entry)
                }}
              >
                {entry.label}
              </button>
            ))
          )}
        </div>
      )}

      {!menuOpen && status.kind !== 'idle' && (
        <div className="pam-status" role="status">
          {renderStatus(status)}
        </div>
      )}
    </div>
  )
}

function renderStatus(status: Status): string {
  switch (status.kind) {
    case 'running':
      return `Pam is working: ${status.label}…`
    case 'done':
      return `Done: ${status.label}`
    case 'failed':
      return `Pam: ${status.message}`
    default:
      return ''
  }
}

function labelFor(id: string, menu: PamMenuEntry[] | null): string {
  if (!menu) return id
  const hit = menu.find((m) => m.id === id)
  return hit?.label ?? id
}
