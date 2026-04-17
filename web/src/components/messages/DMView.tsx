import { useEffect, useRef } from 'react'
import { useMessages } from '../../hooks/useMessages'
import { useAgentStream } from '../../hooks/useAgentStream'
import { useAppStore } from '../../stores/app'
import { MessageBubble } from './MessageBubble'
import { Composer } from './Composer'
import { InterviewBar } from './InterviewBar'

export function DMView() {
  const currentChannel = useAppStore((s) => s.currentChannel)
  const dmAgentSlug = useAppStore((s) => s.dmAgentSlug)
  const exitDM = useAppStore((s) => s.exitDM)
  const { data: messages = [] } = useMessages(currentChannel)
  const { lines, connected } = useAgentStream(dmAgentSlug)
  const messagesRef = useRef<HTMLDivElement>(null)
  const streamRef = useRef<HTMLDivElement>(null)

  // Auto-scroll messages
  useEffect(() => {
    if (messagesRef.current) {
      messagesRef.current.scrollTop = messagesRef.current.scrollHeight
    }
  }, [messages.length])

  // Auto-scroll stream
  useEffect(() => {
    if (streamRef.current) {
      streamRef.current.scrollTop = streamRef.current.scrollHeight
    }
  }, [lines.length])

  return (
    <>
      {/* DM banner */}
      <div className="dm-banner active">
        <span>1:1 session with {dmAgentSlug}</span>
        <button className="dm-back-btn" onClick={exitDM}>
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <path d="M19 12H5" />
            <path d="m12 19-7-7 7-7" />
          </svg>
          Back to office
        </button>
      </div>

      {/* Split layout: messages left, live stream right */}
      <div style={{ flex: 1, display: 'flex', overflow: 'hidden' }}>
        {/* Left: Messages + Composer */}
        <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
          <div
            ref={messagesRef}
            className="messages"
          >
            {messages.map((msg) => (
              <MessageBubble key={msg.id} message={msg} />
            ))}
          </div>
          <InterviewBar />
          <Composer />
        </div>

        {/* Right: Live stream */}
        <div style={{
          width: 320,
          flexShrink: 0,
          borderLeft: '1px solid var(--border)',
          display: 'flex',
          flexDirection: 'column',
          overflow: 'hidden',
        }}>
          <div style={{
            padding: '8px 12px',
            borderBottom: '1px solid var(--border)',
            display: 'flex',
            alignItems: 'center',
            gap: 8,
            fontSize: 13,
            fontWeight: 600,
          }}>
            <span
              className={`status-dot ${connected ? 'active pulse' : 'lurking'}`}
            />
            <span>Live output</span>
          </div>
          <div
            ref={streamRef}
            style={{
              flex: 1,
              overflowY: 'auto',
              padding: 8,
              fontFamily: 'var(--font-mono)',
              fontSize: 11,
              lineHeight: 1.5,
              color: 'var(--text-secondary)',
            }}
          >
            {lines.length === 0 ? (
              <div style={{ color: 'var(--text-tertiary)', padding: 8 }}>
                {connected ? 'Waiting for output...' : 'Stream idle'}
              </div>
            ) : (
              lines.map((line) => (
                <div key={line.id} style={{ padding: '1px 0', wordBreak: 'break-all' }}>
                  {line.parsed?.content
                    ? String(line.parsed.content)
                    : line.data.length > 200
                      ? line.data.slice(0, 200) + '...'
                      : line.data}
                </div>
              ))
            )}
          </div>
        </div>
      </div>
    </>
  )
}
