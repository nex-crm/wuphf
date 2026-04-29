import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ToastContainer } from "../../ui/Toast";
import { useRestoreToast } from "../RestoreToast";

vi.mock("../../../api/workspaces", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("../../../api/workspaces")>();
  return {
    ...actual,
    useRestoreWorkspace: vi.fn(),
  };
});

import { useRestoreWorkspace } from "../../../api/workspaces";

const useRestoreWorkspaceMock = vi.mocked(useRestoreWorkspace);

function Harness() {
  const { fire } = useRestoreToast();
  return (
    <div>
      <button
        type="button"
        data-testid="trigger-toast"
        onClick={() => fire("demo-launch", "trash-id-1")}
      >
        fire
      </button>
      <ToastContainer />
    </div>
  );
}

function renderHarness() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <Harness />
    </QueryClientProvider>,
  );
}

describe("useRestoreToast", () => {
  beforeEach(() => {
    useRestoreWorkspaceMock.mockReturnValue({
      mutate: vi.fn(),
      isPending: false,
    } as unknown as ReturnType<typeof useRestoreWorkspace>);
  });
  afterEach(() => {
    vi.clearAllMocks();
  });

  it("renders an undo toast on fire()", () => {
    renderHarness();
    fireEvent.click(screen.getByTestId("trigger-toast"));

    expect(
      screen.getByText(/Workspace 'demo-launch' shredded/i),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /undo/i })).toBeInTheDocument();
  });

  it("clicking Undo invokes the restore mutation with the trash_id", () => {
    const mutate = vi.fn();
    useRestoreWorkspaceMock.mockReturnValue({
      mutate,
      isPending: false,
    } as unknown as ReturnType<typeof useRestoreWorkspace>);
    renderHarness();

    fireEvent.click(screen.getByTestId("trigger-toast"));
    fireEvent.click(screen.getByRole("button", { name: /undo/i }));

    expect(mutate).toHaveBeenCalledWith({ trash_id: "trash-id-1" });
  });
});
