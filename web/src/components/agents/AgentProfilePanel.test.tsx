import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

// Hoist all mocks before imports so vitest can intercept module resolution.
const getSkillsListMock = vi.hoisted(() => vi.fn());
const getChannelsMock = vi.hoisted(() => vi.fn());
const getOfficeTasksMock = vi.hoisted(() => vi.fn());
const listAgentLogTasksMock = vi.hoisted(() => vi.fn());
const navigateMock = vi.hoisted(() => vi.fn());

vi.mock("../../api/client", async () => {
  const actual =
    await vi.importActual<typeof import("../../api/client")>(
      "../../api/client",
    );
  return {
    ...actual,
    getSkillsList: getSkillsListMock,
    getChannels: getChannelsMock,
  };
});

vi.mock("../../api/tasks", async () => {
  const actual =
    await vi.importActual<typeof import("../../api/tasks")>("../../api/tasks");
  return {
    ...actual,
    getOfficeTasks: getOfficeTasksMock,
    listAgentLogTasks: listAgentLogTasksMock,
  };
});

vi.mock("../../hooks/useConfig", () => ({
  useDefaultHarness: () => "claude-code",
}));

vi.mock("../../lib/router", () => ({
  router: { navigate: navigateMock },
}));

vi.mock("../../lib/harness", () => ({
  resolveHarness: (_provider: unknown, fallback: string) => fallback,
}));

vi.mock("../ui/PixelAvatar", () => ({
  PixelAvatar: ({ slug }: { slug: string }) => (
    <span data-testid={`avatar-${slug}`} />
  ),
}));

vi.mock("../ui/HarnessBadge", () => ({
  HarnessBadge: () => null,
}));

import type { OfficeMember } from "../../api/client";
import { AgentProfilePanel } from "./AgentProfilePanel";

function makeQC() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function wrap(ui: ReactNode, qc = makeQC()) {
  return <QueryClientProvider client={qc}>{ui}</QueryClientProvider>;
}

const baseAgent: OfficeMember = {
  slug: "planner",
  name: "Planner",
  role: "Plans tasks and coordinates agents",
  status: "active",
  provider: { kind: "claude-code", model: "claude-3-5-sonnet" },
  built_in: false,
};

