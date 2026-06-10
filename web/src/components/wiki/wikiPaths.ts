/**
 * Reserved pseudo-paths for non-article wiki views.
 *
 * These ride through the same `#/wiki/<path>` splat as real articles but
 * never collide with one: real articles must live under `team/` and end in
 * `.md`, while every pseudo-path starts with an underscore. Centralized here
 * so the shell (Wiki.tsx), the nav rail, and the article chrome all agree on
 * the same strings.
 */

/** Audit-log view. */
export const AUDIT_PATH = "_audit";
/** Wiki-health (lint) view. */
export const LINT_PATH = "_lint";
/**
 * The "All files" escape hatch: the legacy tree-first browsing surface
 * (drag-and-drop file tree + grouped catalog). No longer the default —
 * the wiki home is search-first — but kept reachable because it is the
 * upload surface and the only full filesystem view.
 */
export const FILES_PATH = "_files";
/** Prefix for auto-generated category index pages (`_category/<slug>`). */
export const CATEGORY_PREFIX = "_category/";

/** Build the pseudo-path for a category index page. */
export function categoryPath(slug: string): string {
  return `${CATEGORY_PREFIX}${slug}`;
}

/**
 * Extract the category slug from a `_category/<slug>` pseudo-path.
 * Returns null when the path is not a category path or the slug is empty.
 */
export function parseCategoryPath(path: string): string | null {
  if (!path.startsWith(CATEGORY_PREFIX)) return null;
  const slug = path.slice(CATEGORY_PREFIX.length).trim();
  return slug.length > 0 ? slug : null;
}
