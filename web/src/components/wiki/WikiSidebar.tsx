import WikiTree from "./tree/WikiTree";
import { AUDIT_PATH, LINT_PATH } from "./wikiPaths";

/**
 * The wiki's persistent left navigation: a page tree as THE way around
 * (docmost/Notion model — visual/IA reference only, original implementation).
 *
 * Layout, top to bottom:
 *   1. Fixed menu links — Overview (home), Recent changes, Wiki health.
 *   2. The page tree (search at its top, then spaces → nested pages).
 *
 * The tree's top-level folders are the wiki's kinds (Companies, People,
 * Playbooks, …) and act as the root groups; articles nest beneath them.
 * This replaced the old home/category/All-files split — the tree is always
 * visible next to whatever page is open.
 */

interface WikiSidebarProps {
  /** Currently-open article path (full team/...md), highlighted in the tree. */
  currentPath?: string | null;
  onNavigate: (path: string) => void;
}

interface SidebarLink {
  label: string;
  path: string;
  testId: string;
}

const MENU_LINKS: SidebarLink[] = [
  { label: "Overview", path: "", testId: "wk-sidebar-home" },
  { label: "Recent changes", path: AUDIT_PATH, testId: "wk-sidebar-audit" },
  { label: "Wiki health", path: LINT_PATH, testId: "wk-sidebar-lint" },
];

export default function WikiSidebar({
  currentPath,
  onNavigate,
}: WikiSidebarProps) {
  const current = currentPath ?? "";
  return (
    <aside className="wk-nav-sidebar" data-testid="wk-nav-sidebar">
      <nav aria-label="Wiki navigation" className="wk-sidebar-menu">
        {MENU_LINKS.map((link) => {
          const isActive = current === link.path;
          return (
            <a
              key={link.testId}
              href={`#/wiki/${link.path}`}
              className={`wk-sidebar-menu-link${isActive ? " is-active" : ""}`}
              aria-current={isActive ? "page" : undefined}
              data-testid={link.testId}
              onClick={(e) => {
                e.preventDefault();
                onNavigate(link.path);
              }}
            >
              {link.label}
            </a>
          );
        })}
      </nav>
      <div className="wk-sidebar-pages-label" aria-hidden="true">
        Pages
      </div>
      <WikiTree currentPath={currentPath} onNavigate={onNavigate} />
    </aside>
  );
}
