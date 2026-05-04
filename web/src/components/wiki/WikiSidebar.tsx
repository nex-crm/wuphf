// biome-ignore-all lint/a11y/useAriaPropsSupportedByRole: Passive metadata uses accessible labels queried by screen-reader tests; visual text remains unchanged.
// biome-ignore-all lint/a11y/useKeyWithClickEvents: Pointer handler is paired with an existing modal, image, or routed-control keyboard path; preserving current interaction model.
import { useEffect, useMemo, useState } from "react";

import type { DiscoveredSection, WikiCatalogEntry } from "../../api/wiki";
import { resolveGroupOrder } from "../../lib/groupOrder";

/** Left-rail thematic dir groups + Tools section + search. */

interface WikiSidebarProps {
  catalog: WikiCatalogEntry[];
  /**
   * Dynamic sections discovered by the broker — blueprint-declared and
   * article-derived. When provided, this drives the IA instead of the
   * catalog's `group` field so sections evolve with team content.
   * Absent when the backend endpoint is unavailable (test fallback /
   * non-markdown memory backend).
   */
  sections?: DiscoveredSection[];
  currentPath?: string | null;
  onNavigate: (path: string) => void;
  /** Optional audit-log opener — rendered as a footer link when provided. */
  onNavigateAudit?: () => void;
  /** Optional lint opener — rendered as a footer link when provided. */
  onNavigateLint?: () => void;
}

// Sections first seen within this window render a "new" indicator. 7 days
// matches the roadmap copy — a fresh section is novel enough to draw the
// eye, but once a week later it settles into the steady-state IA.
const NEW_SECTION_WINDOW_MS = 7 * 24 * 60 * 60 * 1000;

export default function WikiSidebar({
  catalog,
  sections,
  currentPath,
  onNavigate,
  onNavigateAudit,
  onNavigateLint,
}: WikiSidebarProps) {
  const [query, setQuery] = useState("");
  const [bannerSlug, setBannerSlug] = useState<string | null>(null);

  // When the broker ships sections, use them verbatim for the IA. Otherwise
  // fall back to the legacy catalog-grouping path so the sidebar still
  // renders against mocks / pre-v1.3 brokers.
  const usingSections = Array.isArray(sections) && sections.length > 0;

  const groupedFromCatalog = useMemo(
    () => groupCatalog(catalog, query.trim()),
    [catalog, query],
  );
  const fallbackOrder = useMemo(
    () => resolveGroupOrder(catalog.map((c) => c.group)),
    [catalog],
  );

  const sectionList = useMemo(
    () =>
      usingSections
        ? applyQueryToSections(sections, catalog, query.trim())
        : [],
    [usingSections, sections, catalog, query],
  );

  const newSectionSlugs = useMemo(() => {
    if (!usingSections) return new Set<string>();
    const cutoff = Date.now() - NEW_SECTION_WINDOW_MS;
    const out = new Set<string>();
    for (const s of sections ?? []) {
      if (s.from_schema) continue;
      const ts = Date.parse(s.first_seen_ts);
      if (!Number.isNaN(ts) && ts >= cutoff) out.add(s.slug);
    }
    return out;
  }, [usingSections, sections]);

  return (
    <aside className="wk-nav-sidebar">
      <input
        type="search"
        className="search"
        placeholder="Search wiki…"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
      />
      {bannerSlug ? (
        <AddToBlueprintBanner
          slug={bannerSlug}
          onDismiss={() => setBannerSlug(null)}
        />
      ) : null}
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
                    setBannerSlug(section.slug);
                  }
                }}
              />
            ))
          : fallbackOrder.map((group) => {
              const items = groupedFromCatalog[group];
              if (!items || items.length === 0) return null;
              return (
                <div key={group}>
                  <h3>{group}</h3>
                  <ul>
                    {items.map((item) => (
                      <li
                        key={item.path}
                        className={
                          articlePathKey(currentPath ?? "") ===
                          articlePathKey(item.path)
                            ? "current"
                            : ""
                        }
                      >
                        <a
                          href={`#/wiki/${encodeURI(item.path)}`}
                          onClick={(e) => {
                            e.preventDefault();
                            onNavigate(item.path);
                          }}
                        >
                          {item.title}
                        </a>
                      </li>
                    ))}
                  </ul>
                </div>
              );
            })}
      </div>
      {onNavigateAudit ? (
        <div className="wk-sidebar-audit">
          <button
            type="button"
            className="wk-sidebar-audit-link"
            onClick={(e) => {
              e.preventDefault();
              onNavigateAudit();
            }}
          >
            View audit log →
          </button>
        </div>
      ) : null}
      {onNavigateLint ? (
        <div className="wk-sidebar-audit">
          <button
            type="button"
            className="wk-sidebar-audit-link"
            onClick={(e) => {
              e.preventDefault();
              onNavigateLint();
            }}
          >
            Check wiki health →
          </button>
        </div>
      ) : null}
    </aside>
  );
}

