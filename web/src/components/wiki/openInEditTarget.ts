/**
 * Cross-navigation hand-off used by the wiki file tree's "New page" flow.
 *
 * Creating a page commits a stub and then navigates to the new article via the
 * shared `onNavigate(path)` contract — which by default lands on the read view.
 * For a brand-new page that is the wrong destination: the user just authored
 * the page and wants the WYSIWYG editor, not an empty read view they have to
 * click "Edit source" out of.
 *
 * Rather than widen `onNavigate` to thread an "open in edit" flag through the
 * router, we park the intent here (sessionStorage, short TTL) keyed to the path
 * being created. WikiArticle pops it on mount; when it matches the path being
 * rendered it defaults the tab to the editor. This mirrors the established
 * `maintenanceTarget.ts` hand-off so the navigation signature stays a plain
 * `(path: string) => void` everywhere.
 */

const STORAGE_KEY = "wuphf:wiki-open-in-edit:target";
const TTL_MS = 30_000;

interface OpenInEditSnapshot {
  path: string;
  ts: number;
}

/** Record that `path` should open directly in the editor on next navigation. */
export function requestOpenInEdit(path: string): void {
  if (typeof window === "undefined") return;
  try {
    const payload: OpenInEditSnapshot = { path, ts: Date.now() };
    window.sessionStorage.setItem(STORAGE_KEY, JSON.stringify(payload));
  } catch {
    // Storage disabled — fall through; the page still opens in read view and
    // the user can click Edit. Not a hard failure.
  }
}

/**
 * Pop the pending "open in edit" intent if it matches the article path about to
 * render. Stale (older than TTL_MS) and mismatched paths are dropped without
 * being honoured so an existing page never opens in edit by accident. The slot
 * is cleared on every read so the intent fires exactly once.
 */
export function consumeOpenInEdit(articlePath: string): boolean {
  if (typeof window === "undefined") return false;
  try {
    const raw = window.sessionStorage.getItem(STORAGE_KEY);
    if (!raw) return false;
    window.sessionStorage.removeItem(STORAGE_KEY);
    const parsed = JSON.parse(raw) as Partial<OpenInEditSnapshot>;
    if (typeof parsed.path !== "string" || typeof parsed.ts !== "number") {
      return false;
    }
    if (Date.now() - parsed.ts > TTL_MS) return false;
    return pathMatches(articlePath, parsed.path);
  } catch {
    return false;
  }
}

/**
 * The create flow stores the canonical `team/<dir>/<slug>.md` path, while the
 * article view may be navigated with either that canonical path or a bare
 * slug (`people/nazz`). Match on exact equality plus the suffix forms
 * fetchArticle resolves, so the intent is honoured for the page just created.
 */
function pathMatches(articlePath: string, target: string): boolean {
  if (!target) return false;
  if (articlePath === target) return true;
  const targetSlug = target.replace(/\.md$/, "");
  const articleSlug = articlePath.replace(/\.md$/, "");
  if (articleSlug === targetSlug) return true;
  return target.endsWith(`/${articleSlug}.md`) || target.endsWith(articleSlug);
}
