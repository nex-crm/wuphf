import { useEffect, useRef, useState, type ComponentType, type MouseEvent } from 'react'
import { createPortal } from 'react-dom'
import {
  Settings as SettingsIcon,
  SidebarExpand,
  Group,
  ChatBubble,
  Play,
  CheckCircle,
  ClipboardCheck,
  Shield,
  Calendar,
  Flash,
  Package,
  Page,
  Search,
  BookStack,
  Activity,
} from 'iconoir-react'
import { useAppStore } from '../../stores/app'
import { SIDEBAR_APPS } from '../../lib/constants'
import { useQuery } from '@tanstack/react-query'
import { AgentList } from '../sidebar/AgentList'
import { ChannelList } from '../sidebar/ChannelList'
import { getUsage } from '../../api/client'
import { formatUSD, formatTokens } from '../../lib/format'

const APP_ICONS: Record<string, ComponentType<{ className?: string }>> = {
  studio: Play,
  wiki: BookStack,
  tasks: CheckCircle,
  requests: ClipboardCheck,
  policies: Shield,
  calendar: Calendar,
  skills: Flash,
  activity: Package,
  receipts: Page,
  'health-check': Search,
  settings: SettingsIcon,
}

type Popover = 'team' | 'channels' | 'usage' | null
type HintState = { label: string; y: number } | null

export function CollapsedSidebar() {
  const toggleCollapsed = useAppStore((s) => s.toggleSidebarCollapsed)
  const currentApp = useAppStore((s) => s.currentApp)
  const setCurrentApp = useAppStore((s) => s.setCurrentApp)
  const [popover, setPopover] = useState<Popover>(null)
  const [hint, setHint] = useState<HintState>(null)
  const popoverRef = useRef<HTMLDivElement>(null)
  const closeTimer = useRef<number | null>(null)

  function openPopover(p: Popover) {
    if (closeTimer.current) { window.clearTimeout(closeTimer.current); closeTimer.current = null }
    setHint(null)
    setPopover(p)
  }
  function scheduleClose() {
    if (closeTimer.current) window.clearTimeout(closeTimer.current)
    closeTimer.current = window.setTimeout(() => setPopover(null), 120)
  }
  function showHint(e: MouseEvent<HTMLElement>, label: string) {
    const r = e.currentTarget.getBoundingClientRect()
    setHint({ label, y: r.top + r.height / 2 })
  }
  function hideHint() { setHint(null) }

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') { setPopover(null); setHint(null) }
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [])

  useEffect(() => {
    return () => {
      if (closeTimer.current) {
        window.clearTimeout(closeTimer.current)
        closeTimer.current = null
      }
    }
  }, [])

  return (
    <>
      <div className="sidebar-rail-top">
        <button
          type="button"
          className="sidebar-icon-btn"
          aria-label="Expand sidebar"
          onClick={toggleCollapsed}
          onMouseEnter={(e) => showHint(e, 'Expand sidebar')}
          onMouseLeave={hideHint}
        >
          <SidebarExpand />
        </button>
        <button
          type="button"
          className={`sidebar-icon-btn${currentApp === 'settings' ? ' active' : ''}`}
          aria-label="Settings"
          onClick={() => setCurrentApp('settings')}
          onMouseEnter={(e) => showHint(e, 'Settings')}
          onMouseLeave={hideHint}
        >
          <SettingsIcon />
        </button>
      </div>

      <div className="sidebar-rail-middle">
        <button
          type="button"
          className={`sidebar-icon-btn${popover === 'team' ? ' is-open' : ''}`}
          aria-label="Team"
          aria-haspopup="dialog"
          aria-expanded={popover === 'team'}
          onMouseEnter={() => openPopover('team')}
          onMouseLeave={scheduleClose}
          onFocus={() => openPopover('team')}
          onBlur={scheduleClose}
        >
          <Group />
        </button>
        <button
          type="button"
          className={`sidebar-icon-btn${popover === 'channels' ? ' is-open' : ''}`}
          aria-label="Channels"
          aria-haspopup="dialog"
          aria-expanded={popover === 'channels'}
          onMouseEnter={() => openPopover('channels')}
          onMouseLeave={scheduleClose}
          onFocus={() => openPopover('channels')}
          onBlur={scheduleClose}
        >
          <ChatBubble />
        </button>
      </div>

      <div className="sidebar-rail-apps">
        {SIDEBAR_APPS.filter((a) => a.id !== 'settings').map((app) => {
          const Icon = APP_ICONS[app.id]
          return (
            <button
              key={app.id}
              type="button"
              className={`sidebar-icon-btn${currentApp === app.id ? ' active' : ''}`}
              aria-label={app.name}
              onClick={() => setCurrentApp(app.id)}
              onMouseEnter={(e) => showHint(e, app.name)}
              onMouseLeave={hideHint}
            >
              {Icon ? <Icon /> : <span className="sidebar-item-emoji">{app.icon}</span>}
            </button>
          )
        })}
      </div>

      <UsageRail onEnter={() => openPopover('usage')} onLeave={scheduleClose} active={popover === 'usage'} />

      {popover && createPortal(
        <div
          ref={popoverRef}
          className={`sidebar-rail-popover sidebar-rail-popover-${popover}`}
          role="dialog"
          onMouseEnter={() => openPopover(popover)}
          onMouseLeave={scheduleClose}
        >
          <div className="sidebar-rail-popover-title">
            {popover === 'team' ? 'Team' : popover === 'channels' ? 'Channels' : 'Usage'}
          </div>
          <div className="sidebar-rail-popover-body">
            {popover === 'team' && <AgentList />}
            {popover === 'channels' && <ChannelList />}
            {popover === 'usage' && <UsageBody />}
          </div>
        </div>,
        document.body,
      )}

      {hint && createPortal(
        <div className="sidebar-rail-hint" style={{ top: hint.y }} role="tooltip">
          {hint.label}
        </div>,
        document.body,
      )}
    </>
  )
}

