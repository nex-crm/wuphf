import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const postMock = vi.hoisted(() => vi.fn());

vi.mock("../../api/client", async () => {
  const actual =
    await vi.importActual<typeof import("../../api/client")>(
      "../../api/client",
    );
  return {
    ...actual,
    generateAgent: vi.fn(),
    post: postMock,
  };
});

import { AgentWizard } from "./AgentWizard";

function wrap(ui: ReactNode) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={qc}>{ui}</QueryClientProvider>;
}

describe("<AgentWizard>", () => {
  beforeEach(() => {
    postMock.mockReset();
  });

  it("closes on Escape from the window while open", () => {
    const onClose = vi.fn();
    render(wrap(<AgentWizard open={true} onClose={onClose} />));

    fireEvent.keyDown(window, { key: "Escape" });

    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("does not close on Escape while creating an agent", async () => {
    postMock.mockImplementation(
      () =>
        new Promise(() => {
          /* never resolves */
        }),
    );
    const onClose = vi.fn();
    render(wrap(<AgentWizard open={true} onClose={onClose} />));

    fireEvent.click(screen.getByRole("button", { name: "Manual" }));
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Revenue Ops" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create" }));

    await waitFor(() => expect(postMock).toHaveBeenCalled());
    fireEvent.keyDown(window, { key: "Escape" });

    expect(onClose).not.toHaveBeenCalled();
  });
});
