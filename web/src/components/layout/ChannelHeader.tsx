import { useEffect } from "react";

import { deriveBreadcrumbs } from "../../hooks/useObjectBreadcrumb";
import { useRecordRecentObject } from "../../hooks/useRecentObjects";
import { useChannels } from "../../hooks/useChannels";
import { appTitle } from "../../lib/constants";
import { useCurrentRoute } from "../../routes/useCurrentRoute";
import { useAppStore } from "../../stores/app";
import { Breadcrumb } from "./Breadcrumb";
import { ThemeSwitcher } from "./ThemeSwitcher";

function headerTitleAndDesc(
  route: ReturnType<typeof useCurrentRoute>,
  channels: { slug: string; description?: string }[],
): { title: string; desc: string } {
  switch (route.kind) {
    case "channel": {
      const ch = channels.find((c) => c.slug === route.channelSlug);
      return {
        title: `# ${route.channelSlug}`,
        desc: ch?.description || "",
      };
    }
    case "dm":
      return { title: `@${route.agentSlug}`, desc: "" };
    case "app":
      return { title: appTitle(route.appId), desc: "" };
    case "task-board":
    case "task-detail":
      return { title: appTitle("tasks"), desc: "" };
    case "wiki":
    case "wiki-article":
    case "wiki-lookup":
      return { title: appTitle("wiki"), desc: "" };
    case "notebook-catalog":
    case "notebook-agent":
    case "notebook-entry":
      return { title: "Notebooks", desc: "" };
    case "reviews":
      return { title: "Reviews", desc: "" };
    case "unknown":
      return { title: "", desc: "" };
    default: {
      const _exhaustive: never = route;
      void _exhaustive;
      return { title: "", desc: "" };
    }
  }
}

/**
 * Derive an ObjectRef for the current route to record in recent-objects.
 * Returns null for routes that aren't discrete navigable objects (channels,
 * wiki catalog, etc.) or for the "unknown" sentinel.
 */
function routeToObjectRef(
  route: ReturnType<typeof useCurrentRoute>,
): Parameters<ReturnType<typeof useRecordRecentObject>>[0] | null {
  switch (route.kind) {
    case "dm":
      return { kind: "agent", slug: route.agentSlug };
    case "task-detail":
      return { kind: "task", id: route.taskId };
    case "wiki-article":
      return { kind: "wiki-page", path: route.articlePath };
    case "app":
      if (route.appId === "settings") {
        return { kind: "settings-section", section: "workspace" };
      }
      if (
        route.appId === "providers" ||
        route.appId === "team" ||
        route.appId === "workspace" ||
        route.appId === "skills"
      ) {
        return { kind: "settings-section", section: route.appId as "providers" | "team" | "workspace" | "skills" };
      }
      return null;
    case "notebook-entry":
      return null; // notebook entries are draft surfaces, not canonical objects
    case "task-board":
    case "wiki":
    case "wiki-lookup":
    case "notebook-catalog":
    case "notebook-agent":
    case "reviews":
    case "channel":
    case "unknown":
      return null;
    default: {
      const _exhaustive: never = route;
      void _exhaustive;
      return null;
    }
  }
}

export function ChannelHeader() {
  const route = useCurrentRoute();
  const setSearchOpen = useAppStore((s) => s.setSearchOpen);
  const { data: channels = [] } = useChannels();
  const recordRecent = useRecordRecentObject();

  const { title, desc } = headerTitleAndDesc(route, channels);
  const breadcrumbItems = deriveBreadcrumbs(route);

  // Record navigations to discrete objects in the recent-objects list.
  useEffect(() => {
    const ref = routeToObjectRef(route);
    if (ref) {
      recordRecent(ref);
    }
  }, [route, recordRecent]);

  return (
    <div className="channel-header">
      <div style={{ display: "flex", alignItems: "center", gap: 10, minWidth: 0, flex: 1 }}>
        {breadcrumbItems.length > 0 ? (
          <Breadcrumb items={breadcrumbItems} />
        ) : (
          <>
            <span className="channel-title">{title}</span>
            {desc ? <span className="channel-desc">{desc}</span> : null}
          </>
        )}
      </div>
      <div className="channel-actions">
        <ThemeSwitcher />
        <button
          type="button"
          className="sidebar-btn"
          title="Search"
          aria-label="Search"
          onClick={() => setSearchOpen(true)}
        >
          <svg
            aria-hidden="true"
            focusable="false"
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <circle cx="11" cy="11" r="8" />
            <path d="m21 21-4.3-4.3" />
          </svg>
        </button>
      </div>
    </div>
  );
}
