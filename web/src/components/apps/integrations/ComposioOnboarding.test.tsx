import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

const updateConfig = vi.fn();
const startComposioSignin = vi.fn();
const getComposioSigninStatus = vi.fn();
vi.mock("../../../api/client", () => ({
  updateConfig: (patch: unknown) => updateConfig(patch),
}));
vi.mock("../../../api/integrations", () => ({
  startComposioSignin: () => startComposioSignin(),
  getComposioSigninStatus: () => getComposioSigninStatus(),
}));

import { ComposioOnboarding } from "./ComposioOnboarding";

function wrap(ui: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{ui}</QueryClientProvider>;
}

function expandManualFallback() {
  fireEvent.click(screen.getByRole("button", { name: /or paste an api key/i }));
}

afterEach(() => {
  vi.clearAllMocks();
  vi.unstubAllGlobals();
});

describe("<ComposioOnboarding>", () => {
  it("renders Sign in with Composio as the primary CTA and hides the paste form", () => {
    render(wrap(<ComposioOnboarding onConnected={() => {}} />));
    expect(
      screen.getByRole("button", { name: /connect integrations/i }),
    ).toBeEnabled();
    // The manual paste path is a collapsed secondary fallback.
    expect(screen.queryByLabelText(/api key/i)).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /or paste an api key/i }),
    ).toBeInTheDocument();
  });

  it("shows the CLI install command with a copy affordance when the CLI is missing", async () => {
    startComposioSignin.mockResolvedValue({
      status: "cli_missing",
      install_command: "curl -fsSL https://composio.dev/install | bash",
    });
    render(wrap(<ComposioOnboarding onConnected={() => {}} />));
    fireEvent.click(
      screen.getByRole("button", { name: /connect integrations/i }),
    );
    await waitFor(() =>
      expect(
        screen.getByText("curl -fsSL https://composio.dev/install | bash"),
      ).toBeInTheDocument(),
    );
    expect(screen.getByRole("button", { name: /copy/i })).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /try again/i }),
    ).toBeInTheDocument();
  });

  it("handles the installing status: shows the setup panel and keeps polling", async () => {
    // Regression: the broker auto-installs the CLI and returns `installing`
    // first. The page previously had no case for it — applySigninState fell
    // through to default and polling stayed disabled, so "Sign in with
    // Composio" silently did nothing. It must now render a working state and
    // keep polling so it can advance to awaiting_login / done.
    vi.stubGlobal("open", vi.fn());
    startComposioSignin.mockResolvedValue({
      status: "installing",
      install_command: "curl -fsSL https://composio.dev/install | bash",
    });
    getComposioSigninStatus.mockResolvedValue({ status: "installing" });
    render(wrap(<ComposioOnboarding onConnected={() => {}} />));
    fireEvent.click(
      screen.getByRole("button", { name: /connect integrations/i }),
    );
    await waitFor(() =>
      expect(screen.getByText(/setting up integrations/i)).toBeInTheDocument(),
    );
    await waitFor(() => expect(getComposioSigninStatus).toHaveBeenCalled());
  });

  it("shows the auth link and auto-opens it once while awaiting login", async () => {
    const open = vi.fn();
    vi.stubGlobal("open", open);
    startComposioSignin.mockResolvedValue({
      status: "awaiting_login",
      auth_url: "https://platform.composio.dev/?cliKey=sess_1",
    });
    getComposioSigninStatus.mockResolvedValue({ status: "awaiting_login" });
    render(wrap(<ComposioOnboarding onConnected={() => {}} />));
    fireEvent.click(
      screen.getByRole("button", { name: /connect integrations/i }),
    );
    await waitFor(() =>
      expect(
        screen.getByText(/finish signing in in your browser/i),
      ).toBeInTheDocument(),
    );
    expect(
      screen.getByRole("link", { name: /open the sign-in link/i }),
    ).toHaveAttribute("href", "https://platform.composio.dev/?cliKey=sess_1");
    expect(open).toHaveBeenCalledTimes(1);
    expect(open).toHaveBeenCalledWith(
      "https://platform.composio.dev/?cliKey=sess_1",
      "_blank",
      "noopener",
    );
  });

  it("transitions to connected when the status poll reports done", async () => {
    vi.stubGlobal("open", vi.fn());
    startComposioSignin.mockResolvedValue({
      status: "awaiting_login",
      auth_url: "https://platform.composio.dev/?cliKey=sess_2",
    });
    getComposioSigninStatus.mockResolvedValue({ status: "done" });
    const onConnected = vi.fn();
    render(wrap(<ComposioOnboarding onConnected={onConnected} />));
    fireEvent.click(
      screen.getByRole("button", { name: /connect integrations/i }),
    );
    await waitFor(() => expect(onConnected).toHaveBeenCalled());
  });

  it("surfaces a sign-in error with the broker's reason", async () => {
    startComposioSignin.mockResolvedValue({
      status: "error",
      reason: "composio login could not start: exit status 1",
    });
    render(wrap(<ComposioOnboarding onConnected={() => {}} />));
    fireEvent.click(
      screen.getByRole("button", { name: /connect integrations/i }),
    );
    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent(
        /composio login could not start/i,
      ),
    );
    // Recoverable: the primary CTA is back.
    expect(
      screen.getByRole("button", { name: /connect integrations/i }),
    ).toBeEnabled();
  });

  it("keeps the manual fallback: renders the form and a get-key link when expanded", () => {
    render(wrap(<ComposioOnboarding onConnected={() => {}} />));
    expandManualFallback();
    expect(screen.getByLabelText(/api key/i)).toBeInTheDocument();
    const link = screen.getByRole("link", { name: /get an api key/i });
    // Regression: the old developer portal (app.composio.dev) is gone; the
    // current dashboard lives at dashboard.composio.dev.
    expect(link).toHaveAttribute("href", "https://dashboard.composio.dev");
  });

  it("disables connect until a key is entered", () => {
    render(wrap(<ComposioOnboarding onConnected={() => {}} />));
    expandManualFallback();
    expect(screen.getByRole("button", { name: /save key/i })).toBeDisabled();
    fireEvent.change(screen.getByLabelText(/api key/i), {
      target: { value: "ak_abc123" },
    });
    expect(screen.getByRole("button", { name: /save key/i })).toBeEnabled();
  });

  it("saves the pasted key via updateConfig and notifies on success", async () => {
    updateConfig.mockResolvedValue({ status: "ok" });
    const onConnected = vi.fn();
    render(wrap(<ComposioOnboarding onConnected={onConnected} />));
    expandManualFallback();
    fireEvent.change(screen.getByLabelText(/api key/i), {
      target: { value: "  ak_abc123  " },
    });
    fireEvent.click(screen.getByRole("button", { name: /save key/i }));
    await waitFor(() =>
      expect(updateConfig).toHaveBeenCalledWith({
        composio_api_key: "ak_abc123",
      }),
    );
    await waitFor(() => expect(onConnected).toHaveBeenCalled());
  });

  it("toggles key visibility", () => {
    render(wrap(<ComposioOnboarding onConnected={() => {}} />));
    expandManualFallback();
    const input = screen.getByLabelText(/api key/i);
    expect(input).toHaveAttribute("type", "password");
    fireEvent.click(screen.getByRole("button", { name: "Show" }));
    expect(input).toHaveAttribute("type", "text");
  });
});
