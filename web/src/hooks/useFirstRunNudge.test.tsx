import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import * as apiClient from "../api/client";
import { useFirstRunNudge } from "./useFirstRunNudge";

function wrapper(client: QueryClient) {
  return ({ children }: { children: React.ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("useFirstRunNudge", () => {
  let queryClient: QueryClient;

  beforeEach(() => {
    queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false, refetchInterval: false, gcTime: 0 },
      },
    });
  });

  it("hides the nudge when humanHasPosted=true", async () => {
    vi.spyOn(apiClient, "getOfficeMembers").mockResolvedValue({
      members: [],
      meta: { humanHasPosted: true },
    });

    const { result } = renderHook(() => useFirstRunNudge(), {
      wrapper: wrapper(queryClient),
    });

    await waitFor(() => {
      expect(result.current.showNudge).toBe(false);
    });
  });

  it("shows the nudge when humanHasPosted=false", async () => {
    vi.spyOn(apiClient, "getOfficeMembers").mockResolvedValue({
      members: [],
      meta: { humanHasPosted: false },
    });

    const { result } = renderHook(() => useFirstRunNudge(), {
      wrapper: wrapper(queryClient),
    });

    await waitFor(() => {
      expect(result.current.showNudge).toBe(true);
    });
  });

  it("hides the nudge when meta is absent (defensive default — no flash on backends without Lane A)", async () => {
    vi.spyOn(apiClient, "getOfficeMembers").mockResolvedValue({
      members: [],
    });

    const { result } = renderHook(() => useFirstRunNudge(), {
      wrapper: wrapper(queryClient),
    });

    await waitFor(() => {
      expect(result.current.showNudge).toBe(false);
    });
  });

  it("dismisses the nudge after the human posts and the query refetches", async () => {
    const fetchSpy = vi
      .spyOn(apiClient, "getOfficeMembers")
      .mockResolvedValueOnce({ members: [], meta: { humanHasPosted: false } });

    const { result } = renderHook(() => useFirstRunNudge(), {
      wrapper: wrapper(queryClient),
    });

    await waitFor(() => {
      expect(result.current.showNudge).toBe(true);
    });

    // Simulate the broker flipping humanHasPosted on the next fetch — the
    // SSE message handler invalidates the query, which triggers a refetch.
    fetchSpy.mockResolvedValueOnce({
      members: [],
      meta: { humanHasPosted: true },
    });

    await act(async () => {
      await queryClient.invalidateQueries({ queryKey: ["office-members"] });
    });

    await waitFor(() => {
      expect(result.current.showNudge).toBe(false);
    });
  });
});
