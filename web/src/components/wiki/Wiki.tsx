import { useEffect, useState } from "react";

import {
  type DiscoveredSection,
  fetchCatalog,
  fetchSections,
  subscribeSectionsUpdated,
  type WikiCatalogEntry,
} from "../../api/wiki";
import EditLogFooter from "./EditLogFooter";
import WikiArticle from "./WikiArticle";
import WikiAudit from "./WikiAudit";
import WikiCatalog from "./WikiCatalog";
import WikiLint from "./WikiLint";
import WikiSidebar from "./WikiSidebar";
import "../../styles/wiki.css";

// Reserved pseudo-path for the audit view. Never collides with a real
// article because real articles must live under `team/` and end in `.md`.
const AUDIT_PATH = "_audit";
// Reserved pseudo-path for the lint view.
const LINT_PATH = "_lint";

interface WikiProps {
  /** When set, renders the article view for this path; otherwise renders the catalog. */
  articlePath?: string | null;
  /**
   * Bumped by Pam (hoisted up to the tab bar) when she finishes an action
   * against the current article. Wiki forwards it into WikiArticle so the
   * article + history re-fetch without a full navigation.
   */
  externalRefreshNonce?: number;
  onNavigate: (path: string | null) => void;
}

/** Three-column wiki shell: left sidebar · main (catalog or article) · right rail (article only). */
export default function Wiki({
  articlePath,
  externalRefreshNonce = 0,
  onNavigate,
}: WikiProps) {
  const [catalog, setCatalog] = useState<WikiCatalogEntry[]>([]);
  const [sections, setSections] = useState<DiscoveredSection[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    // Parallel fetch: catalog and sections are independent so we pay one
    // round-trip of latency, not two.
    Promise.all([fetchCatalog(), fetchSections()])
      .then(([c, s]) => {
        if (cancelled) return;
        setCatalog(c);
        setSections(s);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  // Live-update sections when the broker emits wiki:sections_updated.
  // The event payload carries the full list so no refetch is needed.
  useEffect(() => {
    const unsubscribe = subscribeSectionsUpdated((event) => {
      if (Array.isArray(event.sections)) {
        setSections(event.sections);
      }
    });
    return () => unsubscribe();
  }, []);

  const isAudit = articlePath === AUDIT_PATH;
  const isLint = articlePath === LINT_PATH;
  const view = isAudit
    ? "audit"
    : isLint
      ? "lint"
      : articlePath
        ? "article"
        : "catalog";

  return (
    <div className="wiki-root" data-testid="wiki-root">
      <div className="wiki-layout" data-view={view}>
        <WikiSidebar
          catalog={catalog}
          sections={sections}
          currentPath={isAudit || isLint ? null : articlePath}
          onNavigate={(path) => onNavigate(path)}
          onNavigateAudit={() => onNavigate(AUDIT_PATH)}
          onNavigateLint={() => onNavigate(LINT_PATH)}
        />
        {isAudit ? (
          <WikiAudit onNavigate={(path) => onNavigate(path)} />
        ) : isLint ? (
          <WikiLint onNavigate={(path) => onNavigate(path)} />
        ) : articlePath ? (
          <WikiArticle
            path={articlePath}
            catalog={catalog}
            onNavigate={(path) => onNavigate(path)}
            externalRefreshNonce={externalRefreshNonce}
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
  );
}
