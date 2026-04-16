import { SIDEBAR_APPS } from '../../lib/constants'
import { useAppStore } from '../../stores/app'

export function AppList() {
  const currentApp = useAppStore((s) => s.currentApp)
  const setCurrentApp = useAppStore((s) => s.setCurrentApp)

  return (
    <div className="sidebar-apps">
      {SIDEBAR_APPS.map((app) => (
        <button
          key={app.id}
          className={`sidebar-item${currentApp === app.id ? ' active' : ''}`}
          onClick={() => setCurrentApp(app.id)}
        >
          <span className="sidebar-item-emoji">{app.icon}</span>
          <span>{app.name}</span>
        </button>
      ))}
    </div>
  )
}
