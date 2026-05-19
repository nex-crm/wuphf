// biome-ignore-all lint/a11y/useAriaPropsSupportedByRole: Badge mirrors AppList — aria-label on the span surfaces the pending count to assistive tech.
import { useEffect, useMemo, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import { Mail } from "iconoir-react";

import { getInboxItems } from "../../api/lifecycle";
import { playInboxDing } from "../../lib/notificationSound";
import { navigateToSidebarApp } from "../../lib/sidebarNav";
import type { InboxItem } from "../../lib/types/inbox";
import { useCurrentApp } from "../../routes/useCurrentRoute";

/**
 * Lifecycle states a human can act on. A task in any of these surfaces in
 * the unread badge because the agent has handed off (or wants a call).
 * Drives `count` below so the sidebar badge actually reflects the real
 * attention queue, not just the narrower `decisionRequired` slice the
 * old /tasks/inbox payload exposed.
 */
const ATTENTION_TASK_STATES = new Set([
  "decision",
  "review",
  "changes_requested",
  "blocked_on_pr_merge",
]);

function isAttentionItem(item: InboxItem): boolean {
  if (item.kind === "request" || item.kind === "review") return true;
  if (item.kind === "task") {
    const state = item.task?.state ?? "";
    return ATTENTION_TASK_STATES.has(state);
  }
  return false;
}

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
    queryFn: () => getInboxItems("all"),
    refetchInterval: 5_000,
  });

  const count = useMemo(() => {
    const items = data?.items ?? [];
    let total = 0;
    for (const item of items) {
      if (isAttentionItem(item)) total += 1;
    }
    return total;
  }, [data]);

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
