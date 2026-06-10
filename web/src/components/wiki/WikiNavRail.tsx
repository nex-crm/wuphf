import type { ReactNode } from "react";

import { AUDIT_PATH, FILES_PATH, LINT_PATH } from "./wikiPaths";

/**
 * Wikipedia-style left navigation rail: a short list of site-wide entry
 * points (Main page, Recent changes, All files, Wiki health) plus an
 * optional slot for the per-article Contents panel.
 *
 * This replaces the folder tree as the wiki's primary navigation — the
 * tree stays one click away behind "All files" (the upload surface and
 * full filesystem escape hatch).
 */

interface WikiNavRailProps {
  /** Pseudo-path of the active view ("" = home). */
  activePath?: string | null;
  onNavigate: (path: string) => void;
  /** Per-article Contents panel (and any future article-scoped slots). */
  children?: ReactNode;
}

const LINKS: { label: string; path: string }[] = [
  { label: "Main page", path: "" },
  { label: "Recent changes", path: AUDIT_PATH },
  { label: "All files", path: FILES_PATH },
  { label: "Wiki health", path: LINT_PATH },
];

export default function WikiNavRail({
  activePath,
  onNavigate,
  children,
}: WikiNavRailProps) {
  const current = activePath ?? "";
  return (
    <aside className="wk-nav-rail" data-testid="wk-nav-rail">
      <nav aria-label="Wiki navigation">
        <ul className="wk-nav-rail-links">
          {LINKS.map((link) => {
            const isActive = current === link.path;
            return (
              <li key={link.label}>
                <a
                  href={`#/wiki/${link.path}`}
                  className={isActive ? "is-active" : undefined}
                  aria-current={isActive ? "page" : undefined}
                  onClick={(e) => {
                    e.preventDefault();
                    onNavigate(link.path);
                  }}
                >
                  {link.label}
                </a>
              </li>
            );
          })}
        </ul>
      </nav>
      {children}
    </aside>
  );
}
