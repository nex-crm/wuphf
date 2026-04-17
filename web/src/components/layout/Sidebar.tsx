import { useAppStore } from '../../stores/app'
import { AgentList } from '../sidebar/AgentList'
import { ChannelList } from '../sidebar/ChannelList'
import { AppList } from '../sidebar/AppList'
import { UsagePanel } from '../sidebar/UsagePanel'
import { WorkspaceSummary } from '../sidebar/WorkspaceSummary'
import type { Theme } from '../../stores/app'

export function Sidebar() {
  const theme = useAppStore((s) => s.theme)
  const setTheme = useAppStore((s) => s.setTheme)
  const sidebarAgentsOpen = useAppStore((s) => s.sidebarAgentsOpen)
  const toggleSidebarAgents = useAppStore((s) => s.toggleSidebarAgents)

  return (
    <aside className="sidebar">
      <div className="sidebar-header">
        <span className="sidebar-logo">WUPHF</span>
      </div>

      {/* Team / Agents section */}
      <div className="sidebar-section">
        <p className="sidebar-section-title">Team</p>
        <button
          className="sidebar-item active"
          onClick={toggleSidebarAgents}
        >
          <svg className="sidebar-item-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2" />
            <circle cx="9" cy="7" r="4" />
            <path d="M22 21v-2a4 4 0 0 0-3-3.87" />
            <path d="M16 3.13a4 4 0 0 1 0 7.75" />
          </svg>
          <span>Agents</span>
          <svg
            style={{
              marginLeft: 'auto',
              width: 12,
              height: 12,
              transform: sidebarAgentsOpen ? 'rotate(90deg)' : 'rotate(0deg)',
              transition: 'transform 0.15s',
            }}
            viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"
          >
            <path d="m9 18 6-6-6-6" />
          </svg>
        </button>
      </div>

      {sidebarAgentsOpen && <AgentList />}

      {/* Channels section */}
      <div className="sidebar-section" style={{ marginTop: 4, borderTop: '1px solid var(--border)', paddingTop: 8 }}>
        <p className="sidebar-section-title">Channels</p>
      </div>
      <ChannelList />

      {/* Apps section */}
      <div className="sidebar-section" style={{ marginTop: 4, borderTop: '1px solid var(--border)', paddingTop: 8 }}>
        <p className="sidebar-section-title">Apps</p>
      </div>
      <AppList />

      {/* Workspace summary */}
      <WorkspaceSummary />

      {/* Usage */}
      <UsagePanel />

      {/* Theme switcher */}
      <div className="sidebar-bottom">
        <label className="sr-only" htmlFor="theme-switcher">Switch theme</label>
        <select
          id="theme-switcher"
          aria-label="Switch theme"
          value={theme}
          onChange={(e) => setTheme(e.target.value as Theme)}
          style={{
            width: '100%',
            padding: '4px 6px',
            fontSize: 11,
            fontFamily: 'var(--font-sans)',
            border: '1px solid var(--border)',
            borderRadius: 4,
            background: 'var(--bg-card)',
            color: 'var(--text)',
            cursor: 'pointer',
          }}
        >
          <option value="nex">Nex</option>
          <option value="slack">Slack</option>
          <option value="slack-dark">Slack (Dark)</option>
          <option value="windows-98">Windows 98</option>
        </select>
      </div>
    </aside>
  )
}
