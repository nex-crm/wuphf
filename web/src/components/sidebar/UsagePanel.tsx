import { useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { getUsage } from "../../api/platform";
import { formatTokens, formatUSD } from "../../lib/format";

/**
 * Poll cadence for the usage aggregate. The popover refreshes faster
 * while open (the user is watching the meter); the collapsed pill still
 * refreshes on its own so it can NEVER sit on a stale $0.0000 while
 * agents burn tokens (v1#9: pill showed $0.0000 for 75 minutes while
 * the popover knew $45.74). useBrokerEvents additionally invalidates
 * the ["usage"] query on agent-activity SSE events, so the pill tracks
 * spend with turn activity rather than waiting out the interval.
 */
export const USAGE_REFETCH_OPEN_MS = 5_000;
export const USAGE_REFETCH_CLOSED_MS = 15_000;

/**
 * The regression that motivated this helper: the interval was
 * `open ? 5000 : false` — the pill never refetched while collapsed.
 * Exported so the test pins "closed still refetches".
 */
export function usageRefetchInterval(open: boolean): number {
  return open ? USAGE_REFETCH_OPEN_MS : USAGE_REFETCH_CLOSED_MS;
}

export function UsagePanel() {
  const [open, setOpen] = useState(false);
  const { data: usage } = useQuery({
    queryKey: ["usage"],
    queryFn: () => getUsage(),
    refetchInterval: usageRefetchInterval(open),
  });

  const totalCost = usage?.total?.cost_usd ?? 0;
  const agents = usage?.agents ?? {};
  const slugs = Object.keys(agents).sort();

  return (
    <>
      <button
        type="button"
        className={`usage-toggle${open ? " open" : ""}`}
        onClick={() => setOpen((v) => !v)}
      >
        <svg
          aria-hidden="true"
          focusable="false"
          width="10"
          height="10"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          <path d="m9 18 6-6-6-6" />
        </svg>
        Usage
        <span
          style={{ marginLeft: "auto", fontWeight: 400 }}
          data-testid="usage-pill-cost"
        >
          {formatUSD(totalCost)}
        </span>
      </button>
      {open ? (
        <div className="usage-panel open">
          {slugs.length === 0 && totalCost === 0 ? (
            <p
              style={{
                fontSize: 11,
                color: "var(--text-tertiary)",
                padding: "4px 0",
              }}
            >
              No usage recorded yet.
            </p>
          ) : (
            <>
              <table className="usage-table">
                <thead>
                  <tr>
                    {["Agent", "In", "Out", "Cache", "Cost"].map((h) => (
                      <th key={h}>{h}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {slugs.map((slug) => {
                    const a = agents[slug];
                    return (
                      <tr key={slug}>
                        <td>{slug}</td>
                        <td>{formatTokens(a.input_tokens)}</td>
                        <td>{formatTokens(a.output_tokens)}</td>
                        <td>{formatTokens(a.cache_read_tokens)}</td>
                        <td>{formatUSD(a.cost_usd)}</td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
              <div className="usage-total">
                <span>
                  Session: {formatTokens(usage?.session?.total_tokens ?? 0)}{" "}
                  tokens
                </span>
                <span
                  className="usage-total-cost"
                  data-testid="usage-popover-cost"
                >
                  {formatUSD(totalCost)}
                </span>
              </div>
            </>
          )}
        </div>
      ) : null}
    </>
  );
}
