/**
 * useObjectBreadcrumb — derives a typed ObjectRef + resolved route from the
 * current URL-driven route shape. Returns null for route kinds that don't
 * map to a discrete object (channels, "unknown").
 *
 * Phase 5 PR 2 — app navigation refresh.
 */

import { resolveObjectRoute, type ObjectRouteResolution } from "../lib/objectRoutes";
import type { CurrentRoute } from "../routes/useCurrentRoute";

export interface BreadcrumbItem {
  /** User-visible label. */
  label: string;
  /** Canonical deep-link href (hash URL). */
  href: string;
}

/**
 * Derive up to two breadcrumb segments from the current route:
 *   [section, object]
 * e.g. ["Wiki", "Wiki: people/nazz"] or ["Agents", "Agent: gaia"].
 *
 * Returns an empty array for conversation routes (channels) and unknown.
 * Pure function so tests can call it without a React context.
 */
export function deriveBreadcrumbs(route: CurrentRoute): BreadcrumbItem[] {
  switch (route.kind) {
    case "dm": {
      const res = resolveObjectRoute({ kind: "agent", slug: route.agentSlug });
      return [
        { label: "Agents", href: "#/dm" },
        breadcrumbItem(res, `@${route.agentSlug}`),
      ];
    }
    case "task-board": {
      return [{ label: "Tasks", href: "#/tasks" }];
    }
    case "task-detail": {
      const res = resolveObjectRoute({ kind: "task", id: route.taskId });
      return [
        { label: "Tasks", href: "#/tasks" },
        breadcrumbItem(res, `Task ${route.taskId}`),
      ];
    }
    case "wiki": {
      return [{ label: "Wiki", href: "#/wiki" }];
    }
    case "wiki-article": {
      const res = resolveObjectRoute({
        kind: "wiki-page",
        path: route.articlePath,
      });
      return [
        { label: "Wiki", href: "#/wiki" },
        breadcrumbItem(res, route.articlePath),
      ];
    }
    case "wiki-lookup": {
      return [{ label: "Wiki", href: "#/wiki" }];
    }
    case "notebook-catalog": {
      return [{ label: "Notebooks", href: "#/notebooks" }];
    }
    case "notebook-agent": {
      return [
        { label: "Notebooks", href: "#/notebooks" },
        { label: route.agentSlug, href: `#/notebooks/${encodeURIComponent(route.agentSlug)}` },
      ];
    }
    case "notebook-entry": {
      return [
        { label: "Notebooks", href: "#/notebooks" },
        { label: route.agentSlug, href: `#/notebooks/${encodeURIComponent(route.agentSlug)}` },
        {
          label: route.entrySlug,
          href: `#/notebooks/${encodeURIComponent(route.agentSlug)}/${encodeURIComponent(route.entrySlug)}`,
        },
      ];
    }
    case "reviews": {
      return [{ label: "Reviews", href: "#/reviews" }];
    }
    case "app": {
      if (route.appId === "settings" || isSettingsSection(route.appId)) {
        const section = route.appId === "settings" ? "workspace" : route.appId;
        const res = resolveObjectRoute({ kind: "settings-section", section: section as "providers" | "team" | "workspace" | "skills" });
        return [{
          label: route.appId === "settings" ? "Settings" : (res.fallback ? appLabel(route.appId) : res.label),
          href: res.href,
        }];
      }
      // Generic app — one segment with the app title.
      return [{ label: appLabel(route.appId), href: `#/apps/${route.appId}` }];
    }
    case "channel":
    case "unknown":
      return [];
    default: {
      const _exhaustive: never = route;
      void _exhaustive;
      return [];
    }
  }
}

function breadcrumbItem(
  res: ObjectRouteResolution,
  fallbackLabel: string,
): BreadcrumbItem {
  return { label: res.fallback ? fallbackLabel : res.label, href: res.href };
}

/** Map an app id to a friendly label without importing SIDEBAR_APPS. */
function appLabel(appId: string): string {
  const LABELS: Record<string, string> = {
    console: "Console",
    tasks: "Tasks",
    requests: "Requests",
    graph: "Graph",
    policies: "Policies",
    calendar: "Calendar",
    skills: "Skills",
    activity: "Activity",
    receipts: "Receipts",
    "health-check": "Access & Health",
  };
  return LABELS[appId] ?? appId.replace(/-/g, " ").replace(/\b\w/g, (c) => c.toUpperCase());
}

type SettingsSection = "providers" | "team" | "workspace" | "skills";
const SETTINGS_SECTIONS = new Set<string>(["providers", "team", "workspace", "skills"]);
function isSettingsSection(v: string): v is SettingsSection {
  return SETTINGS_SECTIONS.has(v);
}
