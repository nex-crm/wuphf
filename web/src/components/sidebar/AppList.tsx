// biome-ignore-all lint/a11y/useAriaPropsSupportedByRole: Passive metadata uses accessible labels queried by screen-reader tests; visual text remains unchanged.
import { type ComponentType, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  BookStack,
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
import { navigateToSidebarApp } from "../../lib/sidebarNav";
import {
  SIDEBAR_TOOLS,
  WIKI_SURFACE_APP_IDS,
} from "../../routes/routeRegistry";
import { useCurrentApp } from "../../routes/useCurrentRoute";
import { InboxButton } from "./InboxButton";
import { SidebarItem } from "./SidebarItem";
import { SidebarSection } from "./SidebarSection";

// Notebooks and reviews render inside the Wiki app shell via tabs, so the
// 'Wiki' sidebar entry lights up for any of those three currentApp values.
const WIKI_SURFACE_APPS = new Set<string>(WIKI_SURFACE_APP_IDS);

const APP_ICONS: Record<string, ComponentType<{ className?: string }>> = {
  overview: HomeSimple,
  studio: Play,
  wiki: BookStack,
  console: Terminal,
  tasks: ClipboardCheck,
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

// The sidebar is three labeled groups. `inbox` is special — it renders via
// InboxButton (with its own attention badge); the rest are SIDEBAR_TOOLS ids.
// Order within each group is the display order. (The `routines` tool shows as
// "Scheduled Tasks" via APP_LABELS.)
const NAV_SECTIONS: ReadonlyArray<{
  label: string;
  items: readonly string[];
}> = [
  {
    label: "Work",
    items: ["inbox", "activity", "tasks", "routines", "console", "receipts"],
  },
  { label: "Knowledge", items: ["wiki", "graph"] },
  {
    label: "Config",
    items: ["agents", "policies", "skills", "integrations", "health-check"],
  },
];

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

  const [open, setOpen] = useState<Record<string, boolean>>({
    Work: true,
    Knowledge: true,
    Config: true,
  });
  const toggle = (label: string) =>
    setOpen((prev) => ({ ...prev, [label]: !prev[label] }));

  const toolById = new Map(SIDEBAR_TOOLS.map((tool) => [tool.id, tool]));

  function renderItem(id: string) {
    if (id === "inbox") return <InboxButton key="inbox" />;
    const tool = toolById.get(id as (typeof SIDEBAR_TOOLS)[number]["id"]);
    if (!tool) return null;
    const Icon = APP_ICONS[id];
    const isActive =
      id === "wiki"
        ? WIKI_SURFACE_APPS.has(currentApp ?? "")
        : currentApp === id;
    const badge =
      id === "wiki" && pendingReviewsCount > 0 ? pendingReviewsCount : null;
    return (
      <SidebarItem
        key={id}
        icon={
          Icon ? (
            <Icon className="sidebar-item-icon" />
          ) : (
            <span className="sidebar-item-emoji">{tool.icon}</span>
          )
        }
        label={tool.label}
        active={isActive}
        onClick={() => navigateToSidebarApp(id)}
        badge={
          badge !== null ? (
            <span className="sidebar-badge" aria-label={`${badge} pending`}>
              {badge}
            </span>
          ) : undefined
        }
      />
    );
  }

  return (
    <div className="sidebar-scroll-wrap is-apps">
      {NAV_SECTIONS.map((section) => (
        <SidebarSection
          key={section.label}
          label={section.label}
          open={open[section.label] ?? true}
          onToggle={() => toggle(section.label)}
          data-testid={`sidebar-section-${section.label.toLowerCase()}`}
        >
          <div className="sidebar-apps">{section.items.map(renderItem)}</div>
        </SidebarSection>
      ))}
    </div>
  );
}
