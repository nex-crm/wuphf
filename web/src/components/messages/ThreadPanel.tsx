import { useState, useRef, useEffect } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useThreadMessages } from '../../hooks/useMessages'
import { useAppStore } from '../../stores/app'
import { postMessage } from '../../api/client'
import { showNotice } from '../ui/Toast'
import { MessageBubble } from './MessageBubble'

export function ThreadPanel() {
  const activeThreadId = useAppStore((s) => s.activeThreadId)
  const setActiveThreadId = useAppStore((s) => s.setActiveThreadId)
  const currentChannel = useAppStore((s) => s.currentChannel)
  const [text, setText] = useState('')
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const messagesRef = useRef<HTMLDivElement>(null)
  const queryClient = useQueryClient()

  const { data: replies = [] } = useThreadMessages(currentChannel, activeThreadId)

  // Auto-scroll
  useEffect(() => {
    if (messagesRef.current) {
      messagesRef.current.scrollTop = messagesRef.current.scrollHeight
    }
  }, [replies.length])

  const sendReply = useMutation({
    mutationFn: (content: string) => postMessage(content, currentChannel, activeThreadId ?? undefined),
    onSuccess: () => {
      setText('')
      queryClient.invalidateQueries({ queryKey: ['thread-messages', currentChannel, activeThreadId] })
      queryClient.invalidateQueries({ queryKey: ['messages', currentChannel] })
    },
    onError: (err: unknown) => {
      const message = err instanceof Error ? err.message : 'Failed to send reply'
      showNotice(message, 'error')
    },
  })

  const handleSend = () => {
    const trimmed = text.trim()
    if (!trimmed || sendReply.isPending) return
    sendReply.mutate(trimmed)
  }

  if (!activeThreadId) return null

  return (
    <div className="thread-panel open">
      <div className="thread-panel-header">
        <span className="thread-panel-title">Thread</span>
        <button
          className="thread-panel-close"
          onClick={() => setActiveThreadId(null)}
          aria-label="Close thread"
        >
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <path d="M18 6 6 18" />
            <path d="m6 6 12 12" />
          </svg>
        </button>
      </div>

      <div ref={messagesRef} style={{ flex: 1, overflowY: 'auto', padding: 16, display: 'flex', flexDirection: 'column', gap: 12 }}>
        {replies.length === 0 ? (
          <div style={{ color: 'var(--text-tertiary)', fontSize: 13, textAlign: 'center', padding: 20 }}>
            No replies yet
          </div>
        ) : (
          replies.map((msg) => (
            <MessageBubble key={msg.id} message={msg} />
          ))
        )}
      </div>

      {/* Thread composer */}
      <div className="composer">
        <div className="composer-inner">
          <textarea
            ref={textareaRef}
            className="composer-input"
            placeholder="Reply..."
            value={text}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault()
                handleSend()
              }
            }}
            rows={1}
          />
          <button
            className="composer-send"
            disabled={!text.trim() || sendReply.isPending}
            onClick={handleSend}
            aria-label="Send reply"
          >
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="m22 2-7 20-4-9-9-4Z" />
              <path d="M22 2 11 13" />
            </svg>
          </button>
        </div>
      </div>
    </div>
  )
}
