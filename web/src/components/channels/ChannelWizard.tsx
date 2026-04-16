import { useCallback, useState } from 'react'
import { createChannel, generateChannel } from '../../api/client'
import { useAppStore } from '../../stores/app'
import { useQueryClient } from '@tanstack/react-query'

type WizardMode = 'describe' | 'manual'

interface ManualFormData {
  slug: string
  name: string
  description: string
}

const INITIAL_MANUAL: ManualFormData = {
  slug: '',
  name: '',
  description: '',
}

function slugify(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
}

interface ChannelWizardProps {
  open: boolean
  onClose: () => void
}

export function ChannelWizard({ open, onClose }: ChannelWizardProps) {
  const [mode, setMode] = useState<WizardMode>('describe')
  const [prompt, setPrompt] = useState('')
  const [manual, setManual] = useState<ManualFormData>(INITIAL_MANUAL)
  const [slugEdited, setSlugEdited] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const setCurrentChannel = useAppStore((s) => s.setCurrentChannel)
  const queryClient = useQueryClient()

  const updateManualField = useCallback(
    <K extends keyof ManualFormData>(field: K, value: ManualFormData[K]) => {
      setManual((prev) => {
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

  function resetState() {
    setMode('describe')
    setPrompt('')
    setManual(INITIAL_MANUAL)
    setSlugEdited(false)
    setError(null)
  }

  function handleCancel() {
    resetState()
    onClose()
  }

  function handleOverlayClick(e: React.MouseEvent) {
    if (e.target === e.currentTarget) {
      handleCancel()
    }
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setSubmitting(true)
    setError(null)

    try {
      let newSlug: string

      if (mode === 'describe') {
        if (!prompt.trim()) {
          setError('Please describe the channel you want to create.')
          setSubmitting(false)
          return
        }
        const channel = await generateChannel(prompt.trim())
        newSlug = channel.slug
      } else {
        if (!manual.slug.trim() || !manual.name.trim()) {
          setError('Slug and name are required.')
          setSubmitting(false)
          return
        }
        await createChannel(manual.slug, manual.name, manual.description)
        newSlug = manual.slug
      }

      await queryClient.invalidateQueries({ queryKey: ['channels'] })
      resetState()
      onClose()
      setCurrentChannel(newSlug)
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : 'Failed to create channel'
      setError(message)
    } finally {
      setSubmitting(false)
    }
  }

  if (!open) return null

  return (
    <div className="channel-wizard-overlay" onClick={handleOverlayClick}>
      <div className="channel-wizard-modal card">
        <div className="channel-wizard-title">Create channel</div>

        {/* Mode toggle */}
        <div className="channel-wizard-tabs">
          <button
            type="button"
            className={`channel-wizard-tab${mode === 'describe' ? ' active' : ''}`}
            onClick={() => { setMode('describe'); setError(null) }}
          >
            Describe
          </button>
          <button
            type="button"
            className={`channel-wizard-tab${mode === 'manual' ? ' active' : ''}`}
            onClick={() => { setMode('manual'); setError(null) }}
          >
            Manual
          </button>
        </div>

        <form className="channel-wizard-form" onSubmit={handleSubmit}>
          {mode === 'describe' ? (
            <div className="channel-wizard-field">
              <label className="label" htmlFor="channel-prompt">
                Describe what this channel is for
              </label>
              <textarea
                id="channel-prompt"
                className="input channel-wizard-textarea"
                placeholder="e.g. A channel for the design team to share mockups and get feedback"
                value={prompt}
                onChange={(e) => { setPrompt(e.target.value); setError(null) }}
                rows={3}
                autoFocus
              />
              <span className="channel-wizard-hint">
                AI will generate a slug, name, and description for you.
              </span>
            </div>
          ) : (
            <>
              <div className="channel-wizard-field">
                <label className="label" htmlFor="channel-name">Name</label>
                <input
                  id="channel-name"
                  className="input"
                  type="text"
                  placeholder="e.g. Design Team"
                  value={manual.name}
                  onChange={(e) => updateManualField('name', e.target.value)}
                  autoFocus
                />
              </div>
              <div className="channel-wizard-field">
                <label className="label" htmlFor="channel-slug">Slug</label>
                <input
                  id="channel-slug"
                  className="input"
                  type="text"
                  placeholder="auto-generated-from-name"
                  value={manual.slug}
                  onChange={(e) => {
                    setSlugEdited(true)
                    updateManualField('slug', e.target.value)
                  }}
                />
              </div>
              <div className="channel-wizard-field">
                <label className="label" htmlFor="channel-description">Description</label>
                <input
                  id="channel-description"
                  className="input"
                  type="text"
                  placeholder="What is this channel about?"
                  value={manual.description}
                  onChange={(e) => updateManualField('description', e.target.value)}
                />
              </div>
            </>
          )}

          {error && <div className="channel-wizard-error">{error}</div>}

          <div className="channel-wizard-footer">
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
              disabled={submitting}
            >
              {submitting
                ? mode === 'describe' ? 'Generating...' : 'Creating...'
                : mode === 'describe' ? 'Generate' : 'Create'}
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
 *   const { open, show, hide } = useChannelWizard()
 *   <button onClick={show}>New Channel</button>
 *   <ChannelWizard open={open} onClose={hide} />
 */
export function useChannelWizard() {
  const [open, setOpen] = useState(false)
  const show = useCallback(() => setOpen(true), [])
  const hide = useCallback(() => setOpen(false), [])
  return { open, show, hide }
}
