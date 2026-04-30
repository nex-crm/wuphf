/**
 * Per-workspace status pill — top-bar surface that shows the active
 * workspace's name plus a cumulative-cost or tokens counter.
 *
 * The cost number is read from each broker's existing `/usage` endpoint;
 * the SPA only ever asks its own broker, so this Just Works under the
 * page-reload-on-switch architecture (no peer-token gymnastics).
 *
 * Refresh cadence: every 5 minutes (per design doc — keeps load light at
 * ~12 calls/hour while staying fresh enough for cost-sensitive users).
 */
import { useQuery } from "@tanstack/react-query";

import { getUsage, type UsageData } from "../../api/client";
import { useWorkspacesList, type Workspace } from "../../api/workspaces";

const FIVE_MINUTES_MS = 5 * 60 * 1000;

interface StatusPillProps {
  /** Optional override — primarily for tests and storybook. */
  workspaceName?: string;
  /** Optional override — primarily for tests and storybook. */
  usage?: UsageData;
}

function formatTokens(total?: number): string {
  if (!total || total <= 0) return "0";
  if (total >= 1_000_000) {
    return `${(total / 1_000_000).toFixed(1)}M`;
  }
  if (total >= 1_000) {
    return `${(total / 1_000).toFixed(1)}k`;
  }
  return String(total);
}

function pickActiveName(
  workspaces: readonly Workspace[] | undefined,
  active?: string,
  override?: string,
): string {
  if (override) return override;
  if (active) return active;
  const fallback = workspaces?.find((w) => w.is_active) ?? workspaces?.[0];
  return fallback?.name ?? "main";
}

/**
 * Renders something like: "📁 main · 12.4k tokens today"
 *
 * Click target is intentionally non-interactive in v1 — the full
 * workspace switcher lives in the WorkspaceRail. The pill is surfacing
 * cost so the user knows where the spend lands.
 */
export function StatusPill({
  workspaceName,
  usage: usageOverride,
}: StatusPillProps = {}) {
  const wsQuery = useWorkspacesList({
    // The pill renders inside the persistent shell — keep it cheap.
    refetchInterval: FIVE_MINUTES_MS,
    staleTime: FIVE_MINUTES_MS,
  });
  const usageQuery = useQuery<UsageData>({
    queryKey: ["usage", "workspace-pill"],
    queryFn: () => getUsage(),
    refetchInterval: FIVE_MINUTES_MS,
    staleTime: FIVE_MINUTES_MS,
    enabled: !usageOverride,
  });

  const usage = usageOverride ?? usageQuery.data;
  const name = pickActiveName(
    wsQuery.data?.workspaces,
    wsQuery.data?.active,
    workspaceName,
  );
  const totalTokens =
    usage?.session?.total_tokens ?? usage?.total?.total_tokens ?? 0;

  // While the usage query is in-flight and no override is supplied, render an
  // em-dash placeholder instead of "0 tokens today" — the zero is briefly
  // misleading because the broker has not yet replied. Once usage data is
  // available the counter switches to the real number. The override path
  // (tests/storybook) skips this entirely so existing assertions stay stable.
  const usagePending = !usageOverride && usageQuery.data === undefined;

  return (
    <span
      className="status-bar-item workspace-pill"
      data-testid="workspace-status-pill"
      title={`Active workspace: ${name}`}
    >
      <span aria-hidden="true">📁</span>{" "}
      <span className="workspace-pill-name">{name}</span>
      <span className="status-bar-sep"> · </span>
      <span className="workspace-pill-cost">
        {usagePending ? "— tokens today" : `${formatTokens(totalTokens)} tokens today`}
      </span>
    </span>
  );
}
