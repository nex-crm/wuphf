import { useCallback, useMemo, useState } from 'react'
import { post } from '../../api/client'
import { useQueryClient } from '@tanstack/react-query'

type Provider = 'claude' | 'openai' | 'gemini'

interface AgentFormData {
  name: string
  slug: string
  role: string
  emoji: string
  provider: Provider
  expertise: string
}

const INITIAL_FORM: AgentFormData = {
  name: '',
  slug: '',
  role: '',
  emoji: '',
  provider: 'claude',
  expertise: '',
}

const PROVIDERS: { value: Provider; label: string }[] = [
  { value: 'claude', label: 'Claude (Anthropic)' },
  { value: 'openai', label: 'OpenAI' },
  { value: 'gemini', label: 'Gemini (Google)' },
]

function slugify(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
}

interface AgentWizardProps {
  open: boolean
  onClose: () => void
  onCreated?: () => void
}

export function AgentWizard({ open, onClose, onCreated }: AgentWizardProps) {
  const [form, setForm] = useState<AgentFormData>(INITIAL_FORM)
  const [slugEdited, setSlugEdited] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const queryClient = useQueryClient()

  const updateField = useCallback(
    <K extends keyof AgentFormData>(field: K, value: AgentFormData[K]) => {
      setForm((prev) => {
        const next = { ...prev, [field]: value }
        if (field === 'name' && !slugEdited) {
          next.slug = slugify(value as string)
        }
        return next
      })
      setError(null)
    },
    [slugEdited],
  )

  const expertiseTags = useMemo(() => {
    return form.expertise
      .split(',')
      .map((t) => t.trim())
      .filter(Boolean)
  }, [form.expertise])

  const canSubmit = form.name.trim().length > 0 && form.slug.trim().length > 0

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()

    if (!canSubmit) return
    setSubmitting(true)
    setError(null)

    try {
      const body = {
        slug: form.slug,
        name: form.name,
        role: form.role || undefined,
        emoji: form.emoji || undefined,
        provider: form.provider,
        expertise: expertiseTags.length > 0 ? expertiseTags : undefined,
      }

      await post('/office-members', body)
      await queryClient.invalidateQueries({ queryKey: ['office-members'] })

      setForm(INITIAL_FORM)
      setSlugEdited(false)
      onCreated?.()
      onClose()
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : 'Failed to create agent'
      setError(message)
    } finally {
      setSubmitting(false)
    }
  }

  function handleCancel() {
    setForm(INITIAL_FORM)
    setSlugEdited(false)
    setError(null)
    onClose()
  }

  function handleOverlayClick(e: React.MouseEvent) {
    if (e.target === e.currentTarget) {
      handleCancel()
    }
  }

  if (!open) return null

  return (
    <div className="agent-wizard-overlay" onClick={handleOverlayClick}>
      <div className="agent-wizard-modal card">
        <div className="agent-wizard-title">Create agent</div>

        <form className="agent-wizard-form" onSubmit={handleSubmit}>
          {/* Name */}
          <div className="agent-wizard-field">
            <label className="label" htmlFor="agent-name">Name</label>
            <input
              id="agent-name"
              className="input"
              type="text"
              placeholder="e.g. Sales Rep"
              value={form.name}
              onChange={(e) => updateField('name', e.target.value)}
              autoFocus
            />
          </div>

          {/* Slug */}
          <div className="agent-wizard-field">
            <label className="label" htmlFor="agent-slug">Slug</label>
            <input
              id="agent-slug"
              className="input"
              type="text"
              placeholder="auto-generated-from-name"
              value={form.slug}
              onChange={(e) => {
                setSlugEdited(true)
                updateField('slug', e.target.value)
              }}
            />
          </div>

          {/* Role */}
          <div className="agent-wizard-field">
            <label className="label" htmlFor="agent-role">Role</label>
            <input
              id="agent-role"
              className="input"
              type="text"
              placeholder="e.g. SDR, Engineer, Support"
              value={form.role}
              onChange={(e) => updateField('role', e.target.value)}
            />
          </div>

          {/* Emoji */}
          <div className="agent-wizard-field">
            <label className="label" htmlFor="agent-emoji">Emoji</label>
            <input
              id="agent-emoji"
              className="input"
              type="text"
              placeholder="e.g. robot face"
              value={form.emoji}
              onChange={(e) => updateField('emoji', e.target.value)}
              maxLength={4}
              style={{ width: 80 }}
            />
          </div>

          {/* Provider */}
          <div className="agent-wizard-field">
            <label className="label" htmlFor="agent-provider">Provider</label>
            <select
              id="agent-provider"
              value={form.provider}
              onChange={(e) => updateField('provider', e.target.value as Provider)}
            >
              {PROVIDERS.map((p) => (
                <option key={p.value} value={p.value}>{p.label}</option>
              ))}
            </select>
          </div>

          {/* Expertise */}
          <div className="agent-wizard-field">
            <label className="label" htmlFor="agent-expertise">
              Expertise <span style={{ fontWeight: 400, color: 'var(--text-tertiary)' }}>(comma-separated)</span>
            </label>
            <input
              id="agent-expertise"
              className="input"
              type="text"
              placeholder="e.g. outreach, cold email, pipeline"
              value={form.expertise}
              onChange={(e) => updateField('expertise', e.target.value)}
            />
            {expertiseTags.length > 0 && (
              <div className="agent-panel-tags" style={{ marginTop: 6 }}>
                {expertiseTags.map((tag) => (
                  <span key={tag} className="agent-panel-tag">{tag}</span>
                ))}
              </div>
            )}
          </div>

          {error && <div className="agent-wizard-error">{error}</div>}

          {/* Footer */}
          <div className="agent-wizard-footer">
            <button
              type="button"
              className="btn btn-ghost btn-sm"
              onClick={handleCancel}
              disabled={submitting}
            >
              Cancel
            </button>
            <button
              type="submit"
              className="btn btn-primary btn-sm"
              disabled={!canSubmit || submitting}
            >
              {submitting ? 'Creating...' : 'Create'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

/**
 * Hook to manage wizard open/close state from any component.
 * Usage:
 *   const { open, show, hide } = useAgentWizard()
 *   <button onClick={show}>New Agent</button>
 *   <AgentWizard open={open} onClose={hide} />
 */
export function useAgentWizard() {
  const [open, setOpen] = useState(false)
  const show = useCallback(() => setOpen(true), [])
  const hide = useCallback(() => setOpen(false), [])
  return { open, show, hide }
}
