import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { TaskComposer } from "./TaskComposer";

// Composer reads config + local-provider status and the office roster, and
// creates tasks through the api/tasks client. Stub all three so the tests pin
// the composer's own contract: CEO-by-default ownership and honest copy.
vi.mock("../../api/client", async () => {
  const actual =
    await vi.importActual<typeof import("../../api/client")>(
      "../../api/client",
    );
  return {
    ...actual,
    getConfig: vi.fn(),
    getLocalProvidersStatus: vi.fn(),
  };
});

vi.mock("../../api/tasks", async () => {
  const actual =
    await vi.importActual<typeof import("../../api/tasks")>("../../api/tasks");
  return {
    ...actual,
    createTasks: vi.fn(),
  };
});

vi.mock("../../hooks/useMembers", () => ({
  useOfficeMembers: vi.fn(),
}));

vi.mock("../../lib/router", () => ({
  router: { navigate: vi.fn().mockResolvedValue(undefined) },
}));

import { getConfig, getLocalProvidersStatus } from "../../api/client";
import { createTasks } from "../../api/tasks";
import { useOfficeMembers } from "../../hooks/useMembers";

const getConfigMock = vi.mocked(getConfig);
const getLocalProvidersStatusMock = vi.mocked(getLocalProvidersStatus);
const createTasksMock = vi.mocked(createTasks);
const useOfficeMembersMock = vi.mocked(useOfficeMembers);

function renderComposer() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <TaskComposer />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  getConfigMock.mockResolvedValue({
    team_lead_slug: "ceo",
    llm_provider: "claude-code",
    llm_provider_kinds: ["claude-code", "codex"],
  });
  getLocalProvidersStatusMock.mockResolvedValue([]);
  createTasksMock.mockResolvedValue({ tasks: [] });
  useOfficeMembersMock.mockReturnValue({
    data: [
      { slug: "ceo", name: "CEO", role: "lead" },
      { slug: "builder", name: "Builder", role: "engineer" },
    ],
  } as unknown as ReturnType<typeof useOfficeMembers>);
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("TaskComposer owner default", () => {
  // Regression (live smoke run): the owner picker defaulted to "Auto", so a
  // new user's first task landed ownerless and parked behind CEO staffing —
  // indistinguishable from the removed approval wall.
  it("defaults the owner picker to the CEO, not Auto", async () => {
    renderComposer();
    const select = screen.getByTestId(
      "task-composer-owner",
    ) as HTMLSelectElement;
    await waitFor(() => {
      expect(select.value).toBe("ceo");
    });
  });

  it("keeps Auto available as an explicit option", () => {
    renderComposer();
    const select = screen.getByTestId(
      "task-composer-owner",
    ) as HTMLSelectElement;
    const values = Array.from(select.options).map((o) => o.value);
    expect(values).toContain("auto");
  });

  it("creates the first task assigned to the CEO by default", async () => {
    renderComposer();
    fireEvent.change(screen.getByTestId("task-composer-input"), {
      target: { value: "Ship the Q3 outbound sequence" },
    });
    fireEvent.click(screen.getByTestId("task-composer-start"));
    await waitFor(() => {
      expect(createTasksMock).toHaveBeenCalled();
    });
    const [tasks] = createTasksMock.mock.calls[0];
    expect(tasks[0].assignee).toBe("ceo");
  });
});

describe("TaskComposer copy", () => {
  // Regression: post-R2 there is no spec/approval gate before execution, but
  // the subtitle still promised one ("gets your approval"). Honest copy only.
  it("does not promise an approval gate that no longer exists", () => {
    renderComposer();
    expect(screen.queryByText(/gets your approval/i)).not.toBeInTheDocument();
    expect(screen.getByText(/starts on it immediately/i)).toBeInTheDocument();
  });
});
