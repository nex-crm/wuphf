// Operator apps data layer — thin React Query hooks over the existing app
// builder client (web/src/api/apps.ts). The backend, persistence, build
// pipeline, and Bridge v2 are reused unchanged; the operator surface only adds
// its own hooks + skin. Pure resolvers live here too so the build→appear
// correlation is unit-testable without a network.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import {
  type AppBuildRequest,
  type AppCapabilities,
  type CustomApp,
  type CustomAppDetail,
  getApp,
  getAppCapabilities,
  listApps,
  requestAppBuild,
} from "../../api/apps";
import { del } from "../../api/client";

/** A freshly-registered app shows up within a few seconds; poll while waiting. */
const APPS_POLL_MS = 4000;

/**
 * How long a "building" app may sit unpublished before we treat the build as
 * FAILED rather than still in progress. A real build publishes in ~6 min (warm
 * builds are seconds); past this it is not actually building anymore — the
 * agent stalled — so the operator should see a failure it can clear, not a
 * forever-spinning "building" row.
 */
const BUILD_STALL_MS = 10 * 60 * 1000;

/** App ids carry an `app_<hex>` prefix; the operator uses it to tell a real
 * built app apart from a mock tool id. */
export const APP_ID_PREFIX = "app_";

export function isRealAppId(id: string | null | undefined): boolean {
  return typeof id === "string" && id.startsWith(APP_ID_PREFIX);
}

export type AppBuildState = "ready" | "building" | "failed";

/**
 * Resolve an app's effective build state. "building" only while it is plausibly
 * still building; a building app whose pre-scaffold is older than BUILD_STALL_MS
 * has stalled and is reported "failed".
 */
export function appBuildState(
  app: CustomApp,
  now: number = Date.now(),
): AppBuildState {
  if (app.status !== "building") return "ready";
  const created = Date.parse(app.createdAt ?? "");
  if (Number.isFinite(created) && now - created > BUILD_STALL_MS) {
    return "failed";
  }
  return "building";
}

/**
 * List the workspace's built apps. Polls while any app is GENUINELY building
 * (not stalled/failed) so a freshly-published app appears without a reload, then
 * settles — a failed build no longer keeps the list polling.
 */
export function useOperatorApps() {
  return useQuery({
    queryKey: ["operator-apps"],
    queryFn: listApps,
    refetchInterval: (query) => {
      const apps = query.state.data ?? [];
      return apps.some((a) => appBuildState(a) === "building")
        ? APPS_POLL_MS
        : false;
    },
  });
}

/**
 * Load one app's manifest + sealed HTML. Polls while the app is genuinely
 * building; stops once it is ready OR has failed (a stalled build won't publish,
 * so polling it forever is pointless).
 */
export function useOperatorApp(id: string | null) {
  return useQuery({
    queryKey: ["operator-app", id],
    queryFn: () => getApp(id ?? ""),
    enabled: isRealAppId(id),
    refetchInterval: (query) => {
      const detail = query.state.data as CustomAppDetail | undefined;
      if (!detail) return APPS_POLL_MS;
      return appBuildState(detail.app) === "building" || !detail.html
        ? APPS_POLL_MS
        : false;
    },
  });
}

/**
 * Read the app's deterministic capability map (what it reads/writes), the real
 * basis for its Data tab. Only meaningful once the app is ready (an html-only or
 * still-building app returns an empty map), so it is gated on a real app id.
 */
export function useAppCapabilities(id: string | null) {
  return useQuery({
    queryKey: ["operator-app-capabilities", id],
    queryFn: () => getAppCapabilities(id ?? ""),
    enabled: isRealAppId(id),
  });
}

/** Kick off an app-builder build/improve. Returns the created Task. */
export function useBuildApp() {
  return useMutation({
    mutationFn: (req: AppBuildRequest) => requestAppBuild(req),
  });
}

/** Remove an app (used to clear a failed/stalled build). Authorized via the
 * App Builder writer path — the operator removes a failed build the same way the
 * builder that owns it would. */
export function useDeleteApp() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      del(`/apps/${encodeURIComponent(id)}`, undefined, {
        "X-WUPHF-Agent": "app-builder",
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["operator-apps"] }),
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
