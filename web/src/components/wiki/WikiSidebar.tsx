import { useMemo, useState } from 'react'
import type { DiscoveredSection, WikiCatalogEntry } from '../../api/wiki'
import { resolveGroupOrder } from '../../lib/groupOrder'

/** Left-rail thematic dir groups + Tools section + search. */

interface WikiSidebarProps {
  catalog: WikiCatalogEntry[]
  /**
   * Dynamic sections discovered by the broker — blueprint-declared and
   * article-derived. When provided, this drives the IA instead of the
   * catalog's `group` field so sections evolve with team content.
   * Absent when the backend endpoint is unavailable (test fallback /
   * non-markdown memory backend).
   */
  sections?: DiscoveredSection[]
  currentPath?: string | null
  onNavigate: (path: string) => void
  /** Optional audit-log opener — rendered as a footer link when provided. */
  onNavigateAudit?: () => void
}

// Sections first seen within this window render a "new" indicator. 7 days
// matches the roadmap copy — a fresh section is novel enough to draw the
// eye, but once a week later it settles into the steady-state IA.
const NEW_SECTION_WINDOW_MS = 7 * 24 * 60 * 60 * 1000

export default function WikiSidebar({
  catalog,
  sections,
  currentPath,
  onNavigate,
  onNavigateAudit,
}: WikiSidebarProps) {
  const [query, setQuery] = useState('')
  const [bannerSlug, setBannerSlug] = useState<string | null>(null)

  // When the broker ships sections, use them verbatim for the IA. Otherwise
  // fall back to the legacy catalog-grouping path so the sidebar still
  // renders against mocks / pre-v1.3 brokers.
  const usingSections = Array.isArray(sections) && sections.length > 0

  const groupedFromCatalog = useMemo(
    () => groupCatalog(catalog, query.trim()),
    [catalog, query],
  )
  const fallbackOrder = useMemo(
    () => resolveGroupOrder(catalog.map((c) => c.group)),
    [catalog],
  )

  const sectionList = useMemo(
    () => (usingSections ? applyQueryToSections(sections, catalog, query.trim()) : []),
    [usingSections, sections, catalog, query],
  )

  const newSectionSlugs = useMemo(() => {
    if (!usingSections) return new Set<string>()
    const cutoff = Date.now() - NEW_SECTION_WINDOW_MS
    const out = new Set<string>()
    for (const s of sections ?? []) {
      if (s.from_schema) continue
      const ts = Date.parse(s.first_seen_ts)
      if (!Number.isNaN(ts) && ts >= cutoff) out.add(s.slug)
    }
    return out
  }, [usingSections, sections])

  return (
    <aside className="wk-nav-sidebar">
      <input
        type="search"
        className="search"
        placeholder="Search wiki…"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
      />
      {bannerSlug && (
        <AddToBlueprintBanner
          slug={bannerSlug}
          onDismiss={() => setBannerSlug(null)}
        />
      )}
      <div className="wk-nav-sidebar-scroll">
        {usingSections
          ? sectionList.map((section) => (
              <SectionGroup
                key={section.slug}
                section={section}
                entries={section.entries}
                currentPath={currentPath}
                isNew={newSectionSlugs.has(section.slug)}
                onNavigate={onNavigate}
                onSectionHeaderClick={() => {
                  if (!section.from_schema) {
                    setBannerSlug(section.slug)
                  }
                }}
              />
            ))
          : fallbackOrder.map((group) => {
              const items = groupedFromCatalog[group]
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
      </div>
      {onNavigateAudit && (
        <div className="wk-sidebar-audit">
          <button
            type="button"
            className="wk-sidebar-audit-link"
            onClick={(e) => {
              e.preventDefault()
              onNavigateAudit()
            }}
          >
            View audit log →
          </button>
        </div>
      )}
    </aside>
  )
}

interface SectionWithEntries extends DiscoveredSection {
  entries: WikiCatalogEntry[]
}

interface SectionGroupProps {
  section: DiscoveredSection
  entries: WikiCatalogEntry[]
  currentPath?: string | null
  isNew: boolean
  onNavigate: (path: string) => void
  onSectionHeaderClick: () => void
}

function SectionGroup({
  section,
  entries,
  currentPath,
  isNew,
  onNavigate,
  onSectionHeaderClick,
}: SectionGroupProps) {
  return (
    <div className="wk-section-group" data-section-slug={section.slug}>
      <h3
        className={`wk-section-header wk-section-${section.from_schema ? 'schema' : 'discovered'}`}
        onClick={onSectionHeaderClick}
        title={
          section.from_schema
            ? 'Declared in your blueprint'
            : 'Discovered from articles your team has written'
        }
      >
        <span className="wk-section-title">{section.slug}</span>
        {!section.from_schema && (
          <span className="wk-section-marker" aria-label="Discovered section" />
        )}
        {isNew && (
          <span className="wk-section-new" aria-label="New section">
            new
          </span>
        )}
      </h3>
      <ul>
        {entries.length === 0 ? (
          <li className="wk-section-empty">
            <em>No articles yet</em>
          </li>
        ) : (
          entries.map((item) => (
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
          ))
        )}
      </ul>
    </div>
  )
}

interface AddToBlueprintBannerProps {
  slug: string
  onDismiss: () => void
}

function AddToBlueprintBanner({ slug, onDismiss }: AddToBlueprintBannerProps) {
  return (
    <div className="wk-section-banner" role="status" data-testid="section-banner">
      <div className="wk-section-banner-body">
        <strong>“{slug}”</strong> is a new section your team built organically.
        Add it to your blueprint to make it permanent.
      </div>
      <button
        type="button"
        className="wk-section-banner-dismiss"
        onClick={onDismiss}
        aria-label="Dismiss banner"
      >
        ×
      </button>
    </div>
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

/**
 * Bind each DiscoveredSection to the concrete catalog entries that live
 * under it, then filter by the search query. Runs O(sections * catalog)
 * but the catalog is small enough (hundreds of entries at worst) that
 * this stays ~1ms.
 */
function applyQueryToSections(
  sections: DiscoveredSection[],
  catalog: WikiCatalogEntry[],
  query: string,
): SectionWithEntries[] {
  const q = query.toLowerCase()
  const byGroup: Record<string, WikiCatalogEntry[]> = {}
  for (const entry of catalog) {
    if (!byGroup[entry.group]) byGroup[entry.group] = []
    byGroup[entry.group].push(entry)
  }
  const out: SectionWithEntries[] = []
  for (const section of sections) {
    const entries = (byGroup[section.slug] ?? []).filter((e) => {
      if (!q) return true
      return (
        e.title.toLowerCase().includes(q) || e.path.toLowerCase().includes(q)
      )
    })
    // Under search, hide sections that have no matching entry AND are
    // empty by design (blueprint-declared but no articles yet).
    if (q && entries.length === 0) continue
    out.push({ ...section, entries })
  }
  return out
}
