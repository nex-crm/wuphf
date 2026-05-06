/**
 * Typed object route registry — Phase 5 PR 1.
 *
 * Single source of truth for "given a domain object, where does it open
 * in the app and what should we label it?" Today only one call-site
 * uses this (WikiLink). Future Phase 5 PRs (command palette,
 * breadcrumbs, profile pages, calendar click handlers) will all consume
 * the same registry so navigation cannot drift between surfaces.
 *
 * Pure functions only — no React, no router imports, no side effects.
 * Hash routes match the live shape produced by `createHashHistory()` in
 * `lib/router.ts`.
 */

export type SettingsSection = "providers" | "team" | "workspace" | "skills";

export type ObjectRef =
  | { kind: "agent"; slug: string }
  | { kind: "run"; id: string }
  | { kind: "task"; id: string; agentSlug?: string }
  | { kind: "wiki-page"; path: string }
  | { kind: "workbench-item"; id: string; itemKind: string }
  | { kind: "artifact"; id: string }
  | { kind: "settings-section"; section: SettingsSection };

export type ObjectRouteFallbackReason = "unknown_kind" | "missing_id";

export interface ObjectRouteAppAction {
  app: string;
  channel?: string;
}

export interface ObjectRouteFallback {
  reason: ObjectRouteFallbackReason;
  message: string;
}

export interface ObjectRouteResolution {
  href: string;
  label: string;
  appAction?: ObjectRouteAppAction;
  fallback?: ObjectRouteFallback;
}

const FALLBACK_HREF = "#/";

function missingIdFallback(
  field: string,
  kind: ObjectRef["kind"] | string,
): ObjectRouteResolution {
  return {
    href: FALLBACK_HREF,
    label: `Unknown ${kind}`,
    fallback: {
      reason: "missing_id",
      message: `Cannot resolve ${kind} route: ${field} is empty.`,
    },
  };
}

function settingsLabel(section: SettingsSection): string {
  switch (section) {
    case "providers":
      return "Settings: Providers";
    case "team":
      return "Settings: Team";
    case "workspace":
      return "Settings: Workspace";
    case "skills":
      return "Settings: Skills";
  }
}

export function resolveObjectRoute(ref: ObjectRef): ObjectRouteResolution {
  switch (ref.kind) {
    case "agent": {
      if (!ref.slug) return missingIdFallback("slug", "agent");
      return {
        href: `#/dm/${encodeURIComponent(ref.slug)}`,
        label: `Agent: ${ref.slug}`,
        appAction: { app: "dm", channel: ref.slug },
      };
    }
    case "run": {
      if (!ref.id) return missingIdFallback("id", "run");
      // Runs do not have a dedicated route yet; surface them through
      // the activity app so links remain valid until run pages land.
      return {
        href: `#/apps/activity?run=${encodeURIComponent(ref.id)}`,
        label: `Run: ${ref.id}`,
        appAction: { app: "activity" },
      };
    }
    case "task": {
      if (!ref.id) return missingIdFallback("id", "task");
      return {
        href: `#/tasks/${encodeURIComponent(ref.id)}`,
        label: `Task: ${ref.id}`,
        appAction: { app: "tasks" },
      };
    }
    case "wiki-page": {
      if (!ref.path) return missingIdFallback("path", "wiki-page");
      // Match the existing wiki link contract: encodeURI preserves the
      // path separators inside the slug (e.g. `people/nazz`).
      return {
        href: `#/wiki/${encodeURI(ref.path)}`,
        label: `Wiki: ${ref.path}`,
        appAction: { app: "wiki" },
      };
    }
    case "workbench-item": {
      if (!ref.id) return missingIdFallback("id", "workbench-item");
      // Workbench items live under tasks today; pass the item kind
      // through so future surfaces can route by kind without a second
      // lookup.
      return {
        href: `#/tasks/${encodeURIComponent(ref.id)}`,
        label: `Workbench ${ref.itemKind}: ${ref.id}`,
        appAction: { app: "tasks" },
      };
    }
    case "artifact": {
      if (!ref.id) return missingIdFallback("id", "artifact");
      return {
        href: `#/apps/receipts?artifact=${encodeURIComponent(ref.id)}`,
        label: `Artifact: ${ref.id}`,
        appAction: { app: "receipts" },
      };
    }
    case "settings-section": {
      return {
        href: `#/apps/settings?section=${encodeURIComponent(ref.section)}`,
        label: settingsLabel(ref.section),
        appAction: { app: "settings" },
      };
    }
  }
}

/**
 * For object kinds that arrive from the server at runtime and do not
 * match the typed union. Surfaces (palette, breadcrumbs) should render
 * the fallback instead of crashing or fabricating a URL.
 */
export function resolveUnknownObjectRoute(kind: string): ObjectRouteResolution {
  return {
    href: FALLBACK_HREF,
    label: `Unknown object: ${kind || "(missing kind)"}`,
    fallback: {
      reason: "unknown_kind",
      message: `Object kind "${kind}" is not supported by the route registry.`,
    },
  };
}
