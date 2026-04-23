import { useEffect, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { getConfig, updateConfig, type LLMProvider } from '../../api/client'
import { showNotice } from './Toast'
import { confirm } from './ConfirmDialog'

let requestOpen: (() => void) | null = null

/** Imperatively open the provider switcher from anywhere. */
export function openProviderSwitcher() {
  if (!requestOpen) {
    showNotice('Provider switcher is not available right now.', 'error')
    return
  }
  requestOpen()
}

interface ProviderOption {
  id: LLMProvider
  name: string
  desc: string
}

const PROVIDERS: ProviderOption[] = [
  { id: 'claude-code', name: 'Claude Code', desc: 'Anthropic Claude via Claude Code CLI' },
  { id: 'codex', name: 'Codex', desc: 'OpenAI Codex CLI agent' },
  { id: 'opencode', name: 'Opencode', desc: 'Opencode CLI — routes to Claude, OpenAI, or local/Ollama' },
]

export function ProviderSwitcherHost() {
  const [open, setOpen] = useState(false)
  const [current, setCurrent] = useState<LLMProvider | null>(null)
  const [loading, setLoading] = useState(false)
  const [pending, setPending] = useState<LLMProvider | null>(null)
  const queryClient = useQueryClient()

  useEffect(() => {
    requestOpen = () => {
      setOpen(true)
      setLoading(true)
      getConfig()
        .then((cfg) => setCurrent(cfg.llm_provider ?? 'claude-code'))
        .catch(() => setCurrent('claude-code'))
        .finally(() => setLoading(false))
    }
    return () => {
      if (requestOpen !== null) requestOpen = null
    }
  }, [])

  useEffect(() => {
    if (!open) return
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [open])

  if (!open) return null

  async function switchTo(p: ProviderOption) {
    if (!current || p.id === current) return
    confirm({
      title: 'Switch runtime provider?',
      message: `Agents will be restarted on ${p.name}.`,
      confirmLabel: 'Switch',
      onConfirm: async () => {
        setPending(p.id)
        try {
          await updateConfig({ llm_provider: p.id })
          await queryClient.invalidateQueries({ queryKey: ['config'] })
          await queryClient.invalidateQueries({ queryKey: ['health'] })
          setCurrent(p.id)
          showNotice(`Provider switched to ${p.name}`, 'success')
          setOpen(false)
        } catch (err: unknown) {
          const message = err instanceof Error ? err.message : 'Switch failed'
          showNotice('Switch failed: ' + message, 'error')
        } finally {
          setPending(null)
        }
      },
    })
  }

  return (
    <div
      className="provider-overlay"
      onClick={(e) => {
        if (e.target === e.currentTarget) setOpen(false)
      }}
      role="dialog"
      aria-modal="true"
      aria-labelledby="provider-title"
    >
      <div className="provider-panel card">
        <h3 id="provider-title" className="provider-title">Runtime provider</h3>
        {loading ? (
          <p className="provider-loading">Loading current provider...</p>
        ) : (
          <div className="provider-options">
            {PROVIDERS.map((p) => {
              const isActive = current === p.id
              const isPending = pending === p.id
              return (
                <button
                  key={p.id}
                  type="button"
                  className={`provider-option${isActive ? ' active' : ''}`}
                  onClick={() => switchTo(p)}
                  disabled={isActive || isPending}
                >
                  <div className="provider-option-text">
                    <div className="provider-option-name">{p.name}</div>
                    <div className="provider-option-desc">{p.desc}</div>
                  </div>
                  {isActive && <span className="provider-option-check">{'\u2713'}</span>}
                  {isPending && <span className="provider-option-check">...</span>}
                </button>
              )
            })}
          </div>
        )}
        <div className="provider-footer">
          <button type="button" className="btn btn-ghost btn-sm" onClick={() => setOpen(false)}>
            Close
          </button>
        </div>
      </div>
    </div>
  )
}
