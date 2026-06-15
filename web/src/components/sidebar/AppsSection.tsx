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

  const realNames = useMemo(
    () => new Set(items.map((a) => a.name.trim().toLowerCase())),
    [items],
  );

  // A pending build resolves when its app appears in the list, or expires after
  // the TTL (the build failed or the optimistic note outlived it).
  const pending = useMemo(() => {
    const now = Date.now();
    return Object.entries(appBuilds)
      .filter(
        ([key, b]) => !realNames.has(key) && now - b.startedAt <= BUILD_TTL_MS,
      )
      .map(([, b]) => b.name);
  }, [appBuilds, realNames]);

  useEffect(() => {
    const now = Date.now();
    for (const [key, b] of Object.entries(appBuilds)) {
      if (realNames.has(key) || now - b.startedAt > BUILD_TTL_MS) {
        clearAppBuilding(b.name);
      }
    }
  }, [appBuilds, realNames, clearAppBuilding]);

  return (
    <SidebarSection
      label="Apps"
      open={open}
      onToggle={() => setOpen((prev) => !prev)}
      data-testid="sidebar-section-apps"
    >
      <div className="sidebar-apps">
        {items.map((app) => (
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
        {pending.map((name) => (
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
