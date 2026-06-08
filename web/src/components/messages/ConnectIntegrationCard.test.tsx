import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { AgentRequest } from "../../api/client";

const startConnect = vi.fn();
const getStatus = vi.fn();
vi.mock("../../api/integrations", () => ({
  startIntegrationConnection: (...args: unknown[]) => startConnect(...args),
  getIntegrationConnectStatus: (...args: unknown[]) => getStatus(...args),
}));

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
    vi.stubGlobal("open", vi.fn());
  });

  it("renders the integration identity and the connect/skip actions", () => {
    render(wrap(<ConnectIntegrationCard request={makeConnectRequest()} submitting={false} onSkip={() => {}} onDismiss={() => {}} />));
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

  it("starts the Composio OAuth flow and opens the auth url", async () => {
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

    render(wrap(<ConnectIntegrationCard request={makeConnectRequest()} submitting={false} onSkip={() => {}} onDismiss={() => {}} />));
    fireEvent.click(screen.getByRole("button", { name: /connect gmail/i }));

    await waitFor(() => expect(startConnect).toHaveBeenCalledWith("composio", "gmail"));
    await waitFor(() =>
      expect(window.open).toHaveBeenCalledWith(
        "https://auth.composio.dev/x",
        "_blank",
        "noopener,noreferrer",
      ),
    );
    // The waiting state appears while the popup is open.
    await waitFor(() =>
      expect(screen.getByText(/waiting for you to finish/i)).toBeInTheDocument(),
    );
  });

  it("answers skip without touching the OAuth flow", () => {
    const onSkip = vi.fn();
    render(wrap(<ConnectIntegrationCard request={makeConnectRequest()} submitting={false} onSkip={onSkip} onDismiss={() => {}} />));
    fireEvent.click(screen.getByRole("button", { name: "Skip" }));
    expect(onSkip).toHaveBeenCalledTimes(1);
    expect(startConnect).not.toHaveBeenCalled();
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
