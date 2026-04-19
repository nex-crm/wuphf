import { useMemo, useState } from 'react'
import type { WikiCatalogEntry } from '../../api/wiki'
import { resolveGroupOrder } from '../../lib/groupOrder'

/** Left-rail thematic dir groups + Tools section + search. */

interface WikiSidebarProps {
  catalog: WikiCatalogEntry[]
  currentPath?: string | null
  onNavigate: (path: string) => void
}

export default function WikiSidebar({ catalog, currentPath, onNavigate }: WikiSidebarProps) {
  const [query, setQuery] = useState('')

  const grouped = useMemo(() => groupCatalog(catalog, query.trim()), [catalog, query])
  const groupOrder = useMemo(
    () => resolveGroupOrder(catalog.map((c) => c.group)),
    [catalog],
  )

  return (
    <aside className="wk-nav-sidebar">
      <input
        type="search"
        className="search"
        placeholder="Search wiki…"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
      />
      {groupOrder.map((group) => {
        const items = grouped[group]
        if (!items || items.length === 0) return null
        return (
          <div key={group}>
            <h3>{group}</h3>
            <ul>
              {items.map((item) => (
                <li key={item.path} className={currentPath === item.path ? 'current' : ''}>
                  <a
                    href={`#/wiki/${encodeURI(item.path)}`}
                    onClick={(e) => {
                      e.preventDefault()
                      onNavigate(item.path)
                    }}
                  >
                    {item.title}
                  </a>
                </li>
              ))}
            </ul>
          </div>
        )
      })}
    </aside>
  )
}

function groupCatalog(
  catalog: WikiCatalogEntry[],
  query: string,
): Record<string, WikiCatalogEntry[]> {
  const q = query.toLowerCase()
  const out: Record<string, WikiCatalogEntry[]> = {}
  for (const entry of catalog) {
    if (q && !entry.title.toLowerCase().includes(q) && !entry.path.toLowerCase().includes(q)) {
      continue
    }
    if (!out[entry.group]) out[entry.group] = []
    out[entry.group].push(entry)
  }
  return out
}
