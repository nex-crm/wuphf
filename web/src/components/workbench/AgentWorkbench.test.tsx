import type { ComponentProps } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { getOfficeMembers } from "../../api/client";
import { getOfficeTasks, listAgentLogTasks, type Task } from "../../api/tasks";
import { AgentWorkbench } from "./AgentWorkbench";

vi.mock("../../api/client", () => ({
  getOfficeMembers: vi.fn(),
}));

vi.mock("../../api/tasks", () => ({
  getOfficeTasks: vi.fn(),
  listAgentLogTasks: vi.fn(),
}));

vi.mock("../agents/AgentTerminal", () => ({
  AgentTerminal: ({ slug }: { slug: string | null }) => (
    <div data-testid="agent-terminal">terminal:{slug}</div>
  ),
}));

const getOfficeMembersMock = getOfficeMembers as ReturnType<typeof vi.fn>;
const getOfficeTasksMock = getOfficeTasks as ReturnType<typeof vi.fn>;
const listAgentLogTasksMock = listAgentLogTasks as ReturnType<typeof vi.fn>;

function renderWorkbench(props?: ComponentProps<typeof AgentWorkbench>) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <AgentWorkbench {...props} />
    </QueryClientProvider>,
  );
}

const researchTask: Task = {
  id: "task-123",
  title: "Map onboarding evidence",
  description: "Collect prior decisions and record a reusable summary.",
  status: "in_progress",
  owner: "alpha",
  channel: "ops",
  updated_at: "2026-05-01T10:00:00Z",
  memory_workflow: {
    required: true,
    status: "pending",
    requirement_reason: "Durable context is required before review.",
    required_steps: ["lookup", "capture", "promote"],
    lookup: {
      required: true,
      status: "satisfied",
      count: 2,
      completed_at: "2026-05-01T09:30:00Z",
    },
    capture: {
      required: true,
      status: "satisfied",
      count: 1,
      completed_at: "2026-05-01T09:45:00Z",
    },
    promote: { required: true, status: "pending" },
    citations: [
      {
        title: "Onboarding notes",
        path: "team/onboarding.md",
        source: "wiki",
      },
    ],
    captures: [
      {
        title: "Workbench capture",
        path: "agents/alpha/workbench.md",
        source: "notebook",
        state: "captured",
      },
    ],
    promotions: [
      {
        title: "Review brief",
        path: "team/review.md",
        source: "wiki",
        state: "ready",
      },
    ],
    updated_at: "2026-05-01T10:05:00Z",
  },
};

describe("AgentWorkbench", () => {
  beforeEach(() => {
    getOfficeMembersMock.mockResolvedValue({
      members: [{ slug: "alpha", name: "Alpha", role: "Research" }],
    });
    getOfficeTasksMock.mockResolvedValue({ tasks: [researchTask] });
    listAgentLogTasksMock.mockResolvedValue({
      tasks: [
        {
          taskId: "task-123",
          agentSlug: "alpha",
          toolCallCount: 4,
          lastToolAt: Date.UTC(2026, 4, 1, 10, 10),
          sizeBytes: 2048,
        },
      ],
    });
  });

  it("renders task context, evidence, recent runs, and terminal", async () => {
    renderWorkbench({ agentSlug: "alpha", taskId: "task-123" });

    expect(
      await screen.findByRole("heading", { level: 2, name: "Alpha" }),
    ).toBeInTheDocument();
    expect(
      screen.getAllByText("Map onboarding evidence").length,
    ).toBeGreaterThan(0);
    expect(screen.getByText("memory 2/3")).toBeInTheDocument();
    expect(screen.getByText(/Onboarding notes/)).toBeInTheDocument();
    expect(screen.getByText(/Workbench capture/)).toBeInTheDocument();
    expect(screen.getByText(/Review brief/)).toBeInTheDocument();
    expect(screen.getByTestId("agent-terminal")).toHaveTextContent(
      "terminal:alpha",
    );

    const recentRuns = screen.getByRole("heading", { name: "Recent runs" })
      .parentElement?.parentElement;
    expect(recentRuns).toBeTruthy();
    expect(
      within(recentRuns as HTMLElement).getByText("task-123"),
    ).toBeInTheDocument();
  });

  it("switches context when a task is selected from recent runs", async () => {
    const secondTask: Task = {
      id: "task-456",
      title: "Ship review packet",
      status: "review",
      owner: "alpha",
    };
    getOfficeTasksMock.mockResolvedValue({
      tasks: [researchTask, secondTask],
    });
    listAgentLogTasksMock.mockResolvedValue({
      tasks: [
        {
          taskId: "task-456",
          agentSlug: "alpha",
          toolCallCount: 1,
          lastToolAt: Date.UTC(2026, 4, 1, 11, 0),
          sizeBytes: 512,
        },
        {
          taskId: "task-123",
          agentSlug: "alpha",
          toolCallCount: 4,
          lastToolAt: Date.UTC(2026, 4, 1, 10, 10),
          sizeBytes: 2048,
        },
      ],
    });

    renderWorkbench({ agentSlug: "alpha" });

    await screen.findByRole("button", { name: /task-123/i });
    expect(screen.getAllByText("Ship review packet").length).toBeGreaterThan(0);
    await userEvent.click(screen.getByRole("button", { name: /task-123/i }));
    expect(
      screen.getAllByText("Map onboarding evidence").length,
    ).toBeGreaterThan(0);
  });

  it("derives the agent from run data for a task-specific deep link", async () => {
    renderWorkbench({ taskId: "task-123" });

    expect(
      await screen.findByRole("heading", { level: 2, name: "Alpha" }),
    ).toBeInTheDocument();
    expect(screen.getAllByText("@alpha").length).toBeGreaterThan(0);
    expect(screen.getByText("#task-123")).toBeInTheDocument();
    expect(screen.getByTestId("agent-terminal")).toHaveTextContent(
      "terminal:alpha",
    );
  });

  it("shows a polished empty state when no data is available", async () => {
    getOfficeMembersMock.mockResolvedValue({ members: [] });
    getOfficeTasksMock.mockResolvedValue({ tasks: [] });
    listAgentLogTasksMock.mockResolvedValue({ tasks: [] });

    renderWorkbench();

    expect(await screen.findByText("No workbench data")).toBeInTheDocument();
    expect(
      screen.getByText(
        "Pick an agent or task with recent activity to populate this view.",
      ),
    ).toBeInTheDocument();
  });
});
