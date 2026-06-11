import { useCallback, useEffect, useState } from "react";

import { getSkillsList } from "../../api/client";
import {
  type DiscoveredSection,
  fetchCatalogStrict,
  fetchSections,
  subscribeSectionsUpdated,
  type WikiCatalogEntry,
} from "../../api/wiki";
import { useOfficeStats } from "../../hooks/useOfficeStats";
import EditLogFooter from "./EditLogFooter";
import { APP_NAV_PREFIX } from "./tree/WikiTree";
import FileViewer, { isMarkdownPath } from "./viewers/FileViewer";
import WebsiteViewer from "./WebsiteViewer";
import WikiArticle from "./WikiArticle";
import WikiAudit from "./WikiAudit";
import WikiCatalog from "./WikiCatalog";
import WikiCategoryPage from "./WikiCategoryPage";
import WikiHome from "./WikiHome";
import WikiLint from "./WikiLint";
import WikiNavRail from "./WikiNavRail";
import WikiSidebar, { type SidebarSkill } from "./WikiSidebar";
import {
  AUDIT_PATH,
  FILES_PATH,
  LINT_PATH,
  parseCategoryPath,
} from "./wikiPaths";
import "../../styles/wiki.css";
import "../../styles/wiki-viewers.css";

type WikiView =
  | "audit"
  | "lint"
  | "article"
  | "file"
  | "app"
  | "home"
  | "files"
  | "category";

/**
 * True when `path` targets an embedded app/website folder. The tree prepends
 * APP_NAV_PREFIX when opening an app/website leaf (see WikiTree.openLeaf); we
 * route those to WebsiteViewer rather than the article/file view.
 */
function isAppPath(path: string): boolean {
  return path.startsWith(APP_NAV_PREFIX);
}

/** Strip the APP_NAV_PREFIX sentinel back off to recover the real folder path. */
function appFolderPath(path: string): string {
  return path.slice(APP_NAV_PREFIX.length);
}

/**
 * True when an active path should open in the non-article FileViewer rather
 * than the markdown article view. Pseudo-paths (handled before this in
 * wikiViewFor) and bare slugs / `.md` paths stay on the article path;
 * anything with a non-markdown extension (team/assets/x.pdf, .png, .csv, …)
 * is a wiki file.
 *
 * A bare slug like `people/nazz` has no extension and resolves to an article
 * via fetchArticle's candidate paths, so it correctly stays out of this branch.
 */
function isFilePath(path: string): boolean {
  const leaf = path.split("/").pop() ?? path;
  const dot = leaf.lastIndexOf(".");
  // No extension → treat as an article slug/path, not a file.
  if (dot <= 0 || dot === leaf.length - 1) return false;
  return !isMarkdownPath(path);
}

interface WikiProps {
  /** When set, renders the article view for this path; otherwise the home page. */
  articlePath?: string | null;
  /**
   * Bumped by Pam (hoisted up to the tab bar) when she finishes an action
   * against the current article. Wiki forwards it into WikiArticle so the
   * article + history re-fetch without a full navigation.
   */
  externalRefreshNonce?: number;
  onNavigate: (path: string | null) => void;
}

/**
 * Wikipedia-style wiki shell.
 *
 * The home page is search-first (big search box + category entry points +
 * recent changes); articles read with a left Contents rail and a right
 * meta rail; categories replace folders as the organizing surface. The
 * legacy Files/Sections tree survives behind "All files" — it is the
 * upload surface and the full-filesystem escape hatch, no longer the
 * default navigation.
 */
