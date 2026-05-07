import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterAll, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

import type { SchedulerJob } from "../../api/client";
import type { Task } from "../../api/tasks";

// ── Mock useOfficeTasks ───────────────────────────────────────────────────────

const mockTasksData = vi.hoisted(() => ({
  data: [] as Task[],
  isLoading: false,
  error: null,
}));

vi.mock("../../hooks/useOfficeTasks", () => ({
  useOfficeTasks: () => mockTasksData,
  OFFICE_TASKS_QUERY_KEY: ["office-tasks"],
  OFFICE_TASKS_REFETCH_MS: 10_000,
}));

// ── Mock scheduler API ────────────────────────────────────────────────────────

const mockSchedulerJobs = vi.hoisted(() => ({ jobs: [] as SchedulerJob[] }));

vi.mock("../../api/client", async () => {
  const actual =
    await vi.importActual<typeof import("../../api/client")>(
      "../../api/client",
    );
  return {
    ...actual,
    getScheduler: vi
      .fn()
      .mockImplementation(() => Promise.resolve(mockSchedulerJobs)),
    getSystemCronSpecs: vi.fn().mockResolvedValue([]),
    patchSchedulerJob: vi.fn().mockResolvedValue({ job: {} }),
    runSchedulerJob: vi.fn().mockResolvedValue({ triggered: true }),
  };
});

import { CalendarApp } from "./CalendarApp";

// ── Helpers ───────────────────────────────────────────────────────────────────

function wrap(ui: ReactNode) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

function makeTask(
  overrides: Partial<Task> & { id: string; title: string },
): Task {
  return {
    status: "open",
    ...overrides,
  };
}

/** Build an ISO date string offset `days` from today in UTC. */
function daysFromToday(days: number): string {
  const d = new Date();
  d.setUTCDate(d.getUTCDate() + days);
  return `${d.toISOString().slice(0, 10)}T00:00:00Z`;
}

/** Return Monday of the current UTC week. */
function mondayOfThisWeek(): string {
  const d = new Date();
  const dow = d.getUTCDay();
  const diff = dow === 0 ? -6 : 1 - dow;
  d.setUTCDate(d.getUTCDate() + diff);
  return d.toISOString().slice(0, 10);
}

// ── Tests ─────────────────────────────────────────────────────────────────────

const FIXED_DATE = new Date("2025-06-16T12:00:00Z"); // a stable Monday

beforeAll(() => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(FIXED_DATE);
});

afterAll(() => {
  vi.useRealTimers();
});

beforeEach(() => {
  mockTasksData.data = [];
  mockTasksData.isLoading = false;
  mockTasksData.error = null;
  mockSchedulerJobs.jobs = [];
});

describe("CalendarApp — week grid", () => {
  it("renders calendar app container", () => {
    wrap(<CalendarApp />);
    expect(screen.getByTestId("calendar-app")).toBeTruthy();
  });

  it("renders 7 day columns in the week grid", async () => {
    mockTasksData.data = [
      makeTask({
        id: "t1",
        title: "Task One",
        owner: "agent-alpha",
        due_at: daysFromToday(0),
      }),
    ];
    wrap(<CalendarApp />);
    const grid = await screen.findByTestId("week-grid");
    // thead should have 8 cells (label + 7 days)
    const headers = grid.querySelectorAll("thead th");
    expect(headers.length).toBe(8);
  });

  it("shows a task chip for a scheduled task", async () => {
    mockTasksData.data = [
      makeTask({
        id: "task-abc",
        title: "Write tests",
        owner: "agent-bot",
        due_at: daysFromToday(0),
      }),
    ];
    wrap(<CalendarApp />);
    expect(await screen.findByTestId("task-chip-task-abc")).toBeTruthy();
    expect(screen.getByText(/Write tests/)).toBeTruthy();
  });

  it("groups tasks into agent swimlane rows", async () => {
    mockTasksData.data = [
      makeTask({
        id: "t-a1",
        title: "Task A1",
        owner: "alice",
        due_at: daysFromToday(0),
      }),
      makeTask({
        id: "t-b1",
        title: "Task B1",
        owner: "bob",
        due_at: daysFromToday(1),
      }),
    ];
    wrap(<CalendarApp />);
    await screen.findByTestId("week-grid");
    expect(screen.getByTestId("agent-row-alice")).toBeTruthy();
    expect(screen.getByTestId("agent-row-bob")).toBeTruthy();
  });

  it("places unassigned tasks in the unassigned swimlane", async () => {
    mockTasksData.data = [
      makeTask({
        id: "t-unassigned",
        title: "Orphan task",
        due_at: daysFromToday(0),
      }),
    ];
    wrap(<CalendarApp />);
    await screen.findByTestId("week-grid");
    expect(screen.getByTestId("agent-row-unassigned")).toBeTruthy();
  });
});

