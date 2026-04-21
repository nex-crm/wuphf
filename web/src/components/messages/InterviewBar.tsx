import { useEffect, useMemo, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { NavArrowLeft, NavArrowRight, Xmark } from 'iconoir-react'
import { answerRequest, post, type AgentRequest, type InterviewOption } from '../../api/client'
import { useRequests } from '../../hooks/useRequests'
import { showNotice } from '../ui/Toast'

/**
 * Inline interview bar shown above the Composer. Mirrors the TUI behavior:
 * - Shows the current pending request (1/N counter for the queue)
 * - Allows cycling through queued requests with prev/next
 * - Renders option buttons; if the picked option requires custom text,
 *   switches to a text input mode using the option's hint as placeholder
 * - Skip / close pauses the office (POST /signals kind=pause) and dismisses
 */
export function InterviewBar() {
  const { pending } = useRequests()
  const queryClient = useQueryClient()

  const queue = useMemo(() => {
    // Sort by created_at ascending so the oldest blocking request is first.
    const sorted = [...pending].sort((a, b) => {
      const ta = a.created_at ?? ''
      const tb = b.created_at ?? ''
      return ta.localeCompare(tb)
    })
    return sorted
  }, [pending])

  const [cursor, setCursor] = useState(0)
  const [textMode, setTextMode] = useState<{ option: InterviewOption } | null>(null)
  const [customText, setCustomText] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [dismissedIds, setDismissedIds] = useState<Set<string>>(new Set())
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  const visible = queue.filter((r) => !dismissedIds.has(r.id))
  const safeCursor = Math.min(cursor, Math.max(visible.length - 1, 0))
  const current = visible[safeCursor] ?? null

  // Reset text mode when the active request changes
  useEffect(() => {
    setTextMode(null)
    setCustomText('')
  }, [current?.id])

  // Auto-focus the text input when entering text mode
  useEffect(() => {
    if (textMode && textareaRef.current) {
      textareaRef.current.focus()
    }
  }, [textMode])

  if (!current) return null

  const rawOptions = current.options ?? current.choices ?? []
  const options = [...rawOptions].sort((a, b) => {
    const ar = a.id === current.recommended_id ? 0 : 1
    const br = b.id === current.recommended_id ? 0 : 1
    return ar - br
  })

  const submit = async (option: InterviewOption, text?: string) => {
    if (submitting) return
    setSubmitting(true)
    try {
      await answerRequest(current.id, option.id, text)
      await queryClient.invalidateQueries({ queryKey: ['requests'] })
      setTextMode(null)
      setCustomText('')
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : 'Failed to answer'
      showNotice(message, 'error')
    } finally {
      setSubmitting(false)
    }
  }

  const handleOption = (option: InterviewOption) => {
    if (option.requires_text) {
      setTextMode({ option })
      setCustomText('')
      return
    }
    submit(option)
  }

  const handlePause = async () => {
    // Skip = pause the office. Matches the TUI Esc behavior.
    setDismissedIds((prev) => {
      const next = new Set(prev)
      next.add(current.id)
      return next
    })
    setTextMode(null)
    try {
      await post('/signals', { kind: 'pause', summary: 'Human skipped a blocking interview' })
      showNotice('Office paused. Use /resume when ready.', 'info')
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : 'Failed to pause office'
      showNotice(message, 'error')
    }
  }

  const handleNext = () => setCursor((i) => Math.min(i + 1, visible.length - 1))
  const handlePrev = () => setCursor((i) => Math.max(i - 1, 0))

  return (
    <div className="interview-bar" role="region" aria-label="Pending agent interview">
      <div className="interview-bar-head">
        <span className="badge badge-yellow">BLOCKING</span>
        <span className="interview-bar-from">@{current.from || 'agent'} asks</span>
        {current.channel && (
          <span className="interview-bar-channel">in #{current.channel}</span>
        )}
        <span className="interview-bar-counter">
          {safeCursor + 1}/{visible.length}
        </span>
        <div className="interview-bar-cycle">
          <button
            type="button"
            className="interview-bar-icon-btn"
            onClick={handlePrev}
            disabled={safeCursor === 0}
            aria-label="Previous request"
            title="Previous"
          >
            <NavArrowLeft width={16} height={16} />
          </button>
          <button
            type="button"
            className="interview-bar-icon-btn"
            onClick={handleNext}
            disabled={safeCursor >= visible.length - 1}
            aria-label="Next request"
            title="Next"
          >
            <NavArrowRight width={16} height={16} />
          </button>
        </div>
        <button
          type="button"
          className="interview-bar-close"
          onClick={handlePause}
          aria-label="Skip and pause office"
          title="Skip — pause office"
        >
          <Xmark width={20} height={20} />
        </button>
      </div>

      <div className="interview-bar-body">
        {current.title && current.title !== 'Request' && (
          <div className="interview-bar-title">{current.title}</div>
        )}
        <div className="interview-bar-question">
          {(current.question || '').replace(/\*\*/g, '').replace(/^\s*\d+\.\s*/, '')}
        </div>
        {current.context && (
          <div className="interview-bar-context">{current.context}</div>
        )}
      </div>

      {textMode ? (
        <div className="interview-bar-text">
          <textarea
            ref={textareaRef}
            className="interview-bar-textarea"
            placeholder={textMode.option.text_hint || 'Type your answer...'}
            value={customText}
            onChange={(e) => setCustomText(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Escape') {
                e.preventDefault()
                setTextMode(null)
              }
              if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
                e.preventDefault()
                if (customText.trim()) submit(textMode.option, customText.trim())
              }
            }}
            rows={3}
          />
          <div className="interview-bar-text-actions">
            <button
              type="button"
              className="btn btn-ghost btn-sm"
              onClick={() => setTextMode(null)}
              disabled={submitting}
            >
              Back
            </button>
            <button
              type="button"
              className="btn btn-primary btn-sm"
              onClick={() => submit(textMode.option, customText.trim())}
              disabled={submitting || !customText.trim()}
            >
              {submitting ? 'Sending...' : `Send as ${textMode.option.label}`}
            </button>
          </div>
        </div>
      ) : options.length > 0 ? (
        <div className="interview-bar-actions">
          {options.map((opt, i) => (
            <button
              key={opt.id}
              type="button"
              className={`btn btn-sm ${opt.id === current.recommended_id ? 'btn-primary' : 'btn-ghost'}`}
              onClick={() => handleOption(opt)}
              disabled={submitting}
              title={opt.description}
            >
              <span className="interview-bar-opt-num">{i + 1}</span>
              <span className="interview-bar-opt-label">{opt.label}</span>
              {opt.requires_text && <span className="interview-bar-text-hint"> · type</span>}
            </button>
          ))}
        </div>
      ) : (
        <div className="interview-bar-empty">No options provided.</div>
      )}
    </div>
  )
}
