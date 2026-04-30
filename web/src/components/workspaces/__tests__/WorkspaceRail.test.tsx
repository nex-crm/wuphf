import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { Workspace, WorkspaceListResponse } from "../../../api/workspaces";
import { WorkspaceRail } from "../WorkspaceRail";

vi.mock("../../../api/workspaces", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("../../../api/workspaces")>();
  return {
    ...actual,
    useWorkspacesList: vi.fn(),
    usePauseWorkspace: vi.fn(),
    useResumeWorkspace: vi.fn(),
    useShredWorkspace: vi.fn(),
    useRestoreWorkspace: vi.fn(),
  };
});

vi.mock("../CreateWorkspaceModal", () => ({
  CreateWorkspaceModal: ({ open }: { open: boolean }) =>
    open ? <div data-testid="create-modal-mock">create</div> : null,
}));

import {
  usePauseWorkspace,
  useRestoreWorkspace,
  useResumeWorkspace,
  useShredWorkspace,
  useWorkspacesList,
} from "../../../api/workspaces";

const useWorkspacesListMock = vi.mocked(useWorkspacesList);
const usePauseWorkspaceMock = vi.mocked(usePauseWorkspace);
const useResumeWorkspaceMock = vi.mocked(useResumeWorkspace);
const useShredWorkspaceMock = vi.mocked(useShredWorkspace);
const useRestoreWorkspaceMock = vi.mocked(useRestoreWorkspace);

const mainWorkspace: Workspace = {
  name: "main",
  runtime_home: "/Users/me/.wuphf-spaces/main",
  broker_port: 7890,
  web_port: 7891,
  state: "running",
  blueprint: "founding-team",
  company_name: "Nex",
  created_at: "2026-01-01T00:00:00Z",
  last_used_at: "2026-04-28T12:00:00Z",
  is_active: true,
};

const demoWorkspace: Workspace = {
  name: "demo-launch",
  runtime_home: "/Users/me/.wuphf-spaces/demo-launch",
  broker_port: 7910,
  web_port: 7911,
  state: "paused",
  blueprint: "founding-team",
  company_name: "Acme Demo",
  created_at: "2026-04-15T00:00:00Z",
  last_used_at: "2026-04-22T19:00:00Z",
  paused_at: "2026-04-22T19:30:00Z",
};

const errorWorkspace: Workspace = {
  ...demoWorkspace,
  name: "broken",
  state: "error",
  is_active: false,
};

function setListData(workspaces: Workspace[], active?: string) {
  const payload: WorkspaceListResponse = { workspaces, active };
  useWorkspacesListMock.mockReturnValue({
    data: payload,
    isLoading: false,
    isError: false,
  } as unknown as ReturnType<typeof useWorkspacesList>);
}

function setMutationStubs() {
  const pauseMutate = vi.fn();
  const resumeMutate = vi.fn();
  const shredMutate = vi.fn();
  const restoreMutate = vi.fn();

  usePauseWorkspaceMock.mockReturnValue({
    mutate: pauseMutate,
    isPending: false,
  } as unknown as ReturnType<typeof usePauseWorkspace>);
  useResumeWorkspaceMock.mockReturnValue({
    mutate: resumeMutate,
    isPending: false,
  } as unknown as ReturnType<typeof useResumeWorkspace>);
  useShredWorkspaceMock.mockReturnValue({
    mutate: shredMutate,
    isPending: false,
  } as unknown as ReturnType<typeof useShredWorkspace>);
  useRestoreWorkspaceMock.mockReturnValue({
    mutate: restoreMutate,
    isPending: false,
  } as unknown as ReturnType<typeof useRestoreWorkspace>);

  return { pauseMutate, resumeMutate, shredMutate, restoreMutate };
}

function renderRail(navigate?: (url: string) => void) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <WorkspaceRail navigate={navigate} />
    </QueryClientProvider>,
  );
}