export default function Wiki({
  articlePath,
  externalRefreshNonce = 0,
  onNavigate,
}: WikiProps) {
  const [catalog, setCatalog] = useState<WikiCatalogEntry[]>([]);
  const [sections, setSections] = useState<DiscoveredSection[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [loadNonce, setLoadNonce] = useState(0);
  const [sidebarSkills, setSidebarSkills] = useState<SidebarSkill[]>([]);

  // Shared derived-stats source: the home header's "N articles" reads
  // the broker's count (same filter as /wiki/catalog) instead of the
  // length of whatever slice of the catalog has loaded — the v3 "main
  // page says 0 articles, All-files says 19" contradiction came from
  // counting different local lists.

  useEffect(() => {
    let cancelled = false;
    void loadNonce;
    setLoading(true);
    setLoadError(null);
    // Parallel fetch: catalog and sections are independent. The STRICT
    // catalog fetch is deliberate: the lenient fetchCatalog() swallows
    // errors into an empty list, which is precisely how the home page
    // rendered "0 articles" as fact over a failed load (C4). Here the
    // error must reach the catch so the catalog view can show the
    // broker-not-responding state instead.
    Promise.all([fetchCatalogStrict(), fetchSections()])
      .then(([c, s]) => {
        if (cancelled) return;
        setCatalog(c);
        setSections(s);
      })
      .catch((err: unknown) => {
        // Honest failure: never render "0 articles" as fact over a
        // failed load. The catalog view below switches to an explicit
        // broker-not-responding state with a retry.
        if (!cancelled) {
          setLoadError(
            err instanceof Error ? err.message : "Could not reach the broker.",
          );
        }
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [loadNonce]);

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
  const editLogHistoryPath = wikiHistoryPath(articlePath, view);
  // The app view navigates with the APP_NAV_PREFIX sentinel; strip it so the
  // sidebar tree can still highlight the underlying folder, and so the viewer
  // receives the real folder path.
  const appPath =
    view === "app" && articlePath ? appFolderPath(articlePath) : null;
  const categorySlug =
    view === "category" && articlePath ? parseCategoryPath(articlePath) : null;

  // The legacy tree sidebar serves the file-ish views (All files, file
  // viewer, embedded apps); every other view gets the Wikipedia-style nav
  // rail. The article view renders its own rail (with the Contents panel)
  // inside WikiArticle.
  const showTreeSidebar = view === "files" || view === "file" || view === "app";
  const showNavRail =
    view === "home" ||
    view === "category" ||
    view === "audit" ||
    view === "lint";

  return (
    <div className="wiki-root" data-testid="wiki-root">
      <div className="wiki-layout" data-view={view}>
        {showTreeSidebar ? (
          <WikiSidebar
            catalog={catalog}
            sections={sections}
            currentPath={view === "files" ? null : (appPath ?? articlePath)}
            onNavigate={(path) => onNavigate(path)}
            onNavigateAudit={() => onNavigate(AUDIT_PATH)}
            onNavigateLint={() => onNavigate(LINT_PATH)}
            skills={sidebarSkills}
            defaultMode="tree"
          />
        ) : null}
        {showNavRail ? (
          <WikiNavRail
            activePath={articlePath ?? ""}
            onNavigate={(path) => onNavigate(path)}
          />
        ) : null}
        {view === "audit" ? (
          <WikiAudit onNavigate={(path) => onNavigate(path)} />
        ) : view === "lint" ? (
          <WikiLint onNavigate={(path) => onNavigate(path)} />
        ) : view === "app" && appPath ? (
          <div className="wiki-main wiki-main--app" data-testid="wiki-app">
            <WebsiteViewer path={appPath} onExit={() => onNavigate(null)} />
          </div>
        ) : view === "file" && articlePath ? (
          <div className="wiki-main wiki-main--file" data-testid="wiki-file">
            <FileViewer path={articlePath} />
          </div>
        ) : view === "category" && categorySlug ? (
          <WikiCategoryPage
            slug={categorySlug}
            catalog={catalog}
            onNavigate={(path) => onNavigate(path)}
          />
        ) : view === "files" ? (
          <WikiCatalog
            catalog={catalog}
            onNavigate={(path) => onNavigate(path)}
            onOpenAudit={() => onNavigate(AUDIT_PATH)}
          />
        ) : view === "article" && articlePath ? (
          <WikiArticle
            path={articlePath}
            catalog={catalog}
            onNavigate={(path) => onNavigate(path)}
            externalRefreshNonce={externalRefreshNonce}
          />
        ) : loading ? (
          <main
            className="wiki-main wk-catalog wk-catalog--loading"
            data-testid="wk-catalog-loading"
            aria-busy="true"
          >
            <p className="wk-catalog-stats">Loading wiki…</p>
          </main>
        ) : loadError ? (
          <main
            className="wiki-main wk-catalog wk-catalog--error"
            data-testid="wk-catalog-error"
          >
            <p className="wk-catalog-stats" role="alert">
              Broker not responding — {loadError}
            </p>
            <button
              type="button"
              className="wk-catalog-retry"
              onClick={() => setLoadNonce((n) => n + 1)}
            >
              Retry
            </button>
          </main>
        ) : (
          <WikiHome catalog={catalog} onNavigate={(path) => onNavigate(path)} />
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
  if (!articlePath) return "home";
  if (articlePath === AUDIT_PATH) return "audit";
  if (articlePath === LINT_PATH) return "lint";
  if (articlePath === FILES_PATH) return "files";
  if (parseCategoryPath(articlePath) !== null) return "category";
  if (isAppPath(articlePath)) return "app";
  return isFilePath(articlePath) ? "file" : "article";
}

function wikiHistoryPath(
  articlePath: string | null | undefined,
  view: WikiView,
): string | null | undefined {
  return view === "article" ? articlePath : null;
}