function formatCompactUSD(v: number): string {
  if (v >= 1000) return `$${(v / 1000).toFixed(1)}k`
  if (v >= 100) return `$${v.toFixed(0)}`
  if (v >= 10) return `$${v.toFixed(1)}`
  return `$${v.toFixed(2)}`
}

function UsageRail({ onEnter, onLeave, active }: { onEnter: () => void; onLeave: () => void; active: boolean }) {
  const { data: usage } = useQuery({
    queryKey: ['usage'],
    queryFn: () => getUsage(),
    refetchInterval: 30_000,
  })
  const totalCost = usage?.total?.cost_usd ?? 0
  return (
    <button
      type="button"
      className={`sidebar-rail-bottom${active ? ' is-open' : ''}`}
      aria-label={`Usage ${formatUSD(totalCost)}`}
      aria-haspopup="dialog"
      aria-expanded={active}
      onMouseEnter={onEnter}
      onMouseLeave={onLeave}
      onFocus={onEnter}
      onBlur={onLeave}
      title={`Usage ${formatUSD(totalCost)}`}
    >
      <Activity className="sidebar-rail-usage-icon" />
      <span className="sidebar-rail-usage-value">{formatCompactUSD(totalCost)}</span>
    </button>
  )
}

function UsageBody() {
  const { data: usage } = useQuery({ queryKey: ['usage'], queryFn: () => getUsage(), refetchInterval: 5000 })
  const totalCost = usage?.total?.cost_usd ?? 0
  const agents = usage?.agents ?? {}
  const slugs = Object.keys(agents).sort()
  if (slugs.length === 0 && totalCost === 0) {
    return (
      <p style={{ fontSize: 11, color: 'var(--text-tertiary)', padding: '8px 14px' }}>
        No usage recorded yet.
      </p>
    )
  }
  return (
    <div className="sidebar-rail-usage-panel">
      <table className="usage-table">
        <thead>
          <tr>
            {['Agent', 'In', 'Out', 'Cache', 'Cost'].map((h) => (
              <th key={h}>{h}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {slugs.map((slug) => {
            const a = agents[slug]
            return (
              <tr key={slug}>
                <td>{slug}</td>
                <td>{formatTokens(a.input_tokens)}</td>
                <td>{formatTokens(a.output_tokens)}</td>
                <td>{formatTokens(a.cache_read_tokens)}</td>
                <td>{formatUSD(a.cost_usd)}</td>
              </tr>
            )
          })}
        </tbody>
      </table>
      <div className="usage-total">
        <span>Session: {formatTokens(usage?.session?.total_tokens ?? 0)} tokens</span>
        <span className="usage-total-cost">{formatUSD(totalCost)}</span>
      </div>
    </div>
  )
}
