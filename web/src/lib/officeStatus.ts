import type { OfficeMember } from "../api/client";
import type { Task } from "../api/tasks";
import { formatRelativeTime } from "./format";

/**
 * Canonical status normalizer shared between OfficeOverviewApp and
 * ArtifactsApp. Both files previously carried their own copy.
 */
export function normalizeStatus(raw: string): string {
  const s = raw.toLowerCase().replace(/[\s-]+/g, "_");
  if (s === "completed") return "done";
  if (s === "in_review") return "review";
  if (s === "cancelled") return "canceled";
  return s;
}

/**
 * Returns the display state and label for an active agent.
 * Only call on members that have already passed `isAgentActive`.
 */
export function classifyMember(
  member: OfficeMember,
): { state: "shipping" | "plotting"; label: string } {
  if (member.status === "plotting") {
    return { state: "plotting", label: "Plotting" };
  }
  return { state: "shipping", label: "Shipping" };
}

/**
 * True when the member is an agent (not the human seat) and is visibly
 * working — shipping, plotting, or has a live task string.
 */
export function isAgentActive(member: OfficeMember): boolean {
  return (
    member.slug !== "human" &&
    member.slug !== "you" &&
    (member.status === "shipping" ||
      member.status === "plotting" ||
      Boolean(member.task))
  );
}

/** Builds the channel · owner · relative-time meta string for a task card. */
export function taskMeta(t: Task): string {
  return (
    [
      t.channel ? `#${t.channel}` : "",
      t.owner ? `@${t.owner}` : "",
      t.updated_at ? formatRelativeTime(t.updated_at) : "",
    ]
      .filter(Boolean)
      .join(" · ") || ""
  );
}
