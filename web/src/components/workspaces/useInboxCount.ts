import { useEffect, useMemo, useRef } from "react";
import { useQuery } from "@tanstack/react-query";

import { getInboxItems } from "../../api/lifecycle";
import { playInboxDing } from "../../lib/notificationSound";
import type { InboxItem } from "../../lib/types/inbox";

const ATTENTION_TASK_STATES = new Set([
  "decision",
  "review",
  "changes_requested",
  "blocked_on_pr_merge",
]);

/**
 * Lifecycle states a human can act on. A task in any of these surfaces in
 * the rail's Inbox unread badge because the agent has handed off
 * (or wants a call).
 */
function isAttentionItem(item: InboxItem): boolean {
  if (item.kind === "request" || item.kind === "review") return true;
  if (item.kind === "task") {
    const state = item.task?.state ?? "";
    return ATTENTION_TASK_STATES.has(state);
  }
  return false;
}

/**
 * Counts unread "needs human attention" items in the inbox. Also plays
 * the inbox-ding sound when the count strictly grows so the user gets a
 * passive cue even when the rail is off-screen.
 */
export function useInboxCount(): number {
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
    if (prev !== null && count > prev) playInboxDing();
    lastCountRef.current = count;
  }, [count]);
  return count;
}