interface SectionWithEntries extends DiscoveredSection {
  entries: WikiCatalogEntry[];
}

interface SectionGroupProps {
  section: DiscoveredSection;
  entries: WikiCatalogEntry[];
  currentPath?: string | null;
  isNew: boolean;
  onNavigate: (path: string) => void;
  onSectionHeaderClick: () => void;
}

function SectionGroup({
  section,
  entries,
  currentPath,
  isNew,
  onNavigate,
  onSectionHeaderClick,
}: SectionGroupProps) {
  const tree = useMemo(
    () => buildSectionTree(section.slug, entries),
    [section.slug, entries],
  );
  const currentKey = articlePathKey(currentPath ?? "");

  return (
    <div className="wk-section-group" data-section-slug={section.slug}>
      <h3
        className={`wk-section-header wk-section-${section.from_schema ? "schema" : "discovered"}`}
        onClick={onSectionHeaderClick}
        title={
          section.from_schema
            ? "Declared in your blueprint"
            : "Discovered from articles your team has written"
        }
      >
        <span className="wk-section-title">
          {section.title || section.slug}
        </span>
        <span className="wk-section-count">{section.article_count}</span>
        {!section.from_schema ? (
          <span className="wk-section-marker" aria-label="Discovered section" />
        ) : null}
        {isNew ? (
          <span className="wk-section-new" aria-label="New section">
            new
          </span>
        ) : null}
      </h3>
      <ul className="wk-tree" aria-label={`${section.slug} articles`}>
        {entries.length === 0 ? (
          <li className="wk-section-empty">
            <em>No articles yet</em>
          </li>
        ) : (
          <TreeBranch
            node={tree}
            currentKey={currentKey}
            onNavigate={onNavigate}
            root={true}
          />
        )}
      </ul>
    </div>
  );
}

interface AddToBlueprintBannerProps {
  slug: string;
  onDismiss: () => void;
}

function AddToBlueprintBanner({ slug, onDismiss }: AddToBlueprintBannerProps) {
  return (
    <div
      className="wk-section-banner"
      role="status"
      data-testid="section-banner"
    >
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
  );
}

function groupCatalog(
  catalog: WikiCatalogEntry[],
  query: string,
): Record<string, WikiCatalogEntry[]> {
  const q = query.toLowerCase();
  const out: Record<string, WikiCatalogEntry[]> = {};
  for (const entry of catalog) {
    if (
      q &&
      !entry.title.toLowerCase().includes(q) &&
      !entry.path.toLowerCase().includes(q)
    ) {
      continue;
    }
    if (!out[entry.group]) out[entry.group] = [];
    out[entry.group].push(entry);
  }
  return out;
}

interface WikiTreeNode {
  name: string;
  key: string;
  folders: WikiTreeNode[];
  articles: WikiCatalogEntry[];
}

function buildSectionTree(
  sectionSlug: string,
  entries: WikiCatalogEntry[],
): WikiTreeNode {
  const root: WikiTreeNode = {
    name: sectionSlug,
    key: sectionSlug,
    folders: [],
    articles: [],
  };
  const folderIndex = new Map<string, WikiTreeNode>([[root.key, root]]);
  const sorted = [...entries].sort((a, b) =>
    normalizePathKey(a.path).localeCompare(normalizePathKey(b.path)),
  );

  for (const entry of sorted) {
    const segments = displayPathSegments(sectionSlug, entry);
    if (segments.length <= 1) {
      root.articles.push(entry);
      continue;
    }
    let parent = root;
    let { key } = root;
    for (const segment of segments.slice(0, -1)) {
      key = `${key}/${segment}`;
      let next = folderIndex.get(key);
      if (!next) {
        next = { name: segment, key, folders: [], articles: [] };
        folderIndex.set(key, next);
        parent.folders.push(next);
      }
      parent = next;
    }
    parent.articles.push(entry);
  }

  sortTree(root);
  return root;
}

