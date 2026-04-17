import type { Message } from '../../api/client'
import { formatTime, formatTokens } from '../../lib/format'
import { formatMarkdown } from '../../lib/markdown'
import { useAppStore } from '../../stores/app'
import { toggleReaction } from '../../api/client'
import { useOfficeMembers } from '../../hooks/useMembers'
import { PixelAvatar } from '../ui/PixelAvatar'
import { showNotice } from '../ui/Toast'

interface MessageBubbleProps {
  message: Message
  grouped?: boolean
  onThreadClick?: (id: string) => void
}

export function MessageBubble({ message, grouped = false, onThreadClick }: MessageBubbleProps) {
  const currentChannel = useAppStore((s) => s.currentChannel)
  const { data: members = [] } = useOfficeMembers()
  const isHuman = message.from === 'you' || message.from === 'human'
  const agent = members.find((m) => m.slug === message.from)

  // Status messages — compact
  if (message.content?.startsWith('[STATUS]')) {
    const statusText = message.content.replace(/^\[STATUS\]\s*/, '')
    return <div className="message-status animate-fade">{statusText}</div>
  }

  const usageTotal = message.usage
    ? (message.usage.total_tokens ?? (
        (message.usage.input_tokens ?? 0) +
        (message.usage.output_tokens ?? 0) +
        (message.usage.cache_read_tokens ?? 0) +
        (message.usage.cache_creation_tokens ?? 0)
      ))
    : 0

  const reactions = message.reactions
    ? (Array.isArray(message.reactions)
        ? message.reactions as Array<{ emoji: string; count?: number }>
        : Object.entries(message.reactions).map(([emoji, users]) => ({
            emoji,
            count: Array.isArray(users) ? users.length : 1,
          })))
    : []

  // SECURITY: formatMarkdown escapes all HTML via escapeHtml() before rendering.
  // Only trusted broker messages use this path — human input renders as plain text.
  const renderedHtml = !isHuman ? formatMarkdown(message.content || '') : ''

  return (
    <div
      className={`message animate-fade${grouped ? ' message-grouped' : ''}`}
      data-msg-id={message.id}
    >
      {/* Avatar */}
      <div
        className="message-avatar"
        style={isHuman ? { background: 'var(--bg-warm)', color: 'var(--text-secondary)', fontSize: 12, fontWeight: 600 } : undefined}
      >
        {isHuman ? (
          'You'
        ) : (
          <PixelAvatar slug={message.from} size={36} />
        )}
      </div>

      {/* Content */}
      <div className="message-content">
        {/* Header */}
        <div className="message-header">
          <span className="message-author">
            {isHuman ? 'You' : (agent?.name || message.from)}
          </span>
          {isHuman ? (
            <span className="badge badge-neutral">human</span>
          ) : agent?.role ? (
            <span className="badge badge-green">{agent.role}</span>
          ) : null}
          <span className="message-time">{formatTime(message.timestamp)}</span>
          {usageTotal > 0 && (
            <span className="message-token-badge">{formatTokens(usageTotal)} tok</span>
          )}
        </div>

        {/* Text — human messages as plain text, agent messages as formatted markdown */}
        {isHuman ? (
          <div className="message-text">{message.content}</div>
        ) : (
          <div
            className="message-text"
            dangerouslySetInnerHTML={{ __html: renderedHtml }}
          />
        )}

        {/* Reactions */}
        {reactions.length > 0 && (
          <div className="message-reactions">
            {reactions.map((r) => (
              <button
                key={r.emoji}
                className="reaction-pill"
                onClick={() => {
                  toggleReaction(message.id, r.emoji, currentChannel).catch((e: Error) =>
                    showNotice('Reaction failed: ' + e.message, 'error'),
                  )
                }}
              >
                <span>{r.emoji}</span>
                <span className="reaction-pill-count">{r.count ?? 1}</span>
              </button>
            ))}
          </div>
        )}

        {/* Thread link */}
        {(message.thread_count ?? 0) > 0 && onThreadClick && (
          <button
            className="inline-thread-toggle"
            onClick={() => onThreadClick(message.id)}
          >
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="m9 18 6-6-6-6" />
            </svg>
            {message.thread_count} {message.thread_count === 1 ? 'reply' : 'replies'}
          </button>
        )}
      </div>
    </div>
  )
}
