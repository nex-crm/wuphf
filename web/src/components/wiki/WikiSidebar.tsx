import WikiTree from "./tree/WikiTree";
import { CollapsedPanelRail, PanelCollapseButton } from "./WikiPanelChrome";
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
  /** Folds the sidebar to a thin rail when true. */
  collapsed?: boolean;
  /**
   * Toggles collapse. When omitted, the collapse affordance is hidden entirely
   * (the sidebar renders as before) — keeps standalone/test usage unchanged.
   */
  onToggleCollapse?: () => void;
}

interface SidebarLink {
  label: string;
  path: string;
  testId: string;
}

const MENU_LINKS: SidebarLink[] = [
  { label: "Overview", path: "", testId: "wk-sidebar-home" },
  { label: "Recent changes", path: AUDIT_PATH, testId: "wk-sidebar-audit" },
  { label: "Brain health", path: LINT_PATH, testId: "wk-sidebar-lint" },
];

/** Whether a menu link is the active surface. */
function isMenuLinkActive(linkPath: string, current: string): boolean {
  return current === linkPath;
}

export default function WikiSidebar({
  currentPath,
  onNavigate,
  collapsed = false,
  onToggleCollapse,
}: WikiSidebarProps) {
  const current = currentPath ?? "";

  if (collapsed && onToggleCollapse) {
    return (
      <aside
        className="wk-nav-sidebar is-collapsed"
        data-testid="wk-nav-sidebar"
      >
        <CollapsedPanelRail
          side="left"
          label="Pages"
          onExpand={onToggleCollapse}
        />
      </aside>
    );
  }

  return (
    <aside className="wk-nav-sidebar" data-testid="wk-nav-sidebar">
      {onToggleCollapse ? (
        <div className="wk-sidebar-head">
          <PanelCollapseButton
            side="left"
            label="Pages"
            onClick={onToggleCollapse}
          />
        </div>
      ) : null}
      <nav aria-label="Wiki navigation" className="wk-sidebar-menu">
        {MENU_LINKS.map((link) => {
          const isActive = isMenuLinkActive(link.path, current);
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