function sortTree(node: WikiTreeNode): void {
  node.folders.sort((a, b) => a.name.localeCompare(b.name));
  node.articles.sort((a, b) => a.title.localeCompare(b.title));
  for (const child of node.folders) sortTree(child);
}

function displayPathSegments(
  sectionSlug: string,
  entry: WikiCatalogEntry,
): string[] {
  const segments = normalizePathKey(entry.path).split("/").filter(Boolean);
  if (segments[0] === "team") segments.shift();
  if (segments[0] === sectionSlug) segments.shift();
  return segments.length > 0 ? segments : [entry.title];
}

function normalizePathKey(path: string): string {
  return path.replace(/^\/+/, "").replace(/\.md$/, "");
}

function articlePathKey(path: string): string {
  const normalized = normalizePathKey(path);
  return normalized.startsWith("team/")
    ? normalized.slice("team/".length)
    : normalized;
}

function TreeBranch({
  node,
  currentKey,
  onNavigate,
  root = false,
}: {
  node: WikiTreeNode;
  currentKey: string;
  onNavigate: (path: string) => void;
  root?: boolean;
}) {
  return (
    <>
      {node.folders.map((folder) => (
        <TreeFolder
          key={folder.key}
          node={folder}
          currentKey={currentKey}
          onNavigate={onNavigate}
        />
      ))}
      {node.articles.map((item) => (
        <TreeArticle
          key={item.path}
          item={item}
          current={articlePathKey(item.path) === currentKey}
          onNavigate={onNavigate}
          nested={!root}
        />
      ))}
    </>
  );
}

function TreeFolder({
  node,
  currentKey,
  onNavigate,
}: {
  node: WikiTreeNode;
  currentKey: string;
  onNavigate: (path: string) => void;
}) {
  const [open, setOpen] = useState(true);
  const articleCount = countArticles(node);
  const containsCurrent = branchContainsCurrent(node, currentKey);

  useEffect(() => {
    if (containsCurrent) setOpen(true);
  }, [containsCurrent]);

  return (
    <li className="wk-tree-folder">
      <button
        type="button"
        className="wk-tree-folder-btn"
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
      >
        <span className="wk-tree-chevron" aria-hidden="true">
          {open ? "▾" : "▸"}
        </span>
        <span className="wk-tree-folder-name">{node.name}</span>
        <span className="wk-tree-count">{articleCount}</span>
      </button>
      {open ? (
        <ul className="wk-tree-children">
          <TreeBranch
            node={node}
            currentKey={currentKey}
            onNavigate={onNavigate}
          />
        </ul>
      ) : null}
    </li>
  );
}

function TreeArticle({
  item,
  current,
  onNavigate,
  nested,
}: {
  item: WikiCatalogEntry;
  current: boolean;
  onNavigate: (path: string) => void;
  nested: boolean;
}) {
  return (
    <li className={current ? "current wk-tree-article" : "wk-tree-article"}>
      <a
        href={`#/wiki/${encodeURI(item.path)}`}
        className={`wk-tree-article-link${nested ? " is-nested" : ""}`}
        title={item.path}
        onClick={(e) => {
          e.preventDefault();
          onNavigate(item.path);
        }}
      >
        <span className="wk-tree-file-dot" aria-hidden="true" />
        <span className="wk-tree-article-title">{item.title}</span>
      </a>
    </li>
  );
}

function countArticles(node: WikiTreeNode): number {
  return (
    node.articles.length +
    node.folders.reduce((count, child) => count + countArticles(child), 0)
  );
}

function branchContainsCurrent(
  node: WikiTreeNode,
  currentKey: string,
): boolean {
  if (!currentKey) return false;
  return (
    node.articles.some((item) => articlePathKey(item.path) === currentKey) ||
    node.folders.some((child) => branchContainsCurrent(child, currentKey))
  );
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
  const q = query.toLowerCase();
  const byGroup: Record<string, WikiCatalogEntry[]> = {};
  for (const entry of catalog) {
    if (!byGroup[entry.group]) byGroup[entry.group] = [];
    byGroup[entry.group].push(entry);
  }
  const out: SectionWithEntries[] = [];
  for (const section of sections) {
    const entries = (byGroup[section.slug] ?? []).filter((e) => {
      if (!q) return true;
      return (
        e.title.toLowerCase().includes(q) || e.path.toLowerCase().includes(q)
      );
    });
    // Under search, hide sections that have no matching entry AND are
    // empty by design (blueprint-declared but no articles yet).
    if (q && entries.length === 0) continue;
    out.push({ ...section, entries });
  }
  return out;
}
