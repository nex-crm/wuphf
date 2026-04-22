import { useMemo } from 'react'
import type { Message } from '../../api/client'
import { formatTime, formatTokens } from '../../lib/format'
import { formatMarkdown } from '../../lib/markdown'
import { renderMentions } from '../../lib/mentions'
import { useAppStore } from '../../stores/app'
import { toggleReaction } from '../../api/client'
import { useOfficeMembers } from '../../hooks/useMembers'
import { PixelAvatar } from '../ui/PixelAvatar'
import { HarnessBadge } from '../ui/HarnessBadge'
import { showNotice } from '../ui/Toast'
import { useDefaultHarness } from '../../hooks/useConfig'
import { resolveHarness } from '../../lib/harness'

interface MessageBubbleProps {
  message: Message
  grouped?: boolean
  /** Direct reply to a top-level channel message — renders indented under the parent. */
  isReply?: boolean
  /** Count of direct replies to this message. Shows an "N replies" affordance. */
  replyCount?: number
  /** Open the thread panel for this message. Shown as a hover action when provided. */
  onOpenThread?: (id: string) => void
  /** Reply-to-this-reply inside the thread panel. Shown as a hover action when provided. */
  onQuoteReply?: (message: Message) => void
  /** Copy a permalink to this message. Shown as a hover action when provided. */
  onCopyLink?: (id: string) => void
}

export function MessageBubble({
  message,
  grouped = false,
  isReply = false,
  replyCount = 0,
  onOpenThread,
  onQuoteReply,
  onCopyLink,
}: MessageBubbleProps) {
  const currentChannel = useAppStore((s) => s.currentChannel)
  const { data: members = [] } = useOfficeMembers()
  const isHuman = message.from === 'you' || message.from === 'human'
  const agent = members.find((m) => m.slug === message.from)
  const defaultHarness = useDefaultHarness()
  const harness = !isHuman ? resolveHarness(agent?.provider, defaultHarness) : null

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
  // Only trusted broker messages use this path — human input renders via the
  // safe renderMentions path below (builds ReactNode children, no innerHTML).
  const renderedHtml = !isHuman ? formatMarkdown(message.content || '') : ''

  // Turn human text like "@pm when are you free?" into mention chips for
  // registered agent slugs. Non-agent @-references stay plain text. The
  // memo keys on content + the slug list so rapid renders don't re-parse.
  const knownSlugs = useMemo(() => members.map((m) => m.slug), [members])
  const humanRendered = useMemo(
    () => (isHuman ? renderMentions(message.content || '', knownSlugs) : null),
    [isHuman, message.content, knownSlugs],
  )

  return (
    <div
      className={`message animate-fade${grouped ? ' message-grouped' : ''}${isReply ? ' message-reply' : ''}`}
      data-msg-id={message.id}
    >
      {/* Avatar */}
      <div
        className={`message-avatar${isHuman ? '' : ' avatar-with-harness'}`}
        style={isHuman ? { background: 'var(--bg-warm)', color: 'var(--text-secondary)', fontSize: 12, fontWeight: 600 } : undefined}
      >
        {isHuman ? (
          'You'
        ) : (
          <>
            <PixelAvatar slug={message.from} size={24} />
            {harness && (
              <HarnessBadge kind={harness} size={14} className="harness-badge-on-avatar" />
            )}
          </>
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
          <span className="message-time" title={message.timestamp}>{formatTime(message.timestamp)}</span>
          {usageTotal > 0 && (
            <span className="message-token-badge">{formatTokens(usageTotal)} tok</span>
          )}
        </div>

        {/* Text — humans render mention chips via safe ReactNode children;
            agent messages use the formatMarkdown path. */}
        {isHuman ? (
          <div className="message-text">{humanRendered}</div>
        ) : (
          <div className="message-text" dangerouslySetInnerHTML={{ __html: renderedHtml }} />
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

        {/* Thread summary — shown under a parent that has replies. Clicking
            opens the thread panel where the full chain is browsable. */}
        {replyCount > 0 && onOpenThread && (
          <button
            className="inline-thread-toggle"
            onClick={() => onOpenThread(message.id)}
            title="Open thread"
          >
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" />
            </svg>
            {replyCount} {replyCount === 1 ? 'reply' : 'replies'}
          </button>
        )}
      </div>

      {/* Hover actions — reply in thread, quote, copy link. Absolutely
          positioned so they don't change the bubble's flow layout. */}
      {(onOpenThread || onQuoteReply || onCopyLink) && (
        <div className="message-hover-actions" role="toolbar" aria-label="Message actions">
          {onOpenThread && (
            <button
              className="message-hover-btn"
              onClick={() => onOpenThread(message.id)}
              title="Reply in thread"
              aria-label="Reply in thread"
            >
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" />
              </svg>
            </button>
          )}
          {onQuoteReply && (
            <button
              className="message-hover-btn"
              onClick={() => onQuoteReply(message)}
              title="Quote-reply"
              aria-label="Quote-reply"
            >
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <path d="M3 21v-5a5 5 0 0 1 5-5h13" />
                <path d="m16 16-5-5 5-5" />
              </svg>
            </button>
          )}
          {onCopyLink && (
            <button
              className="message-hover-btn"
              onClick={() => onCopyLink(message.id)}
              title="Copy link"
              aria-label="Copy link"
            >
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71" />
                <path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.72-1.71" />
              </svg>
            </button>
          )}
        </div>
      )}
    </div>
  )
}
