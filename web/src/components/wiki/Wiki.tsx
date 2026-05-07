import { useCallback, useEffect, useState } from "react";

import { getSkillsList } from "../../api/client";
import {
  type DiscoveredSection,
  fetchCatalog,
  fetchCatalogStrict,
  fetchSections,
  subscribeSectionsUpdated,
  type WikiCatalogEntry,
} from "../../api/wiki";
import EditLogFooter from "./EditLogFooter";
import WikiArticle from "./WikiArticle";
import WikiAudit from "./WikiAudit";
import WikiCatalog from "./WikiCatalog";
import WikiLint from "./WikiLint";
import WikiSidebar, { type SidebarSkill } from "./WikiSidebar";
import "../../styles/wiki.css";

// Reserved pseudo-path for the audit view. Never collides with a real
// article because real articles must live under `team/` and end in `.md`.
const AUDIT_PATH = "_audit";
// Reserved pseudo-path for the lint view.
const LINT_PATH = "_lint";
type WikiView = "audit" | "lint" | "article" | "catalog";

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
  const [sidebarSkills, setSidebarSkills] = useState<SidebarSkill[]>([]);

  useEffect(() => {
    let cancelled = false;
    // Parallel fetch: catalog and sections are independent.
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

  useEffect(() => {
    let cancelled = false;
    getSkillsList("all")
      .then((res) => {
        if (cancelled) return;
        setSidebarSkills(
          (res.skills ?? []).map((s) => ({
            name: s.name,
            title: s.title,
            status: s.status,
          })),
        );
      })
      .catch(() => {
        // Skills section is additive — a failure here should not break the wiki.
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const refreshCatalog = useCallback(() => {
    fetchCatalogStrict()
      .then((c) => setCatalog(c))
      .catch(() => {
        // Keep the last known catalog; a transient refresh miss should not
        // blank navigation while the live section event is still useful.
      });
  }, []);

  // Live-update sections when the broker emits wiki:sections_updated.
  // The event payload carries the full section list; the article list still
  // comes from /wiki/catalog, so refresh it after a write-created section.
  useEffect(() => {
    const unsubscribe = subscribeSectionsUpdated((event) => {
      if (Array.isArray(event.sections)) {
        setSections(event.sections);
        refreshCatalog();
      }
    });
    return () => unsubscribe();
  }, [refreshCatalog]);

  const view = wikiViewFor(articlePath);
  const isAudit = view === "audit";
  const isLint = view === "lint";
  const editLogHistoryPath = wikiHistoryPath(articlePath, view);

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
          skills={sidebarSkills}
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
      {!loading && (
        <EditLogFooter
          historyPath={editLogHistoryPath}
          onNavigate={(path) => onNavigate(path)}
        />
      )}
    </div>
  );
}

function wikiViewFor(articlePath: string | null | undefined): WikiView {
  if (articlePath === AUDIT_PATH) return "audit";
  if (articlePath === LINT_PATH) return "lint";
  return articlePath ? "article" : "catalog";
}

function wikiHistoryPath(
  articlePath: string | null | undefined,
  view: WikiView,
): string | null | undefined {
  return view === "article" ? articlePath : null;
}
