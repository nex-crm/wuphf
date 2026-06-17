import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Plus, Tools } from "iconoir-react";

import { listApps } from "../../api/apps";
import { navigateToSidebarApp } from "../../lib/sidebarNav";
import { useCurrentApp } from "../../routes/useCurrentRoute";
import { useAppStore } from "../../stores/app";
import { SidebarItem } from "./SidebarItem";
import { SidebarSection } from "./SidebarSection";

const BUILD_TTL_MS = 5 * 60_000;

/**
 * AppsSection lists the office's agent-generated internal tools (Apps), any
 * in-flight builds, and the "Create app" affordance. Data-driven from GET /apps;
 * the optimistic "building…" rows close the dead air between submitting a new app
 * and it appearing (a build takes 20-60s).
 */
export function AppsSection() {
  const currentApp = useCurrentApp();
  const openCreateAppDialog = useAppStore((s) => s.openCreateAppDialog);
  const appBuilds = useAppStore((s) => s.appBuilds);
  const clearAppBuilding = useAppStore((s) => s.clearAppBuilding);
  const [open, setOpen] = useState(true);

  const { data: apps } = useQuery({
    queryKey: ["apps"],
    queryFn: listApps,
    refetchInterval: 15_000,
  });
  const items = apps ?? [];

  // Published apps are clickable; pre-scaffolded drafts (status "building") show
  // as a real building row instead — their live preview is in the build task.
  const readyItems = useMemo(
    () => items.filter((a) => a.status !== "building"),
    [items],
  );
  const buildingNames = useMemo(
    () => items.filter((a) => a.status === "building").map((a) => a.name),
    [items],
  );

  const readyNames = useMemo(
    () => new Set(readyItems.map((a) => a.name.trim().toLowerCase())),
    [readyItems],
  );
  const buildingKeys = useMemo(
    () => new Set(buildingNames.map((n) => n.trim().toLowerCase())),
    [buildingNames],
  );

  // An optimistic note (added on submit) is superseded once a real draft or
  // published app of that name exists, or it expires after the TTL.
  const pending = useMemo(() => {
    const now = Date.now();
    return Object.entries(appBuilds)
      .filter(
        ([key, b]) =>
          !(readyNames.has(key) || buildingKeys.has(key)) &&
          now - b.startedAt <= BUILD_TTL_MS,
      )
      .map(([, b]) => b.name);
  }, [appBuilds, readyNames, buildingKeys]);

  // The full set of in-flight builds: real pre-scaffolded drafts first, then any
  // optimistic notes not yet backed by a draft.
  const building = useMemo(
    () => [...buildingNames, ...pending],
    [buildingNames, pending],
  );

  useEffect(() => {
    const now = Date.now();
    for (const [key, b] of Object.entries(appBuilds)) {
      if (
        readyNames.has(key) ||
        buildingKeys.has(key) ||
        now - b.startedAt > BUILD_TTL_MS
      ) {
        clearAppBuilding(b.name);
      }
    }
  }, [appBuilds, readyNames, buildingKeys, clearAppBuilding]);

  return (
    <SidebarSection
      label="Apps"
      open={open}
      onToggle={() => setOpen((prev) => !prev)}
      data-testid="sidebar-section-apps"
    >
      <div className="sidebar-apps">
        {readyItems.map((app) => (
          <SidebarItem
            key={app.id}
            icon={
              app.icon ? (
                <span className="sidebar-item-emoji">{app.icon}</span>
              ) : (
                <Tools className="sidebar-item-icon" />
              )
            }
            label={app.name}
            active={currentApp === app.id}
            onClick={() => navigateToSidebarApp(app.id)}
          />
        ))}
        {building.map((name) => (
          <div
            key={`building-${name.toLowerCase()}`}
            className="sidebar-app-building"
            aria-live="polite"
          >
            <span
              className="sidebar-app-building__spinner"
              aria-hidden="true"
            />
            <span className="sidebar-app-building__label">{name}</span>
            <span className="sidebar-app-building__meta">building…</span>
          </div>
        ))}
        <SidebarItem
          icon={<Plus className="sidebar-item-icon" />}
          label="Create app"
          active={false}
          onClick={() => openCreateAppDialog()}
        />
      </div>
    </SidebarSection>
  );
}
