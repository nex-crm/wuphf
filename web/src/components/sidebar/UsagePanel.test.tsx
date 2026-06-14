import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import * as platformApi from "../../api/platform";
import { UsagePanel, usageRefetchInterval } from "./UsagePanel";

const USAGE = {
  total: {
    input_tokens: 1_000_000,
    output_tokens: 250_000,
    cache_read_tokens: 0,
    total_tokens: 1_250_000,
    cost_usd: 45.7437,
  },
  session: { total_tokens: 1_250_000 },
  agents: {
    ceo: {
      input_tokens: 1_000_000,
      output_tokens: 250_000,
      cache_read_tokens: 0,
      total_tokens: 1_250_000,
      cost_usd: 45.7437,
    },
  },
  since: "2026-06-11T00:00:00Z",
} as unknown as Awaited<ReturnType<typeof platformApi.getUsage>>;

function wrap(ui: ReactNode) {
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchInterval: false, gcTime: 0 },
    },
  });
  return <QueryClientProvider client={client}>{ui}</QueryClientProvider>;
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("<UsagePanel>", () => {
  it("pill and popover render the same number from the same payload (C2)", async () => {
    vi.spyOn(platformApi, "getUsage").mockResolvedValue(USAGE);

    render(wrap(<UsagePanel />));

    // Collapsed pill shows the aggregate.
    await waitFor(() => {
      expect(screen.getByTestId("usage-pill-cost")).not.toHaveTextContent(
        "$0.0000",
      );
    });
    const pillText = screen.getByTestId("usage-pill-cost").textContent;

    // Open the popover: its total must be the exact same rendering of
    // the exact same payload — never a second source or cadence.
    fireEvent.click(screen.getByRole("button", { name: /usage/i }));
    const popoverText = screen.getByTestId("usage-popover-cost").textContent;
    expect(popoverText).toBe(pillText);
  });

  it("keeps refetching while collapsed (the $0.0000-for-75-minutes bug)", () => {
    // The regression: refetchInterval was `open ? 5000 : false`, so the
    // collapsed pill NEVER updated after its mount-time fetch. Closed
    // must poll too.
    expect(usageRefetchInterval(false)).toBeGreaterThan(0);
    expect(usageRefetchInterval(true)).toBeGreaterThan(0);
    // Open is allowed to be faster, never slower.
    expect(usageRefetchInterval(true)).toBeLessThanOrEqual(
      usageRefetchInterval(false),
    );
  });
});
