import { useCallback, useEffect, useState } from 'react'

interface ConfirmOptions {
  title?: string
  message: string
  confirmLabel?: string
  cancelLabel?: string
  /** Red "danger" styling for destructive actions. */
  danger?: boolean
  onConfirm: () => void | Promise<void>
}

let requestConfirm: ((opts: ConfirmOptions) => void) | null = null

/**
 * Imperative confirm, callable from anywhere.
 *
 *   confirm({ message: 'Delete this?', danger: true, onConfirm: ... })
 */
export function confirm(opts: ConfirmOptions) {
  if (!requestConfirm) {
    // Host never mounted; fall back to native confirm so work isn't silently dropped.
    if (window.confirm(opts.message)) {
      void opts.onConfirm()
    }
    return
  }
  requestConfirm(opts)
}

/** Mount once near the root. */
export function ConfirmHost() {
  const [open, setOpen] = useState(false)
  const [opts, setOpts] = useState<ConfirmOptions | null>(null)
  const [running, setRunning] = useState(false)

  const close = useCallback(() => {
    setOpen(false)
    setOpts(null)
    setRunning(false)
  }, [])

  useEffect(() => {
    requestConfirm = (o) => {
      setOpts(o)
      setOpen(true)
    }
    return () => {
      if (requestConfirm !== null) requestConfirm = null
    }
  }, [])

  useEffect(() => {
    if (!open) return
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') close()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [open, close])

  if (!open || !opts) return null

  const run = async () => {
    if (running) return
    setRunning(true)
    try {
      await opts.onConfirm()
    } finally {
      close()
    }
  }

  return (
    <div
      className="confirm-overlay"
      onClick={(e) => {
        if (e.target === e.currentTarget) close()
      }}
      role="dialog"
      aria-modal="true"
    >
      <div className="confirm-card card">
        <h3 className="confirm-title">{opts.title || 'Are you sure?'}</h3>
        <p className="confirm-message">{opts.message}</p>
        <div className="confirm-actions">
          <button
            type="button"
            className="btn btn-ghost btn-sm"
            onClick={close}
            disabled={running}
          >
            {opts.cancelLabel || 'Cancel'}
          </button>
          <button
            type="button"
            className={`btn btn-sm ${opts.danger ? 'btn-danger' : 'btn-primary'}`}
            onClick={run}
            disabled={running}
            autoFocus
          >
            {running ? 'Working...' : opts.confirmLabel || 'Confirm'}
          </button>
        </div>
      </div>
    </div>
  )
}
