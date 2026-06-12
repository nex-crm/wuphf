import { useCallback, useEffect, useRef, useState } from "react";

import {
  type DiscoveredSection,
  fetchCatalogStrict,
  fetchSections,
  subscribeSectionsUpdated,
  type WikiCatalogEntry,
} from "../../api/wiki";
import EditLogFooter from "./EditLogFooter";
import { APP_NAV_PREFIX } from "./tree/WikiTree";
import FileViewer, { isMarkdownPath } from "./viewers/FileViewer";
import WebsiteViewer from "./WebsiteViewer";
import WikiArticle from "./WikiArticle";
import WikiAudit from "./WikiAudit";
import WikiCategoryPage from "./WikiCategoryPage";
import WikiHome from "./WikiHome";
import WikiLint from "./WikiLint";
import WikiSidebar from "./WikiSidebar";
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
  | "category";

/**
 * localStorage slot for the last article the user had open. Opening the wiki
 * with no explicit path resumes here (docmost-style "land where you left
 * off") instead of forcing a detour through a landing page.
 */
export const WIKI_LAST_VIEWED_KEY = "wuphf:wiki:last-viewed";

function readLastViewed(): string | null {
  try {
    const value = globalThis.localStorage?.getItem(WIKI_LAST_VIEWED_KEY);
    return value && value.length > 0 ? value : null;
  } catch {
    return null;
  }
}

function writeLastViewed(path: string): void {
  try {
    globalThis.localStorage?.setItem(WIKI_LAST_VIEWED_KEY, path);
  } catch {
    // Private mode / quota — resume-where-you-left-off is best-effort.
  }
}

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
 * Wiki shell, docmost/Notion-style (visual + IA reference only — original
 * implementation in our stack):
 *
 * A persistent left page tree is THE navigation — spaces (the wiki's kinds:
 * Companies, People, Playbooks, …) as root groups with articles nested
 * beneath, search at the top. Opening the wiki resumes on the last-viewed
 * article (or a quiet overview page when there is none). Recent changes and
 * wiki health stay one click away in the sidebar menu. Deep-link article
 * routes are untouched.
 */
export default function Wiki({
  articlePath,
  externalRefreshNonce = 0,
  onNavigate,
}: WikiProps) {
  const [catalog, setCatalog] = useState<WikiCatalogEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [loadNonce, setLoadNonce] = useState(0);

  // Resume where you left off: when the wiki opens with no explicit path,
  // navigate once to the last-viewed article. Guarded by a ref so it fires
  // only for the initial mount — clicking "Overview" later stays on the
  // overview instead of bouncing back to the article.
  const resumeConsideredRef = useRef(false);
  const [resuming, setResuming] = useState(
    () => !articlePath && readLastViewed() !== null,
  );
  useEffect(() => {
    if (resumeConsideredRef.current) return;
    resumeConsideredRef.current = true;
    if (articlePath) {
      setResuming(false);
      return;
    }
    const last = readLastViewed();
    if (last) onNavigate(last);
    setResuming(false);
  }, [articlePath, onNavigate]);

  useEffect(() => {
    let cancelled = false;
    void loadNonce;
    setLoading(true);
    setLoadError(null);
    // The STRICT catalog fetch is deliberate: the lenient fetchCatalog()
    // swallows errors into an empty list, which is precisely how the home
    // page rendered "0 articles" as fact over a failed load (C4). Here the
    // error must reach the catch so the shell can show the
    // broker-not-responding state instead. fetchSections keeps the broker's
    // section discovery warm for live updates below.
    Promise.all([fetchCatalogStrict(), fetchSections()])
      .then(([c, _s]: [WikiCatalogEntry[], DiscoveredSection[]]) => {
        if (cancelled) return;
        setCatalog(c);
      })
      .catch((err: unknown) => {
        // Honest failure: never render "0 articles" as fact over a
        // failed load. The view below switches to an explicit
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

  const refreshCatalog = useCallback(() => {
    fetchCatalogStrict()
      .then((c) => setCatalog(c))
      .catch(() => {
        // Keep the last known catalog; a transient refresh miss should not
        // blank navigation while the live section event is still useful.
      });
  }, []);

  // Live-update the catalog when the broker emits wiki:sections_updated.
  // The article list comes from /wiki/catalog, so refresh it after a
  // write-created section.
  useEffect(() => {
    const unsubscribe = subscribeSectionsUpdated((event) => {
      if (Array.isArray(event.sections)) {
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

  // Persist the last-viewed article so the next wiki open resumes there.
  useEffect(() => {
    if (view === "article" && articlePath) writeLastViewed(articlePath);
  }, [view, articlePath]);

  return (
    <div className="wiki-root" data-testid="wiki-root">
      <div className="wiki-layout" data-view={view}>
        <WikiSidebar
          currentPath={appPath ?? articlePath}
          onNavigate={(path) => onNavigate(path)}
        />
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
        ) : view === "article" && articlePath ? (
          <WikiArticle
            path={articlePath}
            catalog={catalog}
            onNavigate={(path) => onNavigate(path)}
            externalRefreshNonce={externalRefreshNonce}
          />
        ) : resuming ? (
          <main
            className="wiki-main wk-shell-state wk-shell-state--loading"
            data-testid="wk-resuming"
            aria-busy="true"
          >
            <p className="wk-shell-state-msg">Opening your last page…</p>
          </main>
        ) : loading ? (
          <main
            className="wiki-main wk-shell-state wk-shell-state--loading"
            data-testid="wk-catalog-loading"
            aria-busy="true"
          >
            <p className="wk-shell-state-msg">Loading wiki…</p>
          </main>
        ) : loadError ? (
          <main
            className="wiki-main wk-shell-state wk-shell-state--error"
            data-testid="wk-catalog-error"
          >
            <p className="wk-shell-state-msg" role="alert">
              Broker not responding — {loadError}
            </p>
            <button
              type="button"
              className="wk-shell-retry"
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
  // The legacy "All files" surface is retired — the page tree is always
  // visible now. Old `_files` deep links land on the overview.
  if (articlePath === FILES_PATH) return "home";
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
