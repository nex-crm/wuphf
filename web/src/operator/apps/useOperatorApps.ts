// Operator apps data layer — thin React Query hooks over the existing app
// builder client (web/src/api/apps.ts). The backend, persistence, build
// pipeline, and Bridge v2 are reused unchanged; the operator surface only adds
// its own hooks + skin. Pure resolvers live here too so the build→appear
// correlation is unit-testable without a network.

import { useMutation, useQuery } from "@tanstack/react-query";

import {
  type AppBuildRequest,
  type CustomApp,
  type CustomAppDetail,
  getApp,
  listApps,
  requestAppBuild,
} from "../../api/apps";

/** A freshly-registered app shows up within a few seconds; poll while waiting. */
const APPS_POLL_MS = 4000;

/** App ids carry an `app_<hex>` prefix; the operator uses it to tell a real
 * built app apart from a mock tool id. */
export const APP_ID_PREFIX = "app_";

export function isRealAppId(id: string | null | undefined): boolean {
  return typeof id === "string" && id.startsWith(APP_ID_PREFIX);
}

/**
 * List the workspace's built apps. Polls while any app is still building (the
 * app-builder runs async, ~30–60s) so a freshly-published app appears without a
 * reload; settles to no polling once everything is ready.
 */
export function useOperatorApps() {
  return useQuery({
    queryKey: ["operator-apps"],
    queryFn: listApps,
    refetchInterval: (query) => {
      const apps = query.state.data ?? [];
      return apps.some((a) => a.status === "building") ? APPS_POLL_MS : false;
    },
  });
}

/**
 * Load one app's manifest + sealed HTML. Polls while the app is still building
 * (status "building" or no HTML yet) so the UI tab swaps the building state for
 * the live app the moment the first version publishes.
 */
export function useOperatorApp(id: string | null) {
  return useQuery({
    queryKey: ["operator-app", id],
    queryFn: () => getApp(id ?? ""),
    enabled: isRealAppId(id),
    refetchInterval: (query) => {
      const detail = query.state.data as CustomAppDetail | undefined;
      if (!detail) return APPS_POLL_MS;
      const building = detail.app.status === "building" || !detail.html;
      return building ? APPS_POLL_MS : false;
    },
  });
}

/** Kick off an app-builder build/improve. Returns the created Task. */
export function useBuildApp() {
  return useMutation({
    mutationFn: (req: AppBuildRequest) => requestAppBuild(req),
  });
}

// ── Pure resolvers (unit-tested) ────────────────────────────────────────────

/**
 * Pick the app a just-started build produced: the newest app whose id was NOT
 * present before the build began. Robust to the agent tweaking the display name
 * (a name-only match would miss "Open tasks" vs "Open Tasks dashboard"); the
 * only app that can appear after we snapshot the existing ids is the one we just
 * asked to build. Newest-first by updatedAt, then createdAt, as a tiebreak.
 */
export function resolveNewAppId(
  beforeIds: ReadonlySet<string>,
  apps: readonly CustomApp[],
): string | null {
  const fresh = apps.filter((a) => !beforeIds.has(a.id));
  if (fresh.length === 0) return null;
  const newest = [...fresh].sort((a, b) => {
    const byUpdated = (b.updatedAt ?? "").localeCompare(a.updatedAt ?? "");
    if (byUpdated !== 0) return byUpdated;
    return (b.createdAt ?? "").localeCompare(a.createdAt ?? "");
  })[0];
  return newest.id;
}

/**
 * Derive a short, stable app name from the operator's free-text description, so
 * a chat-first "describe it" flow still gives requestAppBuild an explicit name
 * (the brief instructs the agent to register under it). Take the first clause,
 * strip filler lead-ins, title-case, and cap to a handful of words.
 */
export function deriveAppName(description: string): string {
  const leadIn =
    /^\s*(please|build|make|create|set up|give me|i want|i need|a|an|the)\s+/i;
  let firstClause = description.split(/[.\n,;:]/)[0].trim();
  // Strip a chain of lead-ins ("build a dashboard" → "dashboard"), not just one.
  let prev = "";
  while (prev !== firstClause) {
    prev = firstClause;
    firstClause = firstClause.replace(leadIn, "").trim();
  }
  const words = firstClause.split(/\s+/).filter(Boolean).slice(0, 6);
  if (words.length === 0) return "Untitled app";
  const titled = words
    .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
    .join(" ");
  return titled.slice(0, 120);
}
