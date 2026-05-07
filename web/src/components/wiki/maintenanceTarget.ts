import type { WikiMaintenanceAction } from "../../api/wiki";

/**
 * Cross-route hand-off used by WikiLint's "Suggest fix" → WikiMaintenanceAssistant.
 *
 * WikiLint lives at /wiki/.lint while the assistant lives in WikiArticle's
 * sidebar. Clicking "Suggest fix" navigates to the entity article *and*
 * needs to tell the assistant which action to focus. We avoid threading a
 * prop through a router by parking the request in sessionStorage with a
 * short TTL — the assistant picks it up on mount, then clears the slot.
 */

const STORAGE_KEY = "wuphf:wiki-maint:target";
const TTL_MS = 60_000;

export interface MaintenanceTargetSnapshot {
  slug: string;
  action: WikiMaintenanceAction;
  ts: number;
}

export function requestMaintenanceTarget(
  slug: string,
  action: WikiMaintenanceAction,
): void {
  if (typeof window === "undefined") return;
  try {
    const payload: MaintenanceTargetSnapshot = {
      slug,
      action,
      ts: Date.now(),
    };
    window.sessionStorage.setItem(STORAGE_KEY, JSON.stringify(payload));
  } catch {
    // Storage disabled — fall through; the user can still pick the action manually.
  }
}

/**
 * Pop the pending target if it matches the article slug we are about to
 * render. Stale (older than TTL_MS) and mismatched slugs are dropped without
 * being returned so the assistant does not auto-open with a wrong action.
 */
export function consumeMaintenanceTarget(
  articlePath: string,
): WikiMaintenanceAction | null {
  if (typeof window === "undefined") return null;
  try {
    const raw = window.sessionStorage.getItem(STORAGE_KEY);
    if (!raw) return null;
    window.sessionStorage.removeItem(STORAGE_KEY);
    const parsed = JSON.parse(raw) as Partial<MaintenanceTargetSnapshot>;
    if (
      typeof parsed.slug !== "string" ||
      typeof parsed.action !== "string" ||
      typeof parsed.ts !== "number"
    ) {
      return null;
    }
    if (Date.now() - parsed.ts > TTL_MS) return null;
    if (!articleMatchesSlug(articlePath, parsed.slug)) return null;
    return parsed.action as WikiMaintenanceAction;
  } catch {
    return null;
  }
}

function articleMatchesSlug(articlePath: string, slug: string): boolean {
  if (!slug) return false;
  if (articlePath === slug) return true;
  return (
    articlePath.includes(`/${slug}.md`) || articlePath.endsWith(`/${slug}`)
  );
}
