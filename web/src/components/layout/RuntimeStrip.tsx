import { useQuery } from "@tanstack/react-query";

import { getOfficeTasks } from "../../api/tasks";
import { useOfficeMembers } from "../../hooks/useMembers";
import { useRequests } from "../../hooks/useRequests";

/**
 * Thin strip under the channel header with pills for "N active",
 * "M blocked", "K need you". Mirrors the legacy runtime-strip.
 *
 * Uses a separate cache key ("office-tasks-active") from TasksApp
 * ("office-tasks") because this component fetches with includeDone:false
 * while TasksApp fetches with includeDone:true. Sharing the same key
 * caused the two queries to overwrite each other's cached results.
 */
export function RuntimeStrip() {
  const { data: members = [] } = useOfficeMembers();
  const { data: tasksData } = useQuery({
    queryKey: ["office-tasks-active"],
    queryFn: () => getOfficeTasks({ includeDone: false }),
    refetchInterval: 15_000,
  });
  const { pending } = useRequests();

  const tasks = tasksData?.tasks ?? [];

  const active = members.filter((m) => {
    if (!m.slug || m.slug === "human" || m.slug === "you") return false;
    return (m.status || "").toLowerCase() === "active";
  }).length;

  const blocked = tasks.filter((t) => {
    const s = (t.status ?? "").toLowerCase();
    return s === "blocked" || t.blocked === true;
  }).length;

  const needYou = pending.filter((r) => r.blocking || r.required).length;

  if (active === 0 && blocked === 0 && needYou === 0) {
    return (
      <div className="runtime-strip">
        <span className="runtime-pill runtime-pill-idle">all quiet</span>
      </div>
    );
  }

  return (
    <div className="runtime-strip">
      {needYou > 0 && (
        <span className="runtime-pill runtime-pill-needyou">
          {needYou} need you
        </span>
      )}
      {active > 0 && (
        <span className="runtime-pill runtime-pill-active">
          {active} active
        </span>
      )}
      {blocked > 0 && (
        <span className="runtime-pill runtime-pill-blocked">
          {blocked} blocked
        </span>
      )}
    </div>
  );
}
