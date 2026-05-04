import { rootRoute } from "../lib/router";

// Sentinel routeId for the root match — TanStack Router exposes this as
// `__root__`. Imported via `rootRoute.id` so a future TanStack rename
// surfaces as a single broken reference instead of a silent string-match
// drift.
export const ROOT_ROUTE_ID = rootRoute.id;

/** True for unknown URLs — the kind a not-found surface should catch. */
export function isUnmatchedRoute(routeId: string | undefined): boolean {
  return !routeId || routeId === ROOT_ROUTE_ID;
}
