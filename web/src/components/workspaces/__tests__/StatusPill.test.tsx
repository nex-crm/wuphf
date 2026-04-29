import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { UsageData } from "../../../api/client";
import type { Workspace, WorkspaceListResponse } from "../../../api/workspaces";
import { StatusPill } from "../StatusPill";

vi.mock("../../../api/workspaces", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("../../../api/workspaces")>();
  return {
    ...actual,
    useWorkspacesList: vi.fn(),
  };
});

vi.mock("../../../api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../../../api/client")>();
  return {
    ...actual,
    getUsage: vi.fn(),
  };
});

import { getUsage } from "../../../api/client";
import { useWorkspacesList } from "../../../api/workspaces";

const useWorkspacesListMock = vi.mocked(useWorkspacesList);
const getUsageMock = vi.mocked(getUsage);

function setListData(workspaces: Workspace[], active?: string) {
  const payload: WorkspaceListResponse = { workspaces, active };
  useWorkspacesListMock.mockReturnValue({
    data: payload,
    isLoading: false,
    isError: false,
  } as unknown as ReturnType<typeof useWorkspacesList>);
}

function renderPill(usage?: UsageData, override?: string) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <StatusPill usage={usage} workspaceName={override} />
    </QueryClientProvider>,
  );
}

describe("<StatusPill>", () => {
  beforeEach(() => {
    getUsageMock.mockResolvedValue({
      total: { cost_usd: 0, total_tokens: 12_400 },
      session: { total_tokens: 12_400 },
    });
  });
  afterEach(() => {
    vi.clearAllMocks();
  });

  it("renders the active workspace name and cost", () => {
    setListData(
      [
        {
          name: "main",
          runtime_home: "/r",
          broker_port: 7890,
          web_port: 7891,
          state: "running",
          is_active: true,
        },
      ],
      "main",
    );

    renderPill({ session: { total_tokens: 1_200 } });

    const pill = screen.getByTestId("workspace-status-pill");
    expect(pill.textContent).toContain("main");
    expect(pill.textContent).toContain("1.2k tokens today");
  });

  it("formats large counts as k/M", () => {
    setListData(
      [
        {
          name: "main",
          runtime_home: "/r",
          broker_port: 7890,
          web_port: 7891,
          state: "running",
          is_active: true,
        },
      ],
      "main",
    );

    renderPill({ session: { total_tokens: 2_500_000 } });
    expect(screen.getByTestId("workspace-status-pill").textContent).toContain(
      "2.5M",
    );
  });

  it("uses the override workspace name when provided", () => {
    setListData([], undefined);
    renderPill({ session: { total_tokens: 0 } }, "demo-launch");

    expect(screen.getByTestId("workspace-status-pill").textContent).toContain(
      "demo-launch",
    );
  });

  it("falls back to 'main' when no active workspace is reported", () => {
    setListData([], undefined);
    renderPill({ session: { total_tokens: 50 } });

    expect(screen.getByTestId("workspace-status-pill").textContent).toContain(
      "main",
    );
    expect(screen.getByTestId("workspace-status-pill").textContent).toContain(
      "50 tokens today",
    );
  });
});
