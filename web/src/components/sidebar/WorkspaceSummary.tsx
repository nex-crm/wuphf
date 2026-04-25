import { useQuery } from "@tanstack/react-query";

import { getOfficeTasks, getUsage } from "../../api/client";
import { useOfficeMembers } from "../../hooks/useMembers";

function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return String(n);
}

/**
 * Small status line at the bottom of the sidebar. Mirrors the legacy
 * `renderWorkspaceSummary` output: active agents, open tasks, total tokens.
 */
export function WorkspaceSummary() {
  const { data: members = [] } = useOfficeMembers();
  const { data: tasksData } = useQuery({
    queryKey: ["office-tasks"],
    queryFn: () => getOfficeTasks({ includeDone: false }),
    refetchInterval: 30_000,
  });
  const { data: usage } = useQuery({
    queryKey: ["usage"],
    queryFn: () => getUsage(),
    refetchInterval: 30_000,
  });

  const activeAgents = members.filter((m) => {
    if (!m.slug || m.slug === "human" || m.slug === "you") return false;
    return (m.status || "").toLowerCase() === "active";
  }).length;

  const openTasks = (tasksData?.tasks ?? []).filter((t) => {
    const s = (t.status || "").toLowerCase();
    return s && s !== "done" && s !== "completed";
  }).length;

  const parts: string[] = [
    `${activeAgents} agent${activeAgents === 1 ? "" : "s"} active`,
    `${openTasks} task${openTasks === 1 ? "" : "s"} open`,
  ];
  const total = usage?.total?.total_tokens ?? 0;
  if (total > 0) parts.push(`${formatTokens(total)} tokens`);

  const hint =
    openTasks > 0
      ? `${openTasks} task${openTasks === 1 ? "" : "s"} in progress`
      : "Type / for commands";

  return (
    <>
      <div className="sidebar-summary">{parts.join(", ")}</div>
      <div className="sidebar-hint">{hint}</div>
    </>
  );
}
