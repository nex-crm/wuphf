import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { OpenNewWindow } from "iconoir-react";

import { type CustomApp, getApp, listApps } from "../../api/apps";
import { router } from "../../lib/router";
import { AppLivePreview } from "./AppLivePreview";

// Poll briefly while a build is in flight so a freshly-registered app (or a new
// version) shows up in the preview within a few seconds, no reload needed.
const PREVIEW_POLL_MS = 4000;

/**
 * Parse the app name out of an App Builder task title. Both the slash-command
 * path (api/apps.ts) and the broker proposal path (broker_apps_proposal.go)
 * format the title as "<verb> app: <name>", so this is a reliable correlation
 * without a wire-shape change. Returns null when the title isn't an app task.
 */
export function parseAppNameFromTaskTitle(title: string): string | null {
  const match = title.match(
    /^\s*(?:build|improve|create|update)\s+app:\s*(.+?)\s*$/i,
  );
  return match ? match[1] : null;
}

/** Pick the app a build task is producing: exact name match, newest wins. */
export function resolveAppForTask(
  apps: CustomApp[],
  appName: string | null,
): CustomApp | undefined {
  if (!appName) return undefined;
  const wanted = appName.trim().toLowerCase();
  const matches = apps.filter((a) => a.name.trim().toLowerCase() === wanted);
  if (matches.length === 0) return undefined;
  return matches.sort((a, b) =>
    (b.updatedAt ?? "").localeCompare(a.updatedAt ?? ""),
  )[0];
}

interface AppBuildPreviewProps {
  /** The task title — the app name is parsed from it. */
  taskTitle: string;
}

/**
 * AppBuildPreview is the live preview pane shown beside the chat on an App
 * Builder task. It resolves the app the task is building (by name) and renders
 * it in the same hardened sandbox as the full app surface, refreshing as new
 * versions are published. Until the first version lands it shows a building
 * placeholder, so the 20–60s build is never dead air.
 */
export function AppBuildPreview({ taskTitle }: AppBuildPreviewProps) {
  const appName = useMemo(
    () => parseAppNameFromTaskTitle(taskTitle),
    [taskTitle],
  );

  const appsQuery = useQuery({
    queryKey: ["apps"],
    queryFn: listApps,
    refetchInterval: PREVIEW_POLL_MS,
  });

  const app = useMemo(
    () => resolveAppForTask(appsQuery.data ?? [], appName),
    [appsQuery.data, appName],
  );

  const detail = useQuery({
    queryKey: ["app", app?.id],
    queryFn: () => getApp(app?.id ?? ""),
    enabled: Boolean(app?.id),
    refetchInterval: PREVIEW_POLL_MS,
  });

  if (!(app && detail.data)) {
    return (
      <section className="app-build-preview" aria-label="App preview">
        <div className="app-build-preview__header">
          <span className="app-build-preview__title">Preview</span>
        </div>
        <div className="app-build-preview__state" role="status">
          <span className="app-build-preview__spinner" aria-hidden="true" />
          <p className="app-build-preview__state-title">
            {appName ? `Building ${appName}…` : "Waiting for the App Builder…"}
          </p>
          <p className="app-build-preview__state-detail">
            The live preview appears here the moment the App Builder publishes
            its first version.
          </p>
        </div>
      </section>
    );
  }

  const { app: meta } = detail.data;

  return (
    <section className="app-build-preview" aria-label="App preview">
      <div className="app-build-preview__header">
        <span className="app-build-preview__icon" aria-hidden="true">
          {meta.icon || "🧩"}
        </span>
        <span className="app-build-preview__title" title={meta.name}>
          {meta.name}
        </span>
        <span className="app-build-preview__live">
          <span className="app-build-preview__live-dot" aria-hidden="true" />
          Live
        </span>
        <span
          className="app-build-preview__version"
          title={`Updated ${meta.updatedAt}`}
        >
          v{meta.version}
        </span>
        <button
          type="button"
          className="app-build-preview__open"
          aria-label="Open app full screen"
          title="Open full screen"
          onClick={() =>
            void router.navigate({
              to: "/apps/$appId",
              params: { appId: meta.id },
            })
          }
        >
          <OpenNewWindow width={14} height={14} />
        </button>
      </div>
      <div className="app-build-preview__frame">
        <AppLivePreview appId={meta.id} title={meta.name} />
      </div>
    </section>
  );
}
