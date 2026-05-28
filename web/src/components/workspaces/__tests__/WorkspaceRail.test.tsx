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

// The Tools column + Footer pull in `useInboxCount` (TanStack Query against
// /inbox) + the route registry. Those branches aren't what WorkspaceRail's
// own tests are asserting, and mounting them unmocked makes useQuery throw
// `Cannot read properties of null (reading 'isServer')` in JSDOM. Stub them
// out so the tests focus on the switcher/modal/kebab contract.
vi.mock("../WorkspaceRailTools", () => ({
  WorkspaceRailTools: () => <div data-testid="workspace-rail-tools-stub" />,
  WorkspaceRailFooter: () => <div data-testid="workspace-rail-footer-stub" />,
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

  it("renders only the active workspace tile (others live in the switcher modal)", () => {
    setListData([mainWorkspace, demoWorkspace], "main");
    renderRail();

    expect(screen.getByTestId("workspace-icon-main")).toBeInTheDocument();
    // Non-active workspaces are NOT rendered inline; they appear inside the
    // switcher modal that opens when the active tile is clicked.
    expect(
      screen.queryByTestId("workspace-icon-demo-launch"),
    ).not.toBeInTheDocument();
  });

  it("marks the active workspace tile", () => {
    setListData([mainWorkspace, demoWorkspace], "main");
    renderRail();

    const mainIcon = screen.getByTestId("workspace-icon-main");
    expect(mainIcon.getAttribute("data-active")).toBe("true");
  });

  it("clicking the active tile opens the switcher modal listing other workspaces", () => {
    setListData([mainWorkspace, demoWorkspace], "main");
    renderRail();

    fireEvent.click(screen.getByTestId("workspace-icon-main"));

    expect(
      screen.getByTestId("workspace-switcher-item-demo-launch"),
    ).toBeInTheDocument();
    expect(
      screen.getByTestId("workspace-switcher-create"),
    ).toBeInTheDocument();
  });

  it("clicking a running workspace in the switcher navigates to its broker URL", () => {
    const running: Workspace = {
      ...demoWorkspace,
      name: "side-project",
      state: "running",
      web_port: 7912,
    };
    setListData([mainWorkspace, running], "main");
    const navigate = vi.fn();
    renderRail(navigate);

    fireEvent.click(screen.getByTestId("workspace-icon-main"));
    fireEvent.click(screen.getByTestId("workspace-switcher-item-side-project"));

    expect(navigate).toHaveBeenCalledWith("http://localhost:7912/");
  });

  it("clicking a paused workspace in the switcher opens the Resume modal", () => {
    setListData([mainWorkspace, demoWorkspace], "main");
    renderRail();

    fireEvent.click(screen.getByTestId("workspace-icon-main"));
    fireEvent.click(
      screen.getByTestId("workspace-switcher-item-demo-launch"),
    );

    expect(screen.getByTestId("workspace-resume-modal")).toBeInTheDocument();
    expect(screen.getByTestId("workspace-resume-confirm")).toBeInTheDocument();
  });

  it("Resume confirm fires the resume mutation", () => {
    setListData([mainWorkspace, demoWorkspace], "main");
    const { resumeMutate } = setMutationStubs();
    renderRail();

    fireEvent.click(screen.getByTestId("workspace-icon-main"));
    fireEvent.click(
      screen.getByTestId("workspace-switcher-item-demo-launch"),
    );
    fireEvent.click(screen.getByTestId("workspace-resume-confirm"));

    expect(resumeMutate).toHaveBeenCalledWith({ name: "demo-launch" });
  });

  it("right-click on the active tile opens the kebab menu with lifecycle actions", () => {
    setListData([mainWorkspace, demoWorkspace], "main");
    renderRail();

    fireEvent.contextMenu(screen.getByTestId("workspace-icon-main"));

    const menu = screen.getByTestId("workspace-menu-main");
    expect(menu).toBeInTheDocument();
    expect(within(menu).getByText("Pause")).toBeInTheDocument();
    expect(within(menu).getByText("Settings")).toBeInTheDocument();
    expect(within(menu).getByText("Shred…")).toBeInTheDocument();
  });

  it("kebab button on a switcher row opens the kebab without closing the modal", () => {
    setListData([mainWorkspace, demoWorkspace], "main");
    renderRail();

    fireEvent.click(screen.getByTestId("workspace-icon-main"));
    fireEvent.click(screen.getByTestId("workspace-switcher-menu-demo-launch"));

    // Kebab menu visible…
    expect(
      screen.getByTestId("workspace-menu-demo-launch"),
    ).toBeInTheDocument();
    // …and the switcher modal is still mounted underneath.
    expect(
      screen.getByTestId("workspace-switcher-item-demo-launch"),
    ).toBeInTheDocument();
  });

  it("Resume entry appears in the kebab for paused workspaces only", () => {
    setListData([mainWorkspace, demoWorkspace], "main");
    renderRail();

    fireEvent.click(screen.getByTestId("workspace-icon-main"));
    fireEvent.click(screen.getByTestId("workspace-switcher-menu-demo-launch"));

    const menu = screen.getByTestId("workspace-menu-demo-launch");
    expect(within(menu).getByText("Resume")).toBeInTheDocument();
    expect(within(menu).queryByText("Pause")).toBeNull();
  });

  it("shows the create modal when clicking the New workspace row in the switcher", () => {
    setListData([mainWorkspace], "main");
    renderRail();

    fireEvent.click(screen.getByTestId("workspace-icon-main"));
    fireEvent.click(screen.getByTestId("workspace-switcher-create"));

    expect(screen.getByTestId("create-modal-mock")).toBeInTheDocument();
  });

  it("renders a tooltip on hover with active workspace metadata", () => {
    setListData([mainWorkspace], "main");
    renderRail();

    const tile = screen.getByTestId("workspace-icon-main");
    fireEvent.mouseEnter(tile.parentElement as Element);

    const tooltip = screen.getByRole("tooltip");
    expect(tooltip.textContent).toContain("Nex");
    expect(tooltip.textContent).toContain("main");
    expect(tooltip.textContent).toContain("running");
  });

  it("falls back to the workspace name when company_name is missing", () => {
    const slugOnlyWorkspace: Workspace = {
      ...mainWorkspace,
      company_name: "",
    };
    setListData([slugOnlyWorkspace], "main");
    renderRail();

    const tile = screen.getByTestId("workspace-icon-main");
    fireEvent.mouseEnter(tile.parentElement as Element);

    const tooltip = screen.getByRole("tooltip");
    expect(tooltip.textContent).toContain("main");
    expect(tooltip.textContent).not.toContain("Nex");
  });

  it("falls back to the workspace name when company_name is whitespace", () => {
    const whitespaceWorkspace: Workspace = {
      ...mainWorkspace,
      company_name: "   ",
    };
    setListData([whitespaceWorkspace], "main");
    renderRail();

    const tile = screen.getByTestId("workspace-icon-main");
    fireEvent.mouseEnter(tile.parentElement as Element);

    const tooltip = screen.getByRole("tooltip");
    expect(tooltip.textContent).toContain("main");
    expect(tooltip.textContent).not.toContain("Nex");
  });

  it("does not duplicate the label when company_name matches name", () => {
    const sameLabelWorkspace: Workspace = {
      ...mainWorkspace,
      company_name: "main",
    };
    setListData([sameLabelWorkspace], "main");
    renderRail();

    const tile = screen.getByTestId("workspace-icon-main");
    fireEvent.mouseEnter(tile.parentElement as Element);

    // The tooltip should mention the workspace name exactly once. The
    // tooltip's textContent concatenates divs with no separator, so
    // \bword boundaries\b don't help — count raw substring occurrences
    // instead.
    const tooltip = screen.getByRole("tooltip");
    const matches = tooltip.textContent?.match(/main/g) ?? [];
    expect(matches).toHaveLength(1);
  });

  it("error-state workspaces in the switcher do not navigate", () => {
    setListData([mainWorkspace, errorWorkspace], "main");
    const navigate = vi.fn();
    renderRail(navigate);

    fireEvent.click(screen.getByTestId("workspace-icon-main"));
    fireEvent.click(screen.getByTestId("workspace-switcher-item-broken"));

    expect(navigate).not.toHaveBeenCalled();
  });

  it("opens the shred modal from the kebab menu", () => {
    setListData([mainWorkspace, demoWorkspace], "main");
    renderRail();

    fireEvent.click(screen.getByTestId("workspace-icon-main"));
    fireEvent.click(screen.getByTestId("workspace-switcher-menu-demo-launch"));
    fireEvent.click(screen.getByTestId("workspace-menu-shred-demo-launch"));

    expect(screen.getByTestId("shred-confirm-submit")).toBeInTheDocument();
  });
});
