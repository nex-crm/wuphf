import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { CreateWorkspaceModal } from "../CreateWorkspaceModal";

vi.mock("../../../api/workspaces", async () => {
  const actual = await vi.importActual<
    typeof import("../../../api/workspaces")
  >("../../../api/workspaces");
  return {
    ...actual,
    useCreateWorkspace: vi.fn(),
  };
});

import { useCreateWorkspace } from "../../../api/workspaces";

const useCreateWorkspaceMock = vi.mocked(useCreateWorkspace);

function renderModal(open = true) {
  const onClose = vi.fn();
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const result = render(
    <QueryClientProvider client={client}>
      <CreateWorkspaceModal open={open} onClose={onClose} />
    </QueryClientProvider>,
  );
  return { onClose, ...result };
}

describe("<CreateWorkspaceModal>", () => {
  beforeEach(() => {
    useCreateWorkspaceMock.mockReturnValue({
      mutate: vi.fn(),
      isPending: false,
    } as unknown as ReturnType<typeof useCreateWorkspace>);
  });
  afterEach(() => {
    vi.clearAllMocks();
  });

  it("renders nothing when closed", () => {
    const { container } = renderModal(false);
    expect(container.firstChild).toBeNull();
  });

  it("renders just the name input by default", () => {
    renderModal();
    expect(screen.getByTestId("workspace-slug-input")).toBeInTheDocument();
    // No inherit toggle, no blueprint/company/LLM fields
    expect(screen.queryByTestId("inherit-toggle")).toBeNull();
    expect(screen.queryByLabelText(/Company name/i)).toBeNull();
    expect(screen.queryByLabelText(/Blueprint/i)).toBeNull();
  });

  it("validates the slug inline and disables submit when invalid", () => {
    renderModal();
    const input = screen.getByTestId(
      "workspace-slug-input",
    ) as HTMLInputElement;
    fireEvent.change(input, { target: { value: "9bad-start" } });

    expect(screen.getByTestId("workspace-slug-error").textContent).toMatch(
      /lowercase letters/i,
    );
    expect(
      (screen.getByTestId("workspace-create-submit") as HTMLButtonElement)
        .disabled,
    ).toBe(true);
  });

  it("rejects reserved names with a helpful message", () => {
    renderModal();
    const input = screen.getByTestId(
      "workspace-slug-input",
    ) as HTMLInputElement;
    fireEvent.change(input, { target: { value: "main" } });
    expect(screen.getByTestId("workspace-slug-error").textContent).toMatch(
      /reserved/i,
    );

    fireEvent.change(input, { target: { value: "trash" } });
    expect(screen.getByTestId("workspace-slug-error").textContent).toMatch(
      /reserved/i,
    );
  });

  it("calls create mutation with from_scratch=true on submit", async () => {
    const mutate = vi.fn();
    useCreateWorkspaceMock.mockReturnValue({
      mutate,
      isPending: false,
    } as unknown as ReturnType<typeof useCreateWorkspace>);
    renderModal();

    const input = screen.getByTestId(
      "workspace-slug-input",
    ) as HTMLInputElement;
    fireEvent.change(input, { target: { value: "side-project" } });

    await waitFor(() => {
      expect(
        (screen.getByTestId("workspace-create-submit") as HTMLButtonElement)
          .disabled,
      ).toBe(false);
    });

    fireEvent.click(screen.getByTestId("workspace-create-submit"));

    expect(mutate).toHaveBeenCalledWith({
      name: "side-project",
      from_scratch: true,
    });
  });

  it("navigates to /onboarding?skip_identity=1 on the new broker after success", async () => {
    const assign = vi.fn();
    const originalLocation = window.location;
    Object.defineProperty(window, "location", {
      value: { assign },
      writable: true,
    });
    afterEach(() => {
      Object.defineProperty(window, "location", {
        value: originalLocation,
        writable: true,
      });
    });

    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    useCreateWorkspaceMock.mockImplementation(((opts?: any) => ({
      mutate: (input: unknown) => {
        opts?.onSuccess?.(
          {
            name: "side-project",
            broker_port: 7910,
            web_port: 7911,
            runtime_home: "/tmp/x",
            state: "running",
          },
          input,
          undefined,
          {} as never,
        );
      },
      isPending: false,
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
    })) as unknown as typeof useCreateWorkspace);

    renderModal();

    const input = screen.getByTestId(
      "workspace-slug-input",
    ) as HTMLInputElement;
    fireEvent.change(input, { target: { value: "side-project" } });
    fireEvent.click(screen.getByTestId("workspace-create-submit"));

    await waitFor(() => {
      expect(assign).toHaveBeenCalledWith(
        "http://localhost:7911/onboarding?skip_identity=1",
      );
    });
  });

  it("Esc key closes the modal in form phase", () => {
    const { onClose } = renderModal();
    fireEvent.keyDown(document, { key: "Escape" });
    expect(onClose).toHaveBeenCalled();
  });
});
