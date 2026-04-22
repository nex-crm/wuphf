import { useQuery } from '@tanstack/react-query'
import type { ComponentType } from 'react'
import { useOverflow } from '../../hooks/useOverflow'
import {
  Play,
  CheckCircle,
  ClipboardCheck,
  Shield,
  Calendar,
  Flash,
  Package,
  Page,
  Search,
  Settings,
  BookStack,
  ShareAndroid,
} from 'iconoir-react'
import { SIDEBAR_APPS } from '../../lib/constants'
import { useAppStore } from '../../stores/app'
import { getRequests } from '../../api/client'
import { fetchReviews } from '../../api/notebook'

// Notebooks and reviews render inside the Wiki app shell via tabs, so the
// 'Wiki' sidebar entry lights up for any of those three currentApp values.
const WIKI_SURFACE_APPS = new Set(['wiki', 'notebooks', 'reviews'])

const APP_ICONS: Record<string, ComponentType<{ className?: string }>> = {
  studio: Play,
  wiki: BookStack,
  tasks: CheckCircle,
  requests: ClipboardCheck,
  graph: ShareAndroid,
  policies: Shield,
  calendar: Calendar,
  skills: Flash,
  activity: Package,
  receipts: Page,
  'health-check': Search,
  settings: Settings,
}

export function AppList() {
  const currentApp = useAppStore((s) => s.currentApp)
  const setCurrentApp = useAppStore((s) => s.setCurrentApp)
  const currentChannel = useAppStore((s) => s.currentChannel)

  const { data: requestsData } = useQuery({
    queryKey: ['requests-badge', currentChannel],
    queryFn: () => getRequests(currentChannel),
    refetchInterval: 5_000,
  })

  const { data: reviewsData } = useQuery({
    queryKey: ['reviews-badge'],
    queryFn: fetchReviews,
    refetchInterval: 15_000,
  })

  const pendingCount = (requestsData?.requests ?? []).filter(
    (r) => !r.status || r.status === 'open' || r.status === 'pending',
  ).length

  const pendingReviewsCount = (reviewsData ?? []).filter(
    (r) => r.state === 'pending' || r.state === 'in-review' || r.state === 'changes-requested',
  ).length

  const overflowRef = useOverflow<HTMLDivElement>()

  return (
    <div className="sidebar-scroll-wrap is-apps">
      <div className="sidebar-apps" ref={overflowRef}>
        {SIDEBAR_APPS.filter((app) => app.id !== 'settings').map((app) => {
          let badge: number | null = null
          if (app.id === 'requests' && pendingCount > 0) badge = pendingCount
          if (app.id === 'wiki' && pendingReviewsCount > 0) badge = pendingReviewsCount
          const Icon = APP_ICONS[app.id]
          const isActive =
            app.id === 'wiki'
              ? WIKI_SURFACE_APPS.has(currentApp ?? '')
              : currentApp === app.id
          return (
            <button
              key={app.id}
              className={`sidebar-item${isActive ? ' active' : ''}`}
              onClick={() => setCurrentApp(app.id)}
            >
              {Icon ? (
                <Icon className="sidebar-item-icon" />
              ) : (
                <span className="sidebar-item-emoji">{app.icon}</span>
              )}
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
    </div>
  )
}
