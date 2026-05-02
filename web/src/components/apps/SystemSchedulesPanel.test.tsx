import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { SchedulerJob } from "../../api/client";
import { ToastContainer } from "../ui/Toast";

const MOCK_SPECS = vi.hoisted(() => [
  {
    slug: "nex-insights",
    min_floor_minutes: 30,
    default_interval_minutes: 30,
    description: "Nex insights",
  },
  {
    slug: "task_recheck",
    min_floor_minutes: 5,
    default_interval_minutes: 5,
    description: "Task recheck cadence",
  },
]);

vi.mock("../../api/client", async () => {
  const actual =
    await vi.importActual<typeof import("../../api/client")>(
      "../../api/client",
    );
  return {
    ...actual,
    patchSchedulerJob: vi.fn().mockResolvedValue({ job: {} }),
    getSystemCronSpecs: vi.fn().mockResolvedValue(MOCK_SPECS),
    runSchedulerJob: vi.fn().mockResolvedValue({
      triggered: true,
      slug: "task_recheck",
      at: new Date().toISOString(),
    }),
  };
});

import * as clientMod from "../../api/client";
import { SystemSchedulesPanel } from "./SystemSchedulesPanel";

function wrap(ui: ReactNode) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={qc}>{ui}</QueryClientProvider>;
}

function wrapWithToast(ui: ReactNode) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return (
    <QueryClientProvider client={qc}>
      {ui}
      <ToastContainer />
    </QueryClientProvider>
  );
}

const baseSystemJob: SchedulerJob = {
  slug: "task_recheck",
  label: "Task recheck cadence",
  interval_minutes: 10,
  enabled: true,
  system_managed: true,
  next_run: new Date(Date.now() + 60_000).toISOString(),
  last_run: new Date(Date.now() - 60_000).toISOString(),
  last_run_status: "ok",
};

const readOnlyRelay: SchedulerJob = {
  slug: "one-relay-events",
  label: "One relay events",
  interval_minutes: 1,
  enabled: true,
  system_managed: true,
};

const insightsJob: SchedulerJob = {
  slug: "nex-insights",
  label: "Nex insights",
  interval_minutes: 60,
  enabled: true,
  system_managed: true,
};

const workflowCronJob: SchedulerJob = {
  slug: "weekly-digest",
  label: "Weekly digest",
  schedule_expr: "0 9 * * MON",
  enabled: true,
  system_managed: false,
  kind: "workflow",
};

describe("<SystemSchedulesPanel> rendering", () => {
  it("renders nothing when no cadence jobs are present", () => {
    const { container } = render(wrap(<SystemSchedulesPanel jobs={[]} />));
    expect(container.textContent).not.toContain("System Schedules");
  });

  it("renders each row state — system, read-only, workflow cron", () => {
    render(
      wrap(
        <SystemSchedulesPanel
          jobs={[baseSystemJob, readOnlyRelay, workflowCronJob]}
        />,
      ),
    );

    // Section heading present.
    expect(screen.getByText("System Schedules")).toBeInTheDocument();

    // System interval row exposes a number input.
    expect(
      screen.getByRole("spinbutton", {
        name: /Interval in minutes for Task recheck cadence/i,
      }),
    ).toBeInTheDocument();

    // Read-only row uses static text, not an input.
    expect(screen.getByText(/Every 1m \(read-only\)/i)).toBeInTheDocument();
    expect(
      screen.queryByRole("spinbutton", {
        name: /Interval in minutes for One relay events/i,
      }),
    ).not.toBeInTheDocument();

    // Workflow cron row shows the cron string read-only.
    expect(screen.getByText(/cron: 0 9 \* \* MON/)).toBeInTheDocument();
    expect(
      screen.queryByRole("spinbutton", {
        name: /Interval in minutes for Weekly digest/i,
      }),
    ).not.toBeInTheDocument();

    // Source badges.
    expect(screen.getAllByText("system").length).toBe(2);
    expect(screen.getByText("workflow")).toBeInTheDocument();

    // OK chip on last_run_status="ok".
    expect(screen.getByText(/^OK ·/)).toBeInTheDocument();
  });

  it("disables the toggle on read-only crons", () => {
    render(wrap(<SystemSchedulesPanel jobs={[readOnlyRelay]} />));
    const toggle = screen.getByRole("switch", {
      name: /Disable One relay events/i,
    });
    expect(toggle).toBeDisabled();
  });
});

