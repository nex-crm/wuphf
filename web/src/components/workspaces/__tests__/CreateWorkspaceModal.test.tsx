import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { CreateWorkspaceModal } from "../CreateWorkspaceModal";

vi.mock("../../../api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../../../api/client")>();
  return {
    ...actual,
    getConfig: vi.fn(),
  };
});

vi.mock("../../../api/workspaces", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("../../../api/workspaces")>();
  return {
    ...actual,
    useCreateWorkspace: vi.fn(),
  };
});

vi.mock("../../onboarding/Wizard", () => ({
  Wizard: ({ onComplete }: { onComplete?: () => void }) => (
    <div data-testid="wizard-stub">
      <button type="button" onClick={onComplete}>
        Complete
      </button>
    </div>
  ),
}));

import { getConfig } from "../../../api/client";
import { useCreateWorkspace } from "../../../api/workspaces";

const getConfigMock = vi.mocked(getConfig);
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
    getConfigMock.mockResolvedValue({
      blueprint: "founding-team",
      company_name: "Nex",
      company_description: "Synthesis",
      company_priority: "Ship multi-workspace",
      llm_provider: "claude-code",
      team_lead_slug: "michael",
      config_path: "/Users/me/.wuphf/config.json",
    });
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

  it("renders inherit form by default with pre-filled fields", async () => {
    renderModal();

    await waitFor(() => {
      expect(
        (screen.getByLabelText(/Company name/i) as HTMLInputElement).value,
      ).toBe("Nex");
    });
    expect(
      (screen.getByLabelText(/Blueprint/i) as HTMLInputElement).value,
    ).toBe("founding-team");
    expect(
      (screen.getByLabelText(/LLM provider/i) as HTMLInputElement).value,
    ).toBe("claude-code");
  });

  it("validates the slug inline and disables submit when invalid", async () => {
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

  it("enables submit and calls the create mutation with form values", async () => {
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

    expect(mutate).toHaveBeenCalledWith(
      expect.objectContaining({
        name: "side-project",
        inherit_from_current: true,
      }),
    );
  });

  it("toggling 'Inherit from current' switches to the inline wizard", () => {
    renderModal();

    fireEvent.click(screen.getByTestId("inherit-toggle"));

    expect(screen.getByTestId("wizard-stub")).toBeInTheDocument();
  });

  it("Esc key closes the modal in form phase", () => {
    const { onClose } = renderModal();
    fireEvent.keyDown(document, { key: "Escape" });
    expect(onClose).toHaveBeenCalled();
  });
});
