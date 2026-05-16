// biome-ignore-all lint/a11y/useAriaPropsSupportedByRole: Badge mirrors AppList — aria-label on the span surfaces the pending count to assistive tech.
import { useEffect, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import { MailIn } from "iconoir-react";

import { getInboxPayload } from "../../api/lifecycle";
import { playInboxDing } from "../../lib/notificationSound";
import { navigateToSidebarApp } from "../../lib/sidebarNav";
import { useCurrentApp } from "../../routes/useCurrentRoute";

/**
 * Top-of-sidebar Inbox entry. Renders the same DOM/class set as every
 * other sidebar app (AppList) so it visually belongs in the rail; the
 * surrounding `.sidebar-primary` wrapper handles the separator that
 * elevates it above the Agents / Channels / Tools sections.
 */
export function InboxButton() {
  const currentApp = useCurrentApp();
  const { data } = useQuery({
    queryKey: ["inbox-badge"],
    queryFn: getInboxPayload,
    refetchInterval: 5_000,
  });

  const count = data?.counts.decisionRequired ?? 0;

  const lastCountRef = useRef<number | null>(null);
  useEffect(() => {
    const prev = lastCountRef.current;
    if (prev !== null && count > prev) {
      playInboxDing();
    }
    lastCountRef.current = count;
  }, [count]);

  const isActive = currentApp === "inbox";

  return (
    <button
      type="button"
      className={`sidebar-item${isActive ? " active" : ""}`}
      onClick={() => navigateToSidebarApp("inbox")}
    >
      <MailIn className="sidebar-item-icon" />
      <span style={{ flex: 1 }}>Inbox</span>
      {count > 0 ? (
        <span className="sidebar-badge" aria-label={`${count} pending`}>
          {count}
        </span>
      ) : null}
    </button>
  );
}