describe("CalendarApp — unscheduled tray", () => {
  it("shows tray for tasks without due_at", async () => {
    mockTasksData.data = [
      makeTask({
        id: "t-noduedate",
        title: "No due date task",
        owner: "agent-x",
      }),
    ];
    wrap(<CalendarApp />);
    const tray = await screen.findByTestId("unscheduled-tray");
    expect(tray).toBeTruthy();
    expect(screen.getByTestId("task-chip-t-noduedate")).toBeTruthy();
  });

  it("does not render tray when all tasks are scheduled", async () => {
    mockTasksData.data = [
      makeTask({
        id: "t-sched",
        title: "Scheduled",
        owner: "bob",
        due_at: daysFromToday(0),
      }),
    ];
    wrap(<CalendarApp />);
    await screen.findByTestId("week-grid");
    expect(screen.queryByTestId("unscheduled-tray")).toBeNull();
  });

  it("shows tray for tasks with an invalid due_at", async () => {
    mockTasksData.data = [
      makeTask({
        id: "t-invalid-due",
        title: "Invalid due",
        owner: "carol",
        due_at: "not-a-date",
      }),
    ];
    wrap(<CalendarApp />);
    expect(await screen.findByTestId("unscheduled-tray")).toBeTruthy();
  });
});

describe("CalendarApp — status markers", () => {
  it("renders blocked task with task chip", async () => {
    mockTasksData.data = [
      makeTask({
        id: "t-blocked",
        title: "Blocked task",
        status: "blocked",
        owner: "agent-a",
        due_at: daysFromToday(0),
      }),
    ];
    wrap(<CalendarApp />);
    const chip = await screen.findByTestId("task-chip-t-blocked");
    expect(chip).toBeTruthy();
    expect(chip.getAttribute("title")).toContain("blocked");
  });

  it("renders review task with task chip", async () => {
    mockTasksData.data = [
      makeTask({
        id: "t-review",
        title: "Review task",
        status: "review",
        owner: "agent-a",
        due_at: daysFromToday(0),
      }),
    ];
    wrap(<CalendarApp />);
    const chip = await screen.findByTestId("task-chip-t-review");
    expect(chip.getAttribute("title")).toContain("review");
  });

  it("renders done task with task chip", async () => {
    mockTasksData.data = [
      makeTask({
        id: "t-done",
        title: "Done task",
        status: "done",
        owner: "agent-a",
        due_at: daysFromToday(0),
      }),
    ];
    wrap(<CalendarApp />);
    expect(await screen.findByTestId("task-chip-t-done")).toBeTruthy();
  });
});

