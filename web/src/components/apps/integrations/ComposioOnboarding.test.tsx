import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

const updateConfig = vi.fn();
vi.mock("../../../api/client", () => ({
  updateConfig: (patch: unknown) => updateConfig(patch),
}));

import { ComposioOnboarding } from "./ComposioOnboarding";

function wrap(ui: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{ui}</QueryClientProvider>;
}

describe("<ComposioOnboarding>", () => {
  it("renders the connect form and a get-key link", () => {
    render(wrap(<ComposioOnboarding onConnected={() => {}} />));
    expect(
      screen.getByRole("heading", { name: /connect composio/i }),
    ).toBeInTheDocument();
    expect(screen.getByLabelText(/composio api key/i)).toBeInTheDocument();
    const link = screen.getByRole("link", { name: /get an api key/i });
    expect(link).toHaveAttribute("href", "https://app.composio.dev/developers");
  });

  it("disables connect until a key is entered", () => {
    render(wrap(<ComposioOnboarding onConnected={() => {}} />));
    expect(
      screen.getByRole("button", { name: /connect composio/i }),
    ).toBeDisabled();
    fireEvent.change(screen.getByLabelText(/composio api key/i), {
      target: { value: "comp_abc123" },
    });
    expect(
      screen.getByRole("button", { name: /connect composio/i }),
    ).toBeEnabled();
  });

  it("saves the key via updateConfig and notifies on success", async () => {
    updateConfig.mockResolvedValue({ status: "ok" });
    const onConnected = vi.fn();
    render(wrap(<ComposioOnboarding onConnected={onConnected} />));
    fireEvent.change(screen.getByLabelText(/composio api key/i), {
      target: { value: "  comp_abc123  " },
    });
    fireEvent.click(screen.getByRole("button", { name: /connect composio/i }));
    await waitFor(() =>
      expect(updateConfig).toHaveBeenCalledWith({ composio_api_key: "comp_abc123" }),
    );
    await waitFor(() => expect(onConnected).toHaveBeenCalled());
  });

  it("toggles key visibility", () => {
    render(wrap(<ComposioOnboarding onConnected={() => {}} />));
    const input = screen.getByLabelText(/composio api key/i);
    expect(input).toHaveAttribute("type", "password");
    fireEvent.click(screen.getByRole("button", { name: "Show" }));
    expect(input).toHaveAttribute("type", "text");
  });
});
