import { useQuery } from '@tanstack/react-query'
import { SIDEBAR_APPS } from '../../lib/constants'
import { useAppStore } from '../../stores/app'
import { getRequests } from '../../api/client'

export function AppList() {
  const currentApp = useAppStore((s) => s.currentApp)
  const setCurrentApp = useAppStore((s) => s.setCurrentApp)
  const currentChannel = useAppStore((s) => s.currentChannel)

  const { data: requestsData } = useQuery({
    queryKey: ['requests-badge', currentChannel],
    queryFn: () => getRequests(currentChannel),
    refetchInterval: 5_000,
  })

  const pendingCount = (requestsData?.requests ?? []).filter(
    (r) => !r.status || r.status === 'open' || r.status === 'pending',
  ).length

  return (
    <div className="sidebar-apps">
      {SIDEBAR_APPS.map((app) => {
        const badge = app.id === 'requests' && pendingCount > 0 ? pendingCount : null
        return (
          <button
            key={app.id}
            className={`sidebar-item${currentApp === app.id ? ' active' : ''}`}
            onClick={() => setCurrentApp(app.id)}
          >
            <span className="sidebar-item-emoji">{app.icon}</span>
            <span style={{ flex: 1 }}>{app.name}</span>
            {badge !== null && (
              <span className="sidebar-badge" aria-label={`${badge} pending`}>
                {badge}
              </span>
            )}
          </button>
        )
      })}
    </div>
  )
}
