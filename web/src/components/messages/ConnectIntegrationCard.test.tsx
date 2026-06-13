import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { AgentRequest } from "../../api/client";

const startConnect = vi.fn();
const getStatus = vi.fn();
const startSignin = vi.fn();
const getSigninStatus = vi.fn();
vi.mock("../../api/integrations", () => ({
  startIntegrationConnection: (...args: unknown[]) => startConnect(...args),
  getIntegrationConnectStatus: (...args: unknown[]) => getStatus(...args),
  startComposioSignin: (...args: unknown[]) => startSignin(...args),
  getComposioSigninStatus: (...args: unknown[]) => getSigninStatus(...args),
}));

// getConfig drives the Composio sign-in gate (config.composio_key_set). Mock it
// so tests don't hit the network and can flip the signed-in state.
const getConfig = vi.fn();
vi.mock("../../api/client", async () => {
  const actual =
    await vi.importActual<typeof import("../../api/client")>(
      "../../api/client",
    );
  return { ...actual, getConfig: (...args: unknown[]) => getConfig(...args) };
});

import { ConnectIntegrationCard } from "./ConnectIntegrationCard";

function wrap(ui: ReactNode) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={qc}>{ui}</QueryClientProvider>;
}

function makeConnectRequest(
  overrides: Partial<AgentRequest> = {},
): AgentRequest {
  return {
    id: "request-9",
    from: "growthops",
    kind: "connect",
    question: "Connect Gmail so the team can run this action.",
    title: "Connect Gmail",
    channel: "general",
    platform: "gmail",
    options: [
      { id: "connect", label: "Connect" },
      { id: "skip", label: "Skip" },
    ],
    recommended_id: "connect",
    ...overrides,
  };
}

describe("<ConnectIntegrationCard>", () => {
  beforeEach(() => {
    startConnect.mockReset();
    getStatus.mockReset();
    startSignin.mockReset();
    getSigninStatus.mockReset();
    getConfig.mockReset();
    // Default: Composio account is already signed in, so Connect goes straight
    // to the integration OAuth. The sign-in-gate test overrides this.
    getConfig.mockResolvedValue({ composio_key_set: true });
    getSigninStatus.mockResolvedValue({ status: "idle" });
    vi.stubGlobal("open", vi.fn());
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("renders the integration identity and the connect/skip actions", () => {
    render(
      wrap(
        <ConnectIntegrationCard
          request={makeConnectRequest()}
          submitting={false}
          onSkip={() => {}}
          onDismiss={() => {}}
        />,
      ),
    );
    expect(
      screen.getByRole("heading", { name: "Connect Gmail" }),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/needs this to run an external action/i),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /connect gmail/i }),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Skip" })).toBeInTheDocument();
  });

  it("starts the Composio OAuth flow and opens the auth url (signed in)", async () => {
    startConnect.mockResolvedValue({
      provider: "composio",
      platform: "gmail",
      status: "pending",
      connect_id: "cx_1",
      auth_url: "https://auth.composio.dev/x",
    });
    getStatus.mockResolvedValue({
      provider: "composio",
      platform: "gmail",
      status: "pending",
    });

    render(
      wrap(
        <ConnectIntegrationCard
          request={makeConnectRequest()}
          submitting={false}
          onSkip={() => {}}
          onDismiss={() => {}}
        />,
      ),
    );
    // Wait for the signed-in config to settle (button drops the "Sign in &"
    // prefix) so Connect takes the integration-connect path, not sign-in.
    const connectBtn = await screen.findByRole("button", {
      name: /^connect gmail$/i,
    });
    fireEvent.click(connectBtn);

    await waitFor(() =>
      expect(startConnect).toHaveBeenCalledWith("composio", "gmail"),
    );
    await waitFor(() =>
      expect(window.open).toHaveBeenCalledWith(
        "https://auth.composio.dev/x",
        "_blank",
        "noopener,noreferrer",
      ),
    );
    expect(startSignin).not.toHaveBeenCalled();
    await waitFor(() =>
      expect(
        screen.getByText(/waiting for you to finish/i),
      ).toBeInTheDocument(),
    );
  });

  it("runs Composio sign-in FIRST when the office isn't signed in", async () => {
    // Composio not signed in → the first Connect click must kick off the
    // "Sign in with Composio" flow, NOT the integration connection.
    getConfig.mockResolvedValue({ composio_key_set: false });
    startSignin.mockResolvedValue({
      status: "awaiting_login",
      auth_url: "https://app.composio.dev/login?cliKey=abc",
    });
    // The status poll keeps reporting awaiting_login until the user authorizes.
    getSigninStatus.mockResolvedValue({ status: "awaiting_login" });

    render(
      wrap(
        <ConnectIntegrationCard
          request={makeConnectRequest()}
          submitting={false}
          onSkip={() => {}}
          onDismiss={() => {}}
        />,
      ),
    );

    // Button reflects the not-signed-in state once config settles.
    const signinBtn = await screen.findByRole("button", {
      name: /sign in & connect gmail/i,
    });
    fireEvent.click(signinBtn);

    await waitFor(() => expect(startSignin).toHaveBeenCalledTimes(1));
    await waitFor(() =>
      expect(window.open).toHaveBeenCalledWith(
        "https://app.composio.dev/login?cliKey=abc",
        "_blank",
        "noopener,noreferrer",
      ),
    );
    // The integration connection must NOT start until Composio sign-in is done.
    expect(startConnect).not.toHaveBeenCalled();
    await waitFor(() =>
      expect(
        screen.getByText(/finish signing in to composio/i),
      ).toBeInTheDocument(),
    );
  });

  it("answers skip without touching the OAuth flow", () => {
    const onSkip = vi.fn();
    render(
      wrap(
        <ConnectIntegrationCard
          request={makeConnectRequest()}
          submitting={false}
          onSkip={onSkip}
          onDismiss={() => {}}
        />,
      ),
    );
    fireEvent.click(screen.getByRole("button", { name: "Skip" }));
    expect(onSkip).toHaveBeenCalledTimes(1);
    expect(startConnect).not.toHaveBeenCalled();
    expect(startSignin).not.toHaveBeenCalled();
  });

  it("disables connect when no platform is known", () => {
    render(
      wrap(
        <ConnectIntegrationCard
          request={makeConnectRequest({ platform: "", title: "" })}
          submitting={false}
          onSkip={() => {}}
          onDismiss={() => {}}
        />,
      ),
    );
    expect(
      screen.getByRole("button", { name: /connect the integration/i }),
    ).toBeDisabled();
  });
});
