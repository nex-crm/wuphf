/**
 * Regression: LiveStreamTab and AgentProfilePanel (Config tab) share the
 * React Query key ["agent-log-tasks", slug]. AgentProfilePanel caches an
 * ARRAY (it applies arrayOrEmpty to the response's `tasks`). If LiveStreamTab
 * cached the raw `{ tasks: [...] }` envelope under the same key, whichever tab
 * loaded first poisoned the cache for the other — Config then crashed with
 * "agentRuns.map is not a function".
 *
 * This pins the invariant: LiveStreamTab must normalize the cached value to an
 * array so the shared key holds a shape both consumers can map over.
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("../../../hooks/useAgentStream", () => ({
  useAgentStream: () => ({ lines: [], connected: false }),
}));

vi.mock("../../../api/tasks", () => ({
  // The broker shape: an envelope with a `tasks` array.
  listAgentLogTasks: vi.fn(async () => ({
    tasks: [
      { taskId: "GROW-12", toolCallCount: 14, hasError: false },
      { taskId: "GROW-9", toolCallCount: 6, hasError: true },
    ],
  })),
}));

import { LiveStreamTab } from "./LiveStreamTab";

const SLUG = "growth";

function renderTab() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, refetchInterval: false } },
  });
  const utils = render(
    <QueryClientProvider client={client}>
      <LiveStreamTab agentSlug={SLUG} />
    </QueryClientProvider>,
  );
  return { client, ...utils };
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("LiveStreamTab recent-runs cache shape", () => {
  it("renders the recent runs from the envelope", async () => {
    renderTab();
    expect(await screen.findByText("GROW-12")).toBeInTheDocument();
    expect(screen.getByText("GROW-9")).toBeInTheDocument();
  });

  it("caches an array under the shared ['agent-log-tasks', slug] key", async () => {
    const { client } = renderTab();
    await screen.findByText("GROW-12");
    // The Config tab's AgentProfilePanel reads this exact key and calls
    // `.map` on it — so it MUST be an array, never the {tasks} envelope.
    const cached = client.getQueryData(["agent-log-tasks", SLUG]);
    expect(Array.isArray(cached)).toBe(true);
  });
});
