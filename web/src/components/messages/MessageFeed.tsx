import { useEffect, useRef } from 'react'
import { useMessages } from '../../hooks/useMessages'
import { useAppStore } from '../../stores/app'
import { MessageBubble } from './MessageBubble'
import { formatDateLabel } from '../../lib/format'

function dateDayKey(ts: string): string {
  const d = new Date(ts)
  return `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`
}

export function MessageFeed() {
  const currentChannel = useAppStore((s) => s.currentChannel)
  const setActiveThreadId = useAppStore((s) => s.setActiveThreadId)
  const containerRef = useRef<HTMLDivElement>(null)
  const prevLengthRef = useRef(0)

  const { data: messages = [], isLoading } = useMessages(currentChannel)

  // Auto-scroll when new messages arrive
  useEffect(() => {
    if (messages.length > prevLengthRef.current && containerRef.current) {
      containerRef.current.scrollTop = containerRef.current.scrollHeight
    }
    prevLengthRef.current = messages.length
  }, [messages.length])

  if (isLoading && messages.length === 0) {
    return (
      <div className="messages" style={{ alignItems: 'center', justifyContent: 'center' }}>
        <span style={{ color: 'var(--text-tertiary)', fontSize: 13 }}>Loading messages...</span>
      </div>
    )
  }

  if (messages.length === 0) {
    return (
      <div className="messages">
        <div className="channel-empty-state">
          <span className="eyebrow">quiet before the standup</span>
          <span className="title">#{currentChannel} is empty. For now.</span>
          <span className="body">
            This is where your agents will argue, claim tasks, and show progress.
            Unlike Ryan Howard, they actually ship.
          </span>
          <div className="channel-empty-hints">
            <div>Try <code>@ceo what should we build this week?</code></div>
            <div>Type <code>/</code> for commands, <code>@</code> to mention an agent.</div>
          </div>
          <span className="channel-empty-foot">Michael would be proud. Probably.</span>
        </div>
      </div>
    )
  }

  // Build message list with date separators and grouping
  const elements: Array<{ type: 'date'; key: string; label: string } | { type: 'message'; key: string; message: typeof messages[0]; grouped: boolean }> = []
  let lastDate = ''
  let lastFrom = ''
  let lastTime = ''

  for (const msg of messages) {
    // Skip status messages from main feed grouping logic
    if (msg.content?.startsWith('[STATUS]')) continue
    // Skip reply messages in main feed
    if (msg.reply_to) continue

    // Date separator
    if (msg.timestamp) {
      const dayKey = dateDayKey(msg.timestamp)
      if (dayKey !== lastDate) {
        elements.push({ type: 'date', key: `date-${dayKey}`, label: formatDateLabel(msg.timestamp) })
        lastDate = dayKey
        lastFrom = ''
        lastTime = ''
      }
    }

    // Grouping: same sender within 5 minutes
    let grouped = false
    if (lastFrom === msg.from && msg.timestamp && lastTime) {
      const delta = new Date(msg.timestamp).getTime() - new Date(lastTime).getTime()
      if (delta >= 0 && delta < 5 * 60 * 1000) grouped = true
    }
    lastFrom = msg.from
    lastTime = msg.timestamp || lastTime

    elements.push({ type: 'message', key: msg.id, message: msg, grouped })
  }

  return (
    <div className="messages" ref={containerRef}>
      {elements.map((el) => {
        if (el.type === 'date') {
          return (
            <div key={el.key} className="date-separator">
              <div className="date-separator-line" />
              <span className="date-separator-text">{el.label}</span>
              <div className="date-separator-line" />
            </div>
          )
        }
        return (
          <MessageBubble
            key={el.key}
            message={el.message}
            grouped={el.grouped}
            onThreadClick={(id) => setActiveThreadId(id)}
          />
        )
      })}
    </div>
  )
}
