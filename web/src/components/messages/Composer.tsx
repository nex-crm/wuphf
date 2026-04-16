import { useRef, useState, useCallback } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { postMessage } from '../../api/client'
import { useAppStore } from '../../stores/app'

export function Composer() {
  const currentChannel = useAppStore((s) => s.currentChannel)
  const [text, setText] = useState('')
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const queryClient = useQueryClient()

  const sendMutation = useMutation({
    mutationFn: (content: string) => postMessage(content, currentChannel),
    onSuccess: () => {
      setText('')
      if (textareaRef.current) {
        textareaRef.current.style.height = 'auto'
      }
      // Immediately refetch messages
      queryClient.invalidateQueries({ queryKey: ['messages', currentChannel] })
    },
  })

  const handleSend = useCallback(() => {
    const trimmed = text.trim()
    if (!trimmed || sendMutation.isPending) return
    sendMutation.mutate(trimmed)
  }, [text, sendMutation])

  const handleKeyDown = useCallback((e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleSend()
    }
  }, [handleSend])

  const handleInput = useCallback(() => {
    const el = textareaRef.current
    if (el) {
      el.style.height = 'auto'
      el.style.height = Math.min(el.scrollHeight, 120) + 'px'
    }
  }, [])

  return (
    <div className="composer">
      <div className="composer-inner">
        <textarea
          ref={textareaRef}
          className="composer-input"
          placeholder={`Message #${currentChannel}`}
          value={text}
          onChange={(e) => { setText(e.target.value); handleInput() }}
          onKeyDown={handleKeyDown}
          rows={1}
        />
        <button
          className="composer-send"
          disabled={!text.trim() || sendMutation.isPending}
          onClick={handleSend}
          aria-label="Send message"
        >
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <path d="m22 2-7 20-4-9-9-4Z" />
            <path d="M22 2 11 13" />
          </svg>
        </button>
      </div>
    </div>
  )
}
