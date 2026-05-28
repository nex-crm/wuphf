import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { SchedulerJob } from "../../api/client";

// vi.mock is hoisted to the top of the file, so any data it references
// must be declared via vi.hoisted (otherwise the closure captures
// uninitialised module-level bindings and the mock throws on import).
const { MOCK_JOBS, MOCK_RUNS } = vi.hoisted(() => {
  const now = Date.now();
  return {
    MOCK_JOBS: [
      {
        slug: "nex-insights",
        label: "Nex insights pull",
        kind: "cron",
        interval_minutes: 30,
        enabled: true,
        system_managed: true,
        next_run: new Date(now + 45 * 60_000).toISOString(),
        last_run: new Date(now - 2 * 60 * 60_000).toISOString(),
        last_run_status: "ok",
      },
      {
        slug: "workflow:weekly-digest",
        label: "Weekly digest",
        kind: "workflow",
        target_type: "workflow",
        schedule_expr: "0 9 * * 1-5",
        enabled: false,
        next_run: new Date(now + 24 * 60 * 60_000).toISOString(),
        last_run: new Date(now - 26 * 60 * 60_000).toISOString(),
        last_run_status: "failed",
      },
    ] as SchedulerJob[],
    MOCK_RUNS: [
      {
        slug: "nex-insights",
        started_at: new Date(now - 60 * 60_000).toISOString(),
        finished_at: new Date(now - 60 * 60_000 + 3_400).toISOString(),
        status: "ok",
        triggered_by: "scheduler",
      },
      {
        slug: "nex-insights",
        started_at: new Date(now - 90 * 60_000).toISOString(),
        finished_at: new Date(now - 90 * 60_000 + 4_100).toISOString(),
        status: "failed",
        message: "broker returned 502",
      },
    ],
  };
});

vi.mock("../../api/client", async () => {
  const actual =
    await vi.importActual<typeof import("../../api/client")>(
      "../../api/client",
    );
  return {
    ...actual,
    getScheduler: vi.fn().mockResolvedValue({ jobs: MOCK_JOBS }),
    getSchedulerRuns: vi.fn().mockResolvedValue(MOCK_RUNS),
    runSchedulerJob: vi.fn().mockResolvedValue({
      triggered: true,
      slug: "nex-insights",
      at: new Date().toISOString(),
    }),
    patchSchedulerJob: vi.fn().mockResolvedValue({ job: {} }),
  };
});

const navigateMock = vi.hoisted(() => vi.fn());
vi.mock("../../lib/router", async () => {
  const actual =
    await vi.importActual<typeof import("../../lib/router")>(
      "../../lib/router",
    );
  return {
    ...actual,
    router: { ...actual.router, navigate: navigateMock },
  };
});

import * as clientMod from "../../api/client";
import { RoutinesApp } from "./RoutinesApp";

function wrap(ui: ReactNode) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={qc}>{ui}</QueryClientProvider>;
}

beforeEach(() => {
  // Reset localStorage to keep default-view assertions deterministic.
  window.localStorage.removeItem("routines.viewMode");
  window.localStorage.removeItem("routines.showSystem");
  // mockReset (not mockClear) drops per-test mockResolvedValueOnce
  // overrides too, otherwise an override in one test would leak into
  // the next if test order shifts. Re-seed the default fixture after
  // resetting so the common path stays one assertion away.
  vi.mocked(clientMod.getScheduler).mockReset();
  vi.mocked(clientMod.getScheduler).mockResolvedValue({ jobs: MOCK_JOBS });
  vi.mocked(clientMod.getSchedulerRuns).mockReset();
  vi.mocked(clientMod.getSchedulerRuns).mockResolvedValue(MOCK_RUNS);
  vi.mocked(clientMod.runSchedulerJob).mockReset();
  vi.mocked(clientMod.runSchedulerJob).mockResolvedValue({
    triggered: true,
    slug: "nex-insights",
    at: new Date().toISOString(),
  });
  vi.mocked(clientMod.patchSchedulerJob).mockReset();
  vi.mocked(clientMod.patchSchedulerJob).mockResolvedValue({ job: {} });
  navigateMock.mockReset();
});

afterEach(() => {
  vi.clearAllMocks();
});

describe("RoutinesApp", () => {
  it("renders the header with the routine count and a view toggle", async () => {
    render(wrap(<RoutinesApp />));
    await waitFor(() =>
      expect(screen.getByTestId("routines-title")).toBeInTheDocument(),
    );
    expect(screen.getByTestId("routines-view-calendar")).toBeInTheDocument();
    expect(screen.getByTestId("routines-view-list")).toBeInTheDocument();
    // System routine is hidden by default, so the visible count is 1
    // (the workflow), not 2.
    await waitFor(() =>
      expect(screen.getByTestId("routines-title").textContent).toContain("1"),
    );
  });

  it("renders the list view by default and hides system routines", async () => {
    render(wrap(<RoutinesApp />));
    await waitFor(() =>
      expect(screen.getByTestId("routine-list")).toBeInTheDocument(),
    );
    // Workflow routine is always visible.
    expect(
      screen.getByTestId("routine-row-workflow:weekly-digest"),
    ).toBeInTheDocument();
    // System routine (nex-insights) is hidden behind the opt-in toggle.
    expect(
      screen.queryByTestId("routine-row-nex-insights"),
    ).not.toBeInTheDocument();
  });

  it("reveals system routines when the show-system toggle is checked", async () => {
    render(wrap(<RoutinesApp />));
    const toggle = await screen.findByTestId("routines-show-system-toggle");
    const checkbox = toggle.querySelector(
      "input[type=checkbox]",
    ) as HTMLInputElement;
    fireEvent.click(checkbox);
    await waitFor(() =>
      expect(
        screen.getByTestId("routine-row-nex-insights"),
      ).toBeInTheDocument(),
    );
  });

  it("switches to calendar view and projects routine chips into the month grid", async () => {
    // Pre-seed showSystem so the system routine (which has the projectable
    // 30-minute cadence) is visible in the calendar.
    window.localStorage.setItem("routines.showSystem", "true");
    render(wrap(<RoutinesApp />));
    await waitFor(() =>
      expect(screen.getByTestId("routines-view-calendar")).toBeInTheDocument(),
    );
    fireEvent.click(screen.getByTestId("routines-view-calendar"));
    await waitFor(() =>
      expect(screen.getByTestId("routine-calendar")).toBeInTheDocument(),
    );
    await waitFor(() => {
      expect(
        screen.getAllByTestId("routine-chip-nex-insights").length,
      ).toBeGreaterThan(0);
    });
  });

  it("navigates to the full detail page when a routine row is clicked", async () => {
    render(wrap(<RoutinesApp />));
    const row = await screen.findByTestId("routine-row-workflow:weekly-digest");
    fireEvent.click(row);
    await waitFor(() => {
      expect(navigateMock).toHaveBeenCalledWith({
        to: "/routines/$routineSlug",
        params: { routineSlug: "workflow:weekly-digest" },
      });
    });
  });

  it("renders the empty state when no routines exist", async () => {
    vi.mocked(clientMod.getScheduler).mockResolvedValue({ jobs: [] });
    render(wrap(<RoutinesApp />));
    await waitFor(() =>
      expect(screen.getByTestId("routines-empty-state")).toBeInTheDocument(),
    );
  });
});