describe("<SystemSchedulesPanel> toggle round-trip", () => {
  it("optimistically flips enabled and submits PATCH on toggle", async () => {
    const patchMock = vi
      .mocked(clientMod.patchSchedulerJob)
      .mockResolvedValueOnce({
        job: { ...baseSystemJob, enabled: false },
      });

    render(wrap(<SystemSchedulesPanel jobs={[baseSystemJob]} />));

    const toggle = screen.getByRole("switch", {
      name: /Disable Task recheck cadence/i,
    });
    expect(toggle).toHaveAttribute("aria-checked", "true");

    fireEvent.click(toggle);

    // Optimistic flip is immediate.
    expect(toggle).toHaveAttribute("aria-checked", "false");

    await waitFor(() => {
      expect(patchMock).toHaveBeenCalledWith("task_recheck", {
        enabled: false,
      });
    });
  });

  it("rolls back optimistic state when PATCH fails", async () => {
    vi.mocked(clientMod.patchSchedulerJob).mockRejectedValueOnce(
      new Error("network down"),
    );

    render(wrap(<SystemSchedulesPanel jobs={[baseSystemJob]} />));

    const toggle = screen.getByRole("switch", {
      name: /Disable Task recheck cadence/i,
    });
    fireEvent.click(toggle);

    await waitFor(() => {
      expect(toggle).toHaveAttribute("aria-checked", "true");
    });
    expect(screen.getByRole("alert")).toHaveTextContent(/network down/);
  });
});

