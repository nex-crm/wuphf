import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { OfficeStats } from "../api/platform";
import * as platformApi from "../api/platform";
import { useAppStore } from "../stores/app";
import { OFFICE_STATS_QUERY_KEY, useOfficeStats } from "./useOfficeStats";

const STATS_FIXTURE: OfficeStats = {
  tasks: {
    backlog: 2,
    active: 3,
    blocked: 1,
    review: 1,
    needs_human: 2,
    done: 4,
    archive: 0,
  },
  requests: { blocking: 2, notices: 1 },
  inbox_attention: 5,
  wiki_articles: 19,
  agents_active: 3,
  generated_at: "2026-06-11T00:00:00Z",
};

function wrapper(client: QueryClient) {
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

function makeClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchInterval: false, gcTime: 0 },
    },
  });
}

afterEach(() => {
  vi.restoreAllMocks();
  useAppStore.setState({ brokerConnected: false });
});

describe("useOfficeStats", () => {
  it("fetches /office/stats and exposes the payload", async () => {
    useAppStore.setState({ brokerConnected: true });
    const spy = vi
      .spyOn(platformApi, "getOfficeStats")
      .mockResolvedValue(STATS_FIXTURE);

    const { result } = renderHook(() => useOfficeStats(), {
      wrapper: wrapper(makeClient()),
    });

    await waitFor(() => {
      expect(result.current.data).toEqual(STATS_FIXTURE);
    });
    expect(spy).toHaveBeenCalledTimes(1);
  });

  it("does not fetch while the broker is disconnected", () => {
    const spy = vi
      .spyOn(platformApi, "getOfficeStats")
      .mockResolvedValue(STATS_FIXTURE);

    const { result } = renderHook(() => useOfficeStats(), {
      wrapper: wrapper(makeClient()),
    });

    expect(spy).not.toHaveBeenCalled();
    expect(result.current.data).toBeUndefined();
  });

  it("shares one cache entry across consumers (single source)", async () => {
    useAppStore.setState({ brokerConnected: true });
    const spy = vi
      .spyOn(platformApi, "getOfficeStats")
      .mockResolvedValue(STATS_FIXTURE);
    const client = makeClient();

    const a = renderHook(() => useOfficeStats(), {
      wrapper: wrapper(client),
    });
    const b = renderHook(() => useOfficeStats(), {
      wrapper: wrapper(client),
    });

    await waitFor(() => {
      expect(a.result.current.data).toEqual(STATS_FIXTURE);
      expect(b.result.current.data).toEqual(STATS_FIXTURE);
    });
    // One fetch serves both consumers via the shared key.
    expect(spy).toHaveBeenCalledTimes(1);
    expect(client.getQueryData(OFFICE_STATS_QUERY_KEY)).toEqual(STATS_FIXTURE);
  });
});
