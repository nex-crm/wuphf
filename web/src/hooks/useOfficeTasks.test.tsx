import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import * as tasksApi from "../api/tasks";
import {
  OFFICE_TASKS_QUERY_KEY,
  OFFICE_TASKS_REFETCH_MS,
  useOfficeTasks,
} from "./useOfficeTasks";

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
});

describe("useOfficeTasks", () => {
  it("fetches once and exposes Task[] data", async () => {
    const spy = vi.spyOn(tasksApi, "getOfficeTasks").mockResolvedValue({
      tasks: [
        { id: "1", title: "first", status: "open" },
        { id: "2", title: "second", status: "done" },
      ],
    });

    const { result } = renderHook(() => useOfficeTasks(), {
      wrapper: wrapper(makeClient()),
    });

    await waitFor(() => {
      expect(result.current.data).toEqual([
        { id: "1", title: "first", status: "open" },
        { id: "2", title: "second", status: "done" },
      ]);
    });
    expect(spy).toHaveBeenCalledTimes(1);
    expect(spy).toHaveBeenCalledWith({ includeDone: true });
  });

  it("normalises a missing tasks field to an empty array", async () => {
    vi.spyOn(tasksApi, "getOfficeTasks").mockResolvedValue(
      {} as unknown as tasksApi.TaskListResponse,
    );

    const { result } = renderHook(() => useOfficeTasks(), {
      wrapper: wrapper(makeClient()),
    });

    await waitFor(() => {
      expect(result.current.data).toEqual([]);
    });
  });

  it("exposes a loading state before the fetch resolves", () => {
    vi.spyOn(tasksApi, "getOfficeTasks").mockImplementation(
      () => new Promise(() => {}),
    );

    const { result } = renderHook(() => useOfficeTasks(), {
      wrapper: wrapper(makeClient()),
    });

    expect(result.current.isLoading).toBe(true);
    expect(result.current.data).toBeUndefined();
    expect(result.current.error).toBeNull();
  });

  it("surfaces errors via the query result", async () => {
    vi.spyOn(tasksApi, "getOfficeTasks").mockRejectedValue(new Error("boom"));

    const { result } = renderHook(() => useOfficeTasks(), {
      wrapper: wrapper(makeClient()),
    });

    await waitFor(() => {
      expect(result.current.error).toBeInstanceOf(Error);
    });
    expect((result.current.error as Error).message).toBe("boom");
    expect(result.current.data).toBeUndefined();
  });

  it("publishes a stable cache key and refetch interval", () => {
    expect(OFFICE_TASKS_QUERY_KEY).toEqual(["office-tasks"]);
    expect(OFFICE_TASKS_REFETCH_MS).toBe(10_000);
  });
});