describe("CalendarApp — scheduler jobs", () => {
  it("renders scheduler one-shot job chip with clock icon", async () => {
    mockSchedulerJobs.jobs = [
      {
        slug: "deploy-prod",
        label: "Deploy to prod",
        next_run: daysFromToday(0),
      },
    ];
    wrap(<CalendarApp />);
    const chip = await screen.findByTestId("scheduler-chip-deploy-prod");
    expect(chip).toBeTruthy();
    // Clock emoji present
    expect(chip.textContent).toContain("⏱");
  });

  it("visually distinguishes scheduler jobs from task chips", async () => {
    mockSchedulerJobs.jobs = [
      {
        slug: "sched-job",
        label: "A scheduled job",
        next_run: daysFromToday(0),
      },
    ];
    mockTasksData.data = [
      makeTask({
        id: "t-task",
        title: "A task",
        owner: "agent-a",
        due_at: daysFromToday(0),
      }),
    ];
    wrap(<CalendarApp />);
    const jobChip = await screen.findByTestId("scheduler-chip-sched-job");
    const taskChip = await screen.findByTestId("task-chip-t-task");
    // Scheduler chip has accent background; task chip has card background
    expect(jobChip.getAttribute("style")).toContain("accent-bg");
    expect(taskChip.getAttribute("style")).toContain("bg-card");
  });
});

describe("CalendarApp — empty state", () => {
  it("renders empty state when there are no tasks or scheduler jobs", async () => {
    wrap(<CalendarApp />);
    expect(await screen.findByTestId("calendar-empty-state")).toBeTruthy();
  });

  it("empty state explains how tasks enter the calendar", async () => {
    wrap(<CalendarApp />);
    expect(await screen.findByText(/Nothing scheduled this week/)).toBeTruthy();
  });
});

describe("CalendarApp — click-through links", () => {
  it("task chip links to the task route", async () => {
    mockTasksData.data = [
      makeTask({
        id: "task-xyz",
        title: "Link me",
        owner: "agent-a",
        due_at: daysFromToday(0),
      }),
    ];
    wrap(<CalendarApp />);
    const chip = await screen.findByTestId("task-chip-task-xyz");
    expect(chip.getAttribute("href")).toContain("task-xyz");
  });

  it("agent label links to agent DM route", async () => {
    mockTasksData.data = [
      makeTask({
        id: "t-agent-link",
        title: "Agent link task",
        owner: "my-agent",
        due_at: daysFromToday(0),
      }),
    ];
    wrap(<CalendarApp />);
    await screen.findByTestId("week-grid");
    const agentLink = screen.getByTitle("Open my-agent");
    expect(agentLink.getAttribute("href")).toContain("my-agent");
  });
});

describe("CalendarApp — week navigation", () => {
  it("shows week range in header", async () => {
    wrap(<CalendarApp />);
    // Should display month + year somewhere
    const today = new Date();
    const year = today.getUTCFullYear();
    // The header should contain the year
    const appEl = screen.getByTestId("calendar-app");
    expect(appEl.textContent).toContain(String(year));
  });

  it("navigates to the next week on clicking Next week", async () => {
    // Place the task in the next week so the grid is visible after navigation
    mockTasksData.data = [
      makeTask({
        id: "t-next",
        title: "Next week task",
        owner: "agent-a",
        due_at: daysFromToday(7),
      }),
    ];
    wrap(<CalendarApp />);
    const nextBtn = screen.getByLabelText("Next week");
    await userEvent.click(nextBtn);
    // Grid should render because the task falls in the navigated week
    const grid = await screen.findByTestId("week-grid");
    expect(grid).toBeTruthy();
  });

  it("returns to current week on clicking Today", async () => {
    wrap(<CalendarApp />);
    const nextBtn = screen.getByLabelText("Next week");
    await userEvent.click(nextBtn);
    const todayBtn = screen.getByLabelText("Go to current week");
    await userEvent.click(todayBtn);
    // Should be back to this week (year is still visible)
    const year = new Date().getUTCFullYear();
    const appEl = screen.getByTestId("calendar-app");
    expect(appEl.textContent).toContain(String(year));
  });
});

