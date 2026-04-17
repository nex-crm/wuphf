import { Settings as SettingsIcon } from 'iconoir-react'
import { useAppStore } from '../../stores/app'
import { AgentList } from '../sidebar/AgentList'
import { ChannelList } from '../sidebar/ChannelList'
import { AppList } from '../sidebar/AppList'
import { UsagePanel } from '../sidebar/UsagePanel'
import { WorkspaceSummary } from '../sidebar/WorkspaceSummary'

export function Sidebar() {
  const sidebarAgentsOpen = useAppStore((s) => s.sidebarAgentsOpen)
  const toggleSidebarAgents = useAppStore((s) => s.toggleSidebarAgents)
  const currentApp = useAppStore((s) => s.currentApp)
  const setCurrentApp = useAppStore((s) => s.setCurrentApp)

  return (
    <aside className="sidebar">
      <div className="sidebar-header">
        <span className="sidebar-logo">WUPHF</span>
        <div className="sidebar-header-actions">
          <button
            type="button"
            className={`sidebar-icon-btn${currentApp === 'settings' ? ' active' : ''}`}
            aria-label="Open settings"
            title="Settings"
            onClick={() => setCurrentApp('settings')}
          >
            <SettingsIcon />
          </button>
        </div>
      </div>

      {/* Team / Agents section */}
      <div className={`sidebar-section is-team${sidebarAgentsOpen ? '' : ' is-collapsed'}`}>
        <button
          type="button"
          className="sidebar-section-title sidebar-section-toggle"
          onClick={toggleSidebarAgents}
          aria-expanded={sidebarAgentsOpen}
        >
          <span>Team</span>
          <svg
            style={{
              width: 10,
              height: 10,
              transform: sidebarAgentsOpen ? 'rotate(90deg)' : 'rotate(0deg)',
              transition: 'transform 0.15s',
            }}
            viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"
          >
            <path d="m9 18 6-6-6-6" />
          </svg>
        </button>
      </div>

      <div className={`sidebar-collapsible${sidebarAgentsOpen ? ' is-open' : ''}`}>
        <AgentList />
      </div>

      {/* Channels section */}
      <div className="sidebar-section">
        <p className="sidebar-section-title">Channels</p>
      </div>
      <ChannelList />

      {/* Apps section */}
      <div className="sidebar-section">
        <p className="sidebar-section-title">Apps</p>
      </div>
      <AppList />

      {/* Workspace summary */}
      <WorkspaceSummary />

      {/* Usage */}
      <UsagePanel />
    </aside>
  )
}
