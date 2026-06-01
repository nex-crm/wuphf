// biome-ignore-all lint/a11y/useAriaPropsSupportedByRole: Passive metadata uses accessible labels queried by screen-reader tests; visual text remains unchanged.
import type { ComponentType } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  BookStack,
  CheckCircle,
  ClipboardCheck,
  Flash,
  HomeSimple,
  Package,
  Page,
  Play,
  Repeat,
  Search,
  Settings,
  ShareAndroid,
  Shield,
  TaskList,
  Terminal,
} from "iconoir-react";

import { fetchReviews } from "../../api/notebook";
import { useOverflow } from "../../hooks/useOverflow";
import { navigateToSidebarApp } from "../../lib/sidebarNav";
import {
  SIDEBAR_TOOLS,
  WIKI_SURFACE_APP_IDS,
} from "../../routes/routeRegistry";
import { useCurrentApp } from "../../routes/useCurrentRoute";
import { SidebarItem } from "./SidebarItem";

// Notebooks and reviews render inside the Wiki app shell via tabs, so the
// 'Wiki' sidebar entry lights up for any of those three currentApp values.
const WIKI_SURFACE_APPS = new Set<string>(WIKI_SURFACE_APP_IDS);

const APP_ICONS: Record<string, ComponentType<{ className?: string }>> = {
  overview: HomeSimple,
  issues: ClipboardCheck,
  studio: Play,
  wiki: BookStack,
  console: Terminal,
  tasks: CheckCircle,
  requests: TaskList,
  graph: ShareAndroid,
  policies: Shield,
  routines: Repeat,
  skills: Flash,
  activity: Package,
  receipts: Page,
  "health-check": Search,
  settings: Settings,
};

export function AppList() {
  const currentApp = useCurrentApp();

  const { data: reviewsData } = useQuery({
    queryKey: ["reviews-badge"],
    queryFn: fetchReviews,
    refetchInterval: 15_000,
  });

  const pendingReviewsCount = (reviewsData ?? []).filter(
    (r) =>
      r.state === "pending" ||
      r.state === "in-review" ||
      r.state === "changes-requested",
  ).length;

  const overflowRef = useOverflow<HTMLDivElement>();

  return (
    <div className="sidebar-scroll-wrap is-apps">
      <div className="sidebar-apps" ref={overflowRef}>
        {SIDEBAR_TOOLS.filter((tool) => tool.id !== "settings").map((tool) => {
          let badge: number | null = null;
          if (tool.id === "wiki" && pendingReviewsCount > 0)
            badge = pendingReviewsCount;
          const Icon = APP_ICONS[tool.id];
          const isActive =
            tool.id === "wiki"
              ? WIKI_SURFACE_APPS.has(currentApp ?? "")
              : currentApp === tool.id;
          return (
            <SidebarItem
              key={tool.id}
              icon={
                Icon ? (
                  <Icon className="sidebar-item-icon" />
                ) : (
                  <span className="sidebar-item-emoji">{tool.icon}</span>
                )
              }
              label={tool.label}
              active={isActive}
              onClick={() => navigateToSidebarApp(tool.id)}
              badge={
                badge !== null ? (
                  <span
                    className="sidebar-badge"
                    aria-label={`${badge} pending`}
                  >
                    {badge}
                  </span>
                ) : undefined
              }
            />
          );
        })}
      </div>
    </div>
  );
}