describe("CalendarApp — agenda view", () => {
  it("toggles to agenda view", async () => {
    mockTasksData.data = [
      makeTask({
        id: "t-agenda",
        title: "Agenda task",
        owner: "agent-b",
        due_at: daysFromToday(0),
      }),
    ];
    wrap(<CalendarApp />);
    const toggleBtn = screen.getByLabelText("Switch to agenda view");
    await userEvent.click(toggleBtn);
    expect(await screen.findByTestId("agenda-view")).toBeTruthy();
  });

  it("agenda view shows tasks for days with work", async () => {
    mockTasksData.data = [
      makeTask({
        id: "t-ag-task",
        title: "Agenda visible",
        owner: "agent-b",
        due_at: daysFromToday(0),
      }),
    ];
    wrap(<CalendarApp />);
    await userEvent.click(screen.getByLabelText("Switch to agenda view"));
    // Task chip should still appear
    expect(await screen.findByTestId("task-chip-t-ag-task")).toBeTruthy();
  });

  it("agenda view shows unscheduled tray for undated tasks", async () => {
    mockTasksData.data = [
      makeTask({ id: "t-ag-unsched", title: "Undated", owner: "agent-c" }),
    ];
    wrap(<CalendarApp />);
    await userEvent.click(screen.getByLabelText("Switch to agenda view"));
    expect(await screen.findByTestId("unscheduled-tray")).toBeTruthy();
  });

  it("agenda empty state renders when no tasks or jobs", async () => {
    wrap(<CalendarApp />);
    await userEvent.click(screen.getByLabelText("Switch to agenda view"));
    expect(await screen.findByTestId("calendar-empty-state")).toBeTruthy();
  });
});

describe("CalendarApp — week boundary (Sunday/Monday)", () => {
  it("week starts on Monday and covers exactly 7 days", async () => {
    mockTasksData.data = [
      makeTask({
        id: "t-boundary",
        title: "Boundary task",
        owner: "agent-a",
        due_at: daysFromToday(0),
      }),
    ];
    wrap(<CalendarApp />);
    const grid = await screen.findByTestId("week-grid");
    const cols = grid.querySelectorAll("col");
    // 1 label col + 7 day cols
    expect(cols.length).toBe(8);
  });

  it("Monday of week derived correctly regardless of current day", async () => {
    // Derive Monday programmatically and check it appears in the first day header
    const monday = mondayOfThisWeek();
    const dayNum = new Date(`${monday}T00:00:00Z`).getUTCDate();
    mockTasksData.data = [
      makeTask({
        id: "t-boundary2",
        title: "Boundary task 2",
        owner: "agent-a",
        due_at: daysFromToday(0),
      }),
    ];
    wrap(<CalendarApp />);
    await screen.findByTestId("week-grid");
    // columnheaders: index 0 is the empty agent label, index 1 is the first day (Monday)
    const headers = screen.getAllByRole("columnheader");
    // The Monday day header (index 1) should contain the day number
    expect(headers[1].textContent).toContain(String(dayNum));
  });
});

describe("CalendarApp — multiple tasks same day same agent", () => {
  it("shows all chips for multiple tasks on same day", async () => {
    const dueDate = daysFromToday(0);
    mockTasksData.data = [
      makeTask({
        id: "t-multi-1",
        title: "First task",
        owner: "multi-agent",
        due_at: dueDate,
      }),
      makeTask({
        id: "t-multi-2",
        title: "Second task",
        owner: "multi-agent",
        due_at: dueDate,
      }),
      makeTask({
        id: "t-multi-3",
        title: "Third task",
        owner: "multi-agent",
        due_at: dueDate,
      }),
    ];
    wrap(<CalendarApp />);
    expect(await screen.findByTestId("task-chip-t-multi-1")).toBeTruthy();
    expect(screen.getByTestId("task-chip-t-multi-2")).toBeTruthy();
    expect(screen.getByTestId("task-chip-t-multi-3")).toBeTruthy();
  });
});
