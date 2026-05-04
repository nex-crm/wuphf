import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { useAppStore } from "../stores/app";
import { useHashRouter } from "./useHashRouter";

afterEach(() => {
  window.location.hash = "";
  useAppStore.setState({
    currentChannel: "general",
    currentApp: null,
    taskDetailId: null,
    channelMeta: {},
    wikiPath: null,
    wikiLookupQuery: null,
    notebookAgentSlug: null,
    notebookEntrySlug: null,
  });
});

describe("useHashRouter", () => {
  it("routes bare tasks hashes to the tasks app", async () => {
    window.location.hash = "#/tasks";

    renderHook(() => useHashRouter());

    await waitFor(() => {
      expect(useAppStore.getState()).toMatchObject({
        currentApp: "tasks",
        taskDetailId: null,
      });
    });
    expect(window.location.hash).toBe("#/tasks");
  });

  it("routes task hashes to a task detail page", async () => {
    window.location.hash = "#/tasks/task-123";

    renderHook(() => useHashRouter());

    await waitFor(() => {
      expect(useAppStore.getState()).toMatchObject({
        currentApp: "tasks",
        taskDetailId: "task-123",
      });
    });
  });

  it("preserves bare tasks app state as the tasks route", async () => {
    const { unmount } = renderHook(() => useHashRouter());

    act(() => {
      useAppStore.getState().setCurrentApp("tasks");
    });

    await waitFor(() => {
      expect(window.location.hash).toBe("#/tasks");
    });

    unmount();
  });

  it("writes task detail state as the task detail route", async () => {
    const { unmount } = renderHook(() => useHashRouter());

    act(() => {
      useAppStore.getState().setTaskDetailRoute("task-123");
    });

    await waitFor(() => {
      expect(window.location.hash).toBe("#/tasks/task-123");
    });

    unmount();
  });
});
