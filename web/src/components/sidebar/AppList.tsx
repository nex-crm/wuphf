// biome-ignore-all lint/a11y/useAriaPropsSupportedByRole: Passive metadata uses accessible labels queried by screen-reader tests; visual text remains unchanged.
import type { ComponentType } from "react";
import { useEffect, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  BellNotification,
  BookStack,
  Calendar,
  CheckCircle,
  ClipboardCheck,
  Flash,
  Package,
  Page,
  Play,
  Search,
  Settings,
  ShareAndroid,
  Shield,
  Terminal,
} from "iconoir-react";

import { getRequests } from "../../api/client";
import { getInboxPayload } from "../../api/lifecycle";
import { fetchReviews } from "../../api/notebook";
import { useOverflow } from "../../hooks/useOverflow";
import { SIDEBAR_APPS } from "../../lib/constants";
import { playInboxDing } from "../../lib/notificationSound";
import { navigateToSidebarApp } from "../../lib/sidebarNav";
import { WIKI_SURFACE_APP_IDS } from "../../routes/routeRegistry";
import {
  useCurrentApp,
  useFallbackChannelSlug,
} from "../../routes/useCurrentRoute";

// Notebooks and reviews render inside the Wiki app shell via tabs, so the
// 'Wiki' sidebar entry lights up for any of those three currentApp values.
const WIKI_SURFACE_APPS = new Set<string>(WIKI_SURFACE_APP_IDS);

const APP_ICONS: Record<string, ComponentType<{ className?: string }>> = {
  studio: Play,
  inbox: BellNotification,
  wiki: BookStack,
  console: Terminal,
  tasks: CheckCircle,
  requests: ClipboardCheck,
  graph: ShareAndroid,
  policies: Shield,
  calendar: Calendar,
  skills: Flash,
  activity: Package,
  receipts: Page,
  "health-check": Search,
  settings: Settings,
};

export function AppList() {
  const currentApp = useCurrentApp();
  // The Requests badge uses the channel-scoped /requests endpoint. Read
  // the last-visited channel here so the badge reflects the user's
  // working channel even while they're parked on a non-conversation
  // surface (apps, wiki, notebooks).
  const currentChannel = useFallbackChannelSlug();

  const { data: requestsData } = useQuery({
    queryKey: ["requests-badge", currentChannel],
    queryFn: () => getRequests(currentChannel),
    refetchInterval: 5_000,
  });

  const { data: reviewsData } = useQuery({
    queryKey: ["reviews-badge"],
    queryFn: fetchReviews,
    refetchInterval: 15_000,
  });

  const { data: inboxData } = useQuery({
    queryKey: ["inbox-badge"],
    queryFn: getInboxPayload,
    refetchInterval: 5_000,
  });

  const pendingCount = (requestsData?.requests ?? []).filter(
    (r) => !r.status || r.status === "open" || r.status === "pending",
  ).length;

  const pendingReviewsCount = (reviewsData ?? []).filter(
    (r) =>
      r.state === "pending" ||
      r.state === "in-review" ||
      r.state === "changes-requested",
  ).length;

  const inboxCount = inboxData?.counts.decisionRequired ?? 0;

  // Ding when the decision-required count strictly increases. The first
  // observation seeds the ref without playing (avoids a ding on mount).
  const lastInboxCountRef = useRef<number | null>(null);
  useEffect(() => {
    const prev = lastInboxCountRef.current;
    if (prev !== null && inboxCount > prev) {
      playInboxDing();
    }
    lastInboxCountRef.current = inboxCount;
  }, [inboxCount]);

  const overflowRef = useOverflow<HTMLDivElement>();

  return (
    <div className="sidebar-scroll-wrap is-apps">
      <div className="sidebar-apps" ref={overflowRef}>
        {SIDEBAR_APPS.filter((app) => app.id !== "settings").map((app) => {
          let badge: number | null = null;
          if (app.id === "requests" && pendingCount > 0) badge = pendingCount;
          if (app.id === "wiki" && pendingReviewsCount > 0)
            badge = pendingReviewsCount;
          if (app.id === "inbox" && inboxCount > 0) badge = inboxCount;
          const Icon = APP_ICONS[app.id];
          const isActive =
            app.id === "wiki"
              ? WIKI_SURFACE_APPS.has(currentApp ?? "")
              : currentApp === app.id;
          return (
            <button
              type="button"
              key={app.id}
              className={`sidebar-item${isActive ? " active" : ""}`}
              onClick={() => navigateToSidebarApp(app.id)}
            >
              {Icon ? (
                <Icon className="sidebar-item-icon" />
              ) : (
                <span className="sidebar-item-emoji">{app.icon}</span>
              )}
              <span style={{ flex: 1 }}>{app.name}</span>
              {badge !== null && (
                <span className="sidebar-badge" aria-label={`${badge} pending`}>
                  {badge}
                </span>
              )}
            </button>
          );
        })}
      </div>
    </div>
  );
}
