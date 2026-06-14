// biome-ignore-all lint/a11y/useAriaPropsSupportedByRole: Badge mirrors AppList — aria-label on the span surfaces the pending count to assistive tech.
import { useEffect, useRef } from "react";
import { Mail } from "iconoir-react";

import { useOfficeStats } from "../../hooks/useOfficeStats";
import { playInboxDing } from "../../lib/notificationSound";
import { navigateToSidebarApp } from "../../lib/sidebarNav";
import { useCurrentApp } from "../../routes/useCurrentRoute";

/**
 * Top-of-sidebar Inbox entry. Renders the same DOM/class set as every
 * other sidebar app (AppList) so it visually belongs in the rail; the
 * surrounding `.sidebar-primary` wrapper handles the separator that
 * elevates it above the Agents / Channels / Tools sections.
 *
 * The badge count is the broker-computed `inbox_attention` from the
 * shared /office/stats payload — requests + reviews + tasks in a
 * human-attention lifecycle state, the same fan-out /inbox/items
 * serves. Reading the shared stats hook (instead of re-deriving from a
 * private /inbox/items poll) keeps this badge consistent with the
 * board's Needs-human lane and the header strip by construction.
 */
export function InboxButton() {
  const currentApp = useCurrentApp();
  const { data: stats } = useOfficeStats();
  const count = stats?.inbox_attention ?? 0;

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
      <Mail className="sidebar-item-icon" />
      <span style={{ flex: 1 }}>Inbox</span>
      {count > 0 ? (
        <span
          className="sidebar-badge"
          aria-label={`${count} pending`}
          data-testid="inbox-unread-badge"
        >
          {count}
        </span>
      ) : null}
    </button>
  );
}