describe("<SystemSchedulesPanel> interval validation", () => {
  it("blocks PATCH when override is below the per-cron floor", async () => {
    const patchMock = vi.mocked(clientMod.patchSchedulerJob);
    patchMock.mockClear();

    // nex-insights floor is 30 min. Try 10.
    render(wrap(<SystemSchedulesPanel jobs={[insightsJob]} />));

    const input = screen.getByRole("spinbutton", {
      name: /Interval in minutes for Nex insights/i,
    });
    await waitFor(() => {
      expect(input).toHaveAttribute("min", "30");
    });
    fireEvent.change(input, { target: { value: "10" } });
    fireEvent.blur(input);

    expect(screen.getByRole("alert")).toHaveTextContent(
      /Min interval is 30 min/i,
    );
    expect(patchMock).not.toHaveBeenCalled();
  });

  it("rejects empty / blank overrides without hitting the network", () => {
    const patchMock = vi.mocked(clientMod.patchSchedulerJob);
    patchMock.mockClear();

    render(wrap(<SystemSchedulesPanel jobs={[baseSystemJob]} />));

    const input = screen.getByRole("spinbutton", {
      name: /Interval in minutes for Task recheck cadence/i,
    });
    fireEvent.change(input, { target: { value: "" } });
    fireEvent.blur(input);

    expect(screen.getByRole("alert")).toHaveTextContent(/required/i);
    expect(patchMock).not.toHaveBeenCalled();
  });

  it("rejects negative overrides without hitting the network", () => {
    const patchMock = vi.mocked(clientMod.patchSchedulerJob);
    patchMock.mockClear();

    render(wrap(<SystemSchedulesPanel jobs={[baseSystemJob]} />));

    const input = screen.getByRole("spinbutton", {
      name: /Interval in minutes for Task recheck cadence/i,
    });
    fireEvent.change(input, { target: { value: "-5" } });
    fireEvent.blur(input);

    expect(screen.getByRole("alert")).toHaveTextContent(
      /non-negative whole number/i,
    );
    expect(patchMock).not.toHaveBeenCalled();
  });

  it("submits a valid override above the floor", async () => {
    const patchMock = vi.mocked(clientMod.patchSchedulerJob);
    patchMock.mockClear();
    patchMock.mockResolvedValueOnce({
      job: { ...baseSystemJob, interval_override: 20 },
    });

    // task_recheck default 10, floor 5. Try 20.
    render(wrap(<SystemSchedulesPanel jobs={[baseSystemJob]} />));

    const input = screen.getByRole("spinbutton", {
      name: /Interval in minutes for Task recheck cadence/i,
    });
    fireEvent.change(input, { target: { value: "20" } });
    fireEvent.blur(input);

    await waitFor(() => {
      expect(patchMock).toHaveBeenCalledWith("task_recheck", {
        interval_override: 20,
      });
    });
  });

  it("clears the override when the user types the default value", async () => {
    const patchMock = vi.mocked(clientMod.patchSchedulerJob);
    patchMock.mockClear();

    // Job already has a 30-minute override; typing back the 10-minute
    // default should send interval_override: 0 (clears the override).
    const overriddenJob: SchedulerJob = {
      ...baseSystemJob,
      interval_override: 30,
    };
    patchMock.mockResolvedValueOnce({
      job: { ...overriddenJob, interval_override: 0 },
    });

    render(wrap(<SystemSchedulesPanel jobs={[overriddenJob]} />));

    const input = screen.getByRole("spinbutton", {
      name: /Interval in minutes for Task recheck cadence/i,
    });
    fireEvent.change(input, { target: { value: "10" } });
    fireEvent.blur(input);

    await waitFor(() => {
      expect(patchMock).toHaveBeenCalledWith("task_recheck", {
        interval_override: 0,
      });
    });
  });
});

describe("<SystemSchedulesPanel> run now", () => {
  it("calls runSchedulerJob with the row's slug on click", async () => {
    const runMock = vi.mocked(clientMod.runSchedulerJob);
    runMock.mockClear();

    render(wrap(<SystemSchedulesPanel jobs={[baseSystemJob]} />));

    const btn = screen.getByRole("button", {
      name: /Run Task recheck cadence now/i,
    });
    fireEvent.click(btn);

    await waitFor(() => {
      expect(runMock).toHaveBeenCalledWith("task_recheck");
    });
  });

  it("shows a success toast and re-enables the button after run succeeds", async () => {
    const runMock = vi.mocked(clientMod.runSchedulerJob);
    runMock.mockResolvedValueOnce({
      triggered: true,
      slug: "task_recheck",
      at: new Date().toISOString(),
    });

    render(wrapWithToast(<SystemSchedulesPanel jobs={[baseSystemJob]} />));

    const btn = screen.getByRole("button", {
      name: /Run Task recheck cadence now/i,
    });
    fireEvent.click(btn);

    await waitFor(() => {
      expect(
        screen.getByText(/Task recheck cadence triggered/i),
      ).toBeInTheDocument();
    });
    expect(btn).not.toBeDisabled();
  });

  it("shows an error toast and re-enables the button after run fails", async () => {
    const runMock = vi.mocked(clientMod.runSchedulerJob);
    runMock.mockRejectedValueOnce(new Error("broker unreachable"));

    render(wrapWithToast(<SystemSchedulesPanel jobs={[baseSystemJob]} />));

    const btn = screen.getByRole("button", {
      name: /Run Task recheck cadence now/i,
    });
    fireEvent.click(btn);

    await waitFor(() => {
      expect(
        screen.getByText(/Couldn't trigger Task recheck cadence/i),
      ).toBeInTheDocument();
    });
    expect(btn).not.toBeDisabled();
  });
});
