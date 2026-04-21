import { useEffect, useState } from 'react'
import {
  fetchCatalog,
  fetchSections,
  subscribeSectionsUpdated,
  type DiscoveredSection,
  type WikiCatalogEntry,
} from '../../api/wiki'
import WikiSidebar from './WikiSidebar'
import WikiCatalog from './WikiCatalog'
import WikiArticle from './WikiArticle'
import WikiAudit from './WikiAudit'
import EditLogFooter from './EditLogFooter'
import '../../styles/wiki.css'

// Reserved pseudo-path for the audit view. Never collides with a real
// article because real articles must live under `team/` and end in `.md`.
const AUDIT_PATH = '_audit'

interface WikiProps {
  /** When set, renders the article view for this path; otherwise renders the catalog. */
  articlePath?: string | null
  onNavigate: (path: string | null) => void
}

/** Three-column wiki shell: left sidebar · main (catalog or article) · right rail (article only). */
export default function Wiki({ articlePath, onNavigate }: WikiProps) {
  const [catalog, setCatalog] = useState<WikiCatalogEntry[]>([])
  const [sections, setSections] = useState<DiscoveredSection[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let cancelled = false
    // Parallel fetch: catalog and sections are independent so we pay one
    // round-trip of latency, not two.
    Promise.all([fetchCatalog(), fetchSections()])
      .then(([c, s]) => {
        if (cancelled) return
        setCatalog(c)
        setSections(s)
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [])

  // Live-update sections when the broker emits wiki:sections_updated.
  // The event payload carries the full list so no refetch is needed.
  useEffect(() => {
    const unsubscribe = subscribeSectionsUpdated((event) => {
      if (Array.isArray(event.sections)) {
        setSections(event.sections)
      }
    })
    return () => unsubscribe()
  }, [])

  const isAudit = articlePath === AUDIT_PATH
  const view = isAudit ? 'audit' : articlePath ? 'article' : 'catalog'

  return (
    <div className="wiki-root" data-testid="wiki-root">
      <div className="wiki-layout" data-view={view}>
        <WikiSidebar
          catalog={catalog}
          sections={sections}
          currentPath={isAudit ? null : articlePath}
          onNavigate={(path) => onNavigate(path)}
          onNavigateAudit={() => onNavigate(AUDIT_PATH)}
        />
        {isAudit ? (
          <WikiAudit onNavigate={(path) => onNavigate(path)} />
        ) : articlePath ? (
          <WikiArticle
            path={articlePath}
            catalog={catalog}
            onNavigate={(path) => onNavigate(path)}
          />
        ) : (
          <WikiCatalog
            catalog={catalog}
            onNavigate={(path) => onNavigate(path)}
            onOpenAudit={() => onNavigate(AUDIT_PATH)}
          />
        )}
      </div>
      {!loading && <EditLogFooter onNavigate={(path) => onNavigate(path)} />}
    </div>
  )
}