describe("<AgentProfilePanel>", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    getSkillsListMock.mockResolvedValue({ skills: [] });
    getChannelsMock.mockResolvedValue({ channels: [] });
    getOfficeTasksMock.mockResolvedValue({ tasks: [] });
    listAgentLogTasksMock.mockResolvedValue({ tasks: [] });
  });

  it("renders agent name, status badge, and close button", async () => {
    const onClose = vi.fn();
    render(wrap(<AgentProfilePanel agent={baseAgent} onClose={onClose} />));

    expect(screen.getByText("Planner")).toBeInTheDocument();
    expect(screen.getByLabelText("Close agent profile")).toBeInTheDocument();
    // Status badge should say "active"
    expect(screen.getByText("active")).toBeInTheDocument();
  });

  it("renders agent role description", async () => {
    render(wrap(<AgentProfilePanel agent={baseAgent} onClose={vi.fn()} />));
    expect(
      screen.getByText("Plans tasks and coordinates agents"),
    ).toBeInTheDocument();
  });

  it("calls onClose when close button is clicked", async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();
    render(wrap(<AgentProfilePanel agent={baseAgent} onClose={onClose} />));

    await user.click(screen.getByLabelText("Close agent profile"));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("renders empty-state messages when all data is missing", async () => {
    render(wrap(<AgentProfilePanel agent={baseAgent} onClose={vi.fn()} />));

    await waitFor(() => {
      expect(screen.getByText("No skills yet")).toBeInTheDocument();
    });
    expect(screen.getByText("No channels")).toBeInTheDocument();
    expect(screen.getByText("No recent tasks")).toBeInTheDocument();
  });

  it("renders skills owned by this agent", async () => {
    getSkillsListMock.mockResolvedValue({
      skills: [
        {
          name: "plan-sprint",
          title: "Plan Sprint",
          status: "active",
          owner_agents: ["planner"],
        },
        {
          name: "review-pr",
          title: "Review PR",
          status: "active",
          owner_agents: ["reviewer"],
        },
        {
          name: "pending-skill",
          title: "Pending Skill",
          status: "proposed",
          owner_agents: ["planner"],
        },
      ],
    });

    render(wrap(<AgentProfilePanel agent={baseAgent} onClose={vi.fn()} />));

    await waitFor(() => {
      expect(screen.getByText("Plan Sprint")).toBeInTheDocument();
    });
    // Skill owned by a different agent should NOT appear
    expect(screen.queryByText("Review PR")).not.toBeInTheDocument();
    // Proposed skill shows a "pending" badge
    expect(screen.getByText("Pending Skill")).toBeInTheDocument();
    expect(screen.getByText("pending")).toBeInTheDocument();
  });

  it("renders channels the agent is a member of", async () => {
    getChannelsMock.mockResolvedValue({
      channels: [
        { slug: "general", name: "general", members: ["planner", "ceo"] },
        { slug: "engineering", name: "engineering", members: ["planner"] },
        { slug: "marketing", name: "marketing", members: ["ceo"] },
      ],
    });

    render(wrap(<AgentProfilePanel agent={baseAgent} onClose={vi.fn()} />));

    await waitFor(() => {
      expect(screen.getByText("#general")).toBeInTheDocument();
    });
    expect(screen.getByText("#engineering")).toBeInTheDocument();
    // marketing doesn't include "planner"
    expect(screen.queryByText("#marketing")).not.toBeInTheDocument();
  });

  it("renders recent runs when log tasks are available", async () => {
    // The API is called with agentSlug so the server filters; the mock
    // returns only the current agent's runs, matching real server behavior.
    listAgentLogTasksMock.mockResolvedValue({
      tasks: [
        {
          taskId: "task-abc-123",
          agentSlug: "planner",
          toolCallCount: 5,
          hasError: false,
          sizeBytes: 1000,
        },
      ],
    });

    render(wrap(<AgentProfilePanel agent={baseAgent} onClose={vi.fn()} />));

    await waitFor(() => {
      expect(screen.getByText("task-abc-123")).toBeInTheDocument();
    });
    // Verify the API was called with the agent-scoped params
    expect(listAgentLogTasksMock).toHaveBeenCalledWith({
      limit: 8,
      agentSlug: "planner",
    });
    // "See all activity" link should render
    expect(screen.getByText("See all activity")).toBeInTheDocument();
  });

  it("renders recent tasks for the agent", async () => {
    getOfficeTasksMock.mockResolvedValue({
      tasks: [
        {
          id: "t1",
          title: "Build feature X",
          status: "in_progress",
          owner: "planner",
          updated_at: "2026-05-07T10:00:00Z",
        },
        {
          id: "t2",
          title: "Unrelated task",
          status: "open",
          owner: "reviewer",
          updated_at: "2026-05-07T09:00:00Z",
        },
      ],
    });

    render(wrap(<AgentProfilePanel agent={baseAgent} onClose={vi.fn()} />));

    await waitFor(() => {
      expect(screen.getByText("Build feature X")).toBeInTheDocument();
    });
    expect(screen.queryByText("Unrelated task")).not.toBeInTheDocument();
  });

  it("renders permissions section with correct role for non-lead agent", async () => {
    render(wrap(<AgentProfilePanel agent={baseAgent} onClose={vi.fn()} />));

    await waitFor(() => {
      // permissions section
      expect(screen.getByText("team member")).toBeInTheDocument();
    });
    // removable: yes
    expect(screen.getAllByText("yes").length).toBeGreaterThan(0);
  });

  it("renders permissions section with correct role for built-in (lead) agent", async () => {
    const leadAgent: OfficeMember = {
      ...baseAgent,
      slug: "ceo",
      name: "CEO",
      built_in: true,
    };
    render(wrap(<AgentProfilePanel agent={leadAgent} onClose={vi.fn()} />));

    await waitFor(() => {
      expect(screen.getByText("lead agent")).toBeInTheDocument();
    });
    // removable: no
    const noEls = screen.getAllByText("no");
    expect(noEls.length).toBeGreaterThan(0);
  });

  it("handles missing optional fields on agent gracefully", async () => {
    const minimalAgent: OfficeMember = {
      slug: "bare",
      name: "Bare",
      role: "",
      status: undefined,
    };
    render(wrap(<AgentProfilePanel agent={minimalAgent} onClose={vi.fn()} />));

    // Should not throw; name renders
    expect(screen.getByText("Bare")).toBeInTheDocument();
    // Status defaults to "idle"
    expect(screen.getByText("idle")).toBeInTheDocument();
    // Role description section (the `<p>` text) should not render when role is empty
    expect(
      screen.queryByText("Plans tasks and coordinates agents"),
    ).not.toBeInTheDocument();
  });

  it("navigates to activity app when 'See all activity' is clicked", async () => {
    const user = userEvent.setup();
    listAgentLogTasksMock.mockResolvedValue({
      tasks: [
        {
          taskId: "task-1",
          agentSlug: "planner",
          toolCallCount: 3,
          hasError: false,
          sizeBytes: 100,
        },
      ],
    });

    render(wrap(<AgentProfilePanel agent={baseAgent} onClose={vi.fn()} />));

    await waitFor(() => {
      expect(screen.getByText("See all activity")).toBeInTheDocument();
    });

    await user.click(screen.getByText("See all activity"));
    expect(navigateMock).toHaveBeenCalledWith({
      to: "/apps/$appId",
      params: { appId: "activity" },
    });
  });
});
