import { useEffect, useRef, useState } from 'react'
import { useAppStore } from '../../stores/app'
import { useOfficeMembers } from '../../hooks/useMembers'
import { useAgentStream } from '../../hooks/useAgentStream'
import { createDM, getAgentLogs } from '../../api/client'
import { PixelAvatar } from '../ui/PixelAvatar'
import { showNotice } from '../ui/Toast'
import { StreamLineView } from '../messages/StreamLineView'
import type { AgentLog, OfficeMember } from '../../api/client'

interface AgentPanelViewProps {
  agent: OfficeMember
  onClose: () => void
}

function StreamSection({ slug }: { slug: string }) {
  const { lines, connected } = useAgentStream(slug)
  const scrollRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const el = scrollRef.current
    if (el) {
      el.scrollTop = el.scrollHeight
    }
  }, [lines])

  return (
    <div className="agent-panel-section">
      <div className="agent-panel-section-title">Live stream</div>
      <div className="agent-stream-status">
        <span className={`status-dot ${connected ? 'active pulse' : 'lurking'}`} />
        {connected ? 'Connected' : 'Disconnected'}
      </div>
      <div className="agent-stream-log" ref={scrollRef}>
        {lines.length === 0 ? (
          <div className="agent-stream-empty">No output yet</div>
        ) : (
          lines.map((line) => (
            <StreamLineView key={line.id} line={line} compact />
          ))
        )}
      </div>
    </div>
  )
}

function LogsSection({ slug }: { slug: string }) {
  const [logs, setLogs] = useState<AgentLog[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let cancelled = false
    setLoading(true)

    getAgentLogs({ limit: 10 })
      .then((data) => {
        if (!cancelled) {
          const agentLogs = data.logs.filter((l) => l.agent === slug)
          setLogs(agentLogs.slice(0, 10))
          setLoading(false)
        }
      })
      .catch(() => {
        if (!cancelled) setLoading(false)
      })

    return () => { cancelled = true }
  }, [slug])

  function formatTime(timestamp: string | undefined): string {
    if (!timestamp) return ''
    try {
      const d = new Date(timestamp)
      return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
    } catch {
      return ''
    }
  }

  return (
    <div className="agent-panel-logs">
      <div className="agent-panel-section">
        <div className="agent-panel-section-title">Recent activity</div>
      </div>
      {loading ? (
        <div className="agent-log-empty">Loading...</div>
      ) : logs.length === 0 ? (
        <div className="agent-log-empty">No recent activity</div>
      ) : (
        logs.map((log) => (
          <div key={log.id} className="agent-log-item">
            {log.action && <div className="agent-log-action">{log.action}</div>}
            {log.content && <div className="agent-log-content">{log.content}</div>}
            <div className="agent-log-time">{formatTime(log.timestamp)}</div>
          </div>
        ))
      )}
    </div>
  )
}

function AgentPanelView({ agent, onClose }: AgentPanelViewProps) {
  const enterDM = useAppStore((s) => s.enterDM)
  const setActiveAgentSlug = useAppStore((s) => s.setActiveAgentSlug)
  const [dmLoading, setDmLoading] = useState(false)
  const [view, setView] = useState<'stream' | 'logs'>('stream')

  async function handleOpenDM() {
    setDmLoading(true)
    try {
      const result = await createDM(agent.slug)
      const channel = (result as { channel?: { slug?: string } })?.channel?.slug
        ?? `dm-human-${agent.slug}`
      enterDM(agent.slug, channel)
      setActiveAgentSlug(null)
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : 'Failed to open DM'
      showNotice(message, 'error')
    } finally {
      setDmLoading(false)
    }
  }

  const statusClass = agent.status === 'active' ? 'active pulse' : 'lurking'

  return (
    <div className="agent-panel">
      {/* Header */}
      <div className="agent-panel-header">
        <div className="agent-panel-identity">
          <div className="agent-panel-avatar">
            <PixelAvatar
              slug={agent.slug}
              size={36}
              className="pixel-avatar-panel"
            />
          </div>
          <div style={{ minWidth: 0, flex: 1, display: 'flex', flexDirection: 'column', gap: 2 }}>
            <div style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
              <span className="agent-panel-name">{agent.name || agent.slug}</span>
              <span className={`status-dot ${statusClass}`} style={{ marginLeft: -2 }} />
            </div>
            {agent.role && (
              <span className="agent-panel-role">{agent.role}</span>
            )}
          </div>
        </div>
        <button
          className="agent-panel-close"
          onClick={onClose}
          aria-label="Close agent panel"
        >
          <svg width="14" height="14" viewBox="0 0 14 14" fill="none">
            <path d="M1 1l12 12M13 1L1 13" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
          </svg>
        </button>
      </div>

      {/* Info */}
      <div className="agent-panel-section">
        <div className="agent-panel-info">
          <div className="agent-panel-info-row">
            <span className="agent-panel-info-label">slug</span>
            <span className="agent-panel-info-value">{agent.slug}</span>
          </div>
          {(() => {
            const p = agent.provider
            const label = typeof p === 'string' ? p : p?.kind
            return label ? (
              <div className="agent-panel-info-row">
                <span className="agent-panel-info-label">provider</span>
                <span className="agent-panel-info-value">{label}</span>
              </div>
            ) : null
          })()}
          {agent.status && (
            <div className="agent-panel-info-row">
              <span className="agent-panel-info-label">status</span>
              <span className="agent-panel-info-value">{agent.status}</span>
            </div>
          )}
          {agent.task && (
            <div className="agent-panel-info-row">
              <span className="agent-panel-info-label">task</span>
              <span className="agent-panel-info-value">{agent.task}</span>
            </div>
          )}
        </div>
      </div>

      {/* Actions */}
      <div className="agent-panel-actions">
        <button
          className="btn btn-primary btn-sm"
          onClick={handleOpenDM}
          disabled={dmLoading}
        >
          {dmLoading ? 'Opening...' : 'Open DM'}
        </button>
        <button
          className="btn btn-ghost btn-sm"
          onClick={() => setView(view === 'logs' ? 'stream' : 'logs')}
        >
          {view === 'logs' ? 'Live stream' : 'View logs'}
        </button>
      </div>

      {/* Stream or Logs */}
      {view === 'stream' ? (
        <StreamSection slug={agent.slug} />
      ) : (
        <LogsSection slug={agent.slug} />
      )}
    </div>
  )
}

export function AgentPanel() {
  const activeAgentSlug = useAppStore((s) => s.activeAgentSlug)
  const setActiveAgentSlug = useAppStore((s) => s.setActiveAgentSlug)
  const { data: members = [] } = useOfficeMembers()

  if (!activeAgentSlug) return null

  const agent = members.find((m) => m.slug === activeAgentSlug)
  if (!agent) return null

  return (
    <AgentPanelView
      agent={agent}
      onClose={() => setActiveAgentSlug(null)}
    />
  )
}