describe("<WorkspaceRail>", () => {
  beforeEach(() => {
    setMutationStubs();
  });
  afterEach(() => {
    vi.clearAllMocks();
  });

  it("renders one icon per workspace and the add button", () => {
    setListData([mainWorkspace, demoWorkspace], "main");
    renderRail();

    expect(screen.getByTestId("workspace-icon-main")).toBeInTheDocument();
    expect(
      screen.getByTestId("workspace-icon-demo-launch"),
    ).toBeInTheDocument();
    expect(screen.getByTestId("workspace-add-button")).toBeInTheDocument();
  });

  it("marks the active workspace and dims paused entries", () => {
    setListData([mainWorkspace, demoWorkspace], "main");
    renderRail();

    const mainIcon = screen.getByTestId("workspace-icon-main");
    const demoIcon = screen.getByTestId("workspace-icon-demo-launch");

    expect(mainIcon.getAttribute("data-active")).toBe("true");
    expect(demoIcon.getAttribute("data-active")).toBe("false");
    expect(demoIcon.getAttribute("data-state")).toBe("paused");
  });

  it("navigates to the target broker URL on click for non-active running workspace", () => {
    const running: Workspace = {
      ...demoWorkspace,
      name: "side-project",
      state: "running",
      web_port: 7912,
    };
    setListData([mainWorkspace, running], "main");
    const navigate = vi.fn();
    renderRail(navigate);

    fireEvent.click(screen.getByTestId("workspace-icon-side-project"));

    expect(navigate).toHaveBeenCalledWith("http://localhost:7912/");
  });

  it("opens a Resume modal when clicking a paused workspace", () => {
    setListData([mainWorkspace, demoWorkspace], "main");
    renderRail();

    fireEvent.click(screen.getByTestId("workspace-icon-demo-launch"));

    expect(screen.getByTestId("workspace-resume-modal")).toBeInTheDocument();
    expect(screen.getByTestId("workspace-resume-confirm")).toBeInTheDocument();
  });

  it("Resume confirm fires the resume mutation", () => {
    setListData([mainWorkspace, demoWorkspace], "main");
    const { resumeMutate } = setMutationStubs();
    renderRail();

    fireEvent.click(screen.getByTestId("workspace-icon-demo-launch"));
    fireEvent.click(screen.getByTestId("workspace-resume-confirm"));

    expect(resumeMutate).toHaveBeenCalledWith({ name: "demo-launch" });
  });

  it("right-click opens a kebab menu with workspace lifecycle actions", () => {
    setListData([mainWorkspace, demoWorkspace], "main");
    renderRail();

    const mainIcon = screen.getByTestId("workspace-icon-main");
    fireEvent.contextMenu(mainIcon);

    const menu = screen.getByTestId("workspace-menu-main");
    expect(menu).toBeInTheDocument();
    expect(within(menu).getByText("Pause")).toBeInTheDocument();
    expect(within(menu).getByText("Settings")).toBeInTheDocument();
    expect(within(menu).getByText("Shred…")).toBeInTheDocument();
  });

  it("Resume entry appears in kebab for paused workspaces only", () => {
    setListData([mainWorkspace, demoWorkspace], "main");
    renderRail();

    fireEvent.contextMenu(screen.getByTestId("workspace-icon-demo-launch"));
    const menu = screen.getByTestId("workspace-menu-demo-launch");
    expect(within(menu).getByText("Resume")).toBeInTheDocument();
    expect(within(menu).queryByText("Pause")).toBeNull();
  });

  it("shows the create modal when clicking the add button", () => {
    setListData([mainWorkspace], "main");
    renderRail();

    fireEvent.click(screen.getByTestId("workspace-add-button"));

    expect(screen.getByTestId("create-modal-mock")).toBeInTheDocument();
  });

  it("renders a tooltip on hover with workspace metadata", () => {
    setListData([mainWorkspace, demoWorkspace], "main");
    renderRail();

    const demoIcon = screen.getByTestId("workspace-icon-demo-launch");
    fireEvent.mouseEnter(demoIcon.parentElement as Element);

    const tooltip = screen.getByRole("tooltip");
    expect(tooltip.textContent).toContain("demo-launch");
    expect(tooltip.textContent).toContain("paused");
  });

  it("error-state workspaces show a notice instead of navigating", () => {
    setListData([mainWorkspace, errorWorkspace], "main");
    const navigate = vi.fn();
    renderRail(navigate);

    fireEvent.click(screen.getByTestId("workspace-icon-broken"));

    expect(navigate).not.toHaveBeenCalled();
  });

  it("opens the shred modal from the kebab menu", () => {
    setListData([mainWorkspace, demoWorkspace], "main");
    renderRail();

    fireEvent.contextMenu(screen.getByTestId("workspace-icon-demo-launch"));
    fireEvent.click(screen.getByTestId("workspace-menu-shred-demo-launch"));

    expect(screen.getByTestId("shred-confirm-submit")).toBeInTheDocument();
  });
});
