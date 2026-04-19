import { useMemo } from 'react'
import PixelAvatar from './PixelAvatar'
import type { WikiCatalogEntry } from '../../api/wiki'
import { formatRelativeTime } from '../../lib/format'
import { resolveGroupOrder } from '../../lib/groupOrder'

/** `/wiki` landing view: grid of thematic dir groups with recent articles. */

interface WikiCatalogProps {
  catalog: WikiCatalogEntry[]
  onNavigate: (path: string) => void
  articlesCount?: number
  commitsCount?: number
  agentsCount?: number
}

export default function WikiCatalog({
  catalog,
  onNavigate,
  articlesCount,
  commitsCount,
  agentsCount,
}: WikiCatalogProps) {
  const grouped = useMemo(() => groupByGroup(catalog), [catalog])
  const groupOrder = useMemo(
    () => resolveGroupOrder(catalog.map((c) => c.group)),
    [catalog],
  )
  const stats = useMemo(
    () => [
      `${articlesCount ?? catalog.length} articles`,
      typeof commitsCount === 'number' ? `${commitsCount} commits` : null,
      typeof agentsCount === 'number' ? `${agentsCount} agents writing` : null,
    ].filter(Boolean).join(' · '),
    [catalog.length, articlesCount, commitsCount, agentsCount],
  )

  return (
    <main className="wk-catalog" data-testid="wk-catalog">
      <header className="wk-catalog-header">
        <h1 className="wk-catalog-title">Team Wiki</h1>
        <div className="wk-catalog-stats">{stats}</div>
        <div className="wk-catalog-clone">
          Your wiki lives on your disk.{' '}
          <code>git clone ~/.wuphf/wiki</code>
        </div>
      </header>
      <div className="wk-catalog-grid">
        {groupOrder.map((group) => {
          const items = grouped[group]
          if (!items || items.length === 0) return null
          return (
            <section key={group} className="wk-catalog-card">
              <h3>
                {group}
                <span className="wk-count">{items.length}</span>
              </h3>
              <ul>
                {items.slice(0, 6).map((item) => (
                  <li key={item.path}>
                    <PixelAvatar slug={item.author_slug} size={16} />
                    <span
                      className="wk-title"
                      role="link"
                      tabIndex={0}
                      onClick={() => onNavigate(item.path)}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter' || e.key === ' ') {
                          e.preventDefault()
                          onNavigate(item.path)
                        }
                      }}
                    >
                      {item.title}
                    </span>
                    <span className="wk-when">{safeRelative(item.last_edited_ts)}</span>
                  </li>
                ))}
              </ul>
            </section>
          )
        })}
      </div>
    </main>
  )
}

function groupByGroup(catalog: WikiCatalogEntry[]): Record<string, WikiCatalogEntry[]> {
  const out: Record<string, WikiCatalogEntry[]> = {}
  for (const entry of catalog) {
    if (!out[entry.group]) out[entry.group] = []
    out[entry.group].push(entry)
  }
  for (const k of Object.keys(out)) {
    out[k].sort((a, b) => (a.last_edited_ts < b.last_edited_ts ? 1 : -1))
  }
  return out
}

function safeRelative(iso: string): string {
  try {
    return formatRelativeTime(iso)
  } catch {
    return iso
  }
}
