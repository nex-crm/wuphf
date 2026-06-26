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
/**
 * Sources browser view — the immutable source layer the wiki compiles FROM.
 * A bare `_sources` opens the list; `_sources/<kind>/<id>` deep-links to one
 * record (used by citation badges' "View source").
 */
export const SOURCES_PATH = "_sources";
const SOURCES_PREFIX = `${SOURCES_PATH}/`;

/** One selected source record inside the browser. */
export interface SourcesSelection {
  kind: string;
  id: string;
}

/** Build the pseudo-path that deep-links to one source record. */
export function sourceRecordPath(kind: string, id: string): string {
  return `${SOURCES_PREFIX}${encodeURIComponent(kind)}/${encodeURIComponent(id)}`;
}

/**
 * True when `path` targets the Sources browser (the bare list or a record).
 */
export function isSourcesPath(path: string): boolean {
  return path === SOURCES_PATH || path.startsWith(SOURCES_PREFIX);
}

/**
 * Extract the `{kind, id}` selection from a `_sources/<kind>/<id>` path.
 * Returns null for the bare `_sources` list (no selection) or a malformed
 * path.
 */
export function parseSourcesPath(path: string): SourcesSelection | null {
  if (!path.startsWith(SOURCES_PREFIX)) return null;
  const rest = path.slice(SOURCES_PREFIX.length);
  const slash = rest.indexOf("/");
  if (slash <= 0) return null;
  const kind = decodeURIComponent(rest.slice(0, slash));
  const id = decodeURIComponent(rest.slice(slash + 1));
  if (!(kind && id)) return null;
  return { kind, id };
}

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
