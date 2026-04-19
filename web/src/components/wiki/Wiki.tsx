import { useEffect, useState } from 'react'
import { fetchCatalog, type WikiCatalogEntry } from '../../api/wiki'
import WikiSidebar from './WikiSidebar'
import WikiCatalog from './WikiCatalog'
import WikiArticle from './WikiArticle'
import EditLogFooter from './EditLogFooter'
import '../../styles/wiki.css'

interface WikiProps {
  /** When set, renders the article view for this path; otherwise renders the catalog. */
  articlePath?: string | null
  onNavigate: (path: string | null) => void
}

/** Three-column wiki shell: left sidebar · main (catalog or article) · right rail (article only). */
export default function Wiki({ articlePath, onNavigate }: WikiProps) {
  const [catalog, setCatalog] = useState<WikiCatalogEntry[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let cancelled = false
    fetchCatalog()
      .then((c) => {
        if (!cancelled) setCatalog(c)
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [])

  const view = articlePath ? 'article' : 'catalog'

  return (
    <div className="wiki-root" data-testid="wiki-root">
      <div className="wiki-layout" data-view={view}>
        <WikiSidebar
          catalog={catalog}
          currentPath={articlePath}
          onNavigate={(path) => onNavigate(path)}
        />
        {articlePath ? (
          <WikiArticle
            path={articlePath}
            catalog={catalog}
            onNavigate={(path) => onNavigate(path)}
          />
        ) : (
          <WikiCatalog catalog={catalog} onNavigate={(path) => onNavigate(path)} />
        )}
      </div>
      {!loading && <EditLogFooter onNavigate={(path) => onNavigate(path)} />}
    </div>
  )
}
