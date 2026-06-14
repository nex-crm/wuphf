import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { IntegrationsApp } from "./IntegrationsApp";

// Mutable per-test config so the same mocked module can exercise both the
// Composio-configured and unconfigured states.
const mockState = vi.hoisted(() => ({
  config: {} as Record<string, unknown>,
}));

vi.mock("../../api/client", () => ({
  getConfig: vi.fn(async () => mockState.config),
  updateConfig: vi.fn(async () => ({ status: "ok" })),
  getLocalProvidersStatus: vi.fn(async () => []),
  getActionGrants: vi.fn(async () => ({ grants: [] })),
  revokeActionGrant: vi.fn(async () => ({})),
}));

vi.mock("../../api/integrations", () => ({
  listIntegrations: vi.fn(async () => ({
    providers: [
      {
        provider: "composio",
        label: "Composio",
        configured: true,
        supports_connect: true,
        supports_disconnect: true,
        detail: "Configured",
      },
    ],
    items: [
      {
        provider: "composio",
        platform: "gmail",
        name: "Gmail",
        description: "Read and send Gmail messages",
        state: "connected",
        connection_key: "ca_123",
        can_connect: true,
        can_disconnect: true,
      },
    ],
  })),
  getIntegrationAudit: vi.fn(async () => []),
  getIntegrationConnectStatus: vi.fn(async () => ({
    provider: "composio",
    platform: "gmail",
    status: "connected",
  })),
  startIntegrationConnection: vi.fn(),
  disconnectIntegration: vi.fn(),
  startComposioSignin: vi.fn(async () => ({ status: "idle" })),
  getComposioSigninStatus: vi.fn(async () => ({ status: "idle" })),
}));

function wrap(ui: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return <QueryClientProvider client={client}>{ui}</QueryClientProvider>;
}

describe("IntegrationsApp", () => {
  beforeEach(() => {
    // Mirrors the broker's real config shape: GatewayKinds() reports the
    // gateway-only provider kinds compiled into the build.
    mockState.config = {
      gateway_kinds: ["hermes-agent", "openclaw-http"],
      action_provider: "composio",
      composio_key_set: true,
    };
  });

  it("renders provider status and dynamic action toolkits", async () => {
    render(wrap(<IntegrationsApp />));

    expect(await screen.findByText("Action Accounts")).toBeInTheDocument();
    expect(screen.getByText("Composio")).toBeInTheDocument();
    expect(screen.getByText("Gmail")).toBeInTheDocument();
    expect(
      screen.getByText("Read and send Gmail messages"),
    ).toBeInTheDocument();
  });

  it("renders the transports section when Composio is configured", async () => {
    render(wrap(<IntegrationsApp />));

    expect(await screen.findByText("Telegram")).toBeInTheDocument();
    expect(screen.getByText("Hermes")).toBeInTheDocument();
    expect(screen.getByText("OpenClaw")).toBeInTheDocument();
  });

  it("keeps the transports visible when no Composio key is set", async () => {
    // Regression: the Composio onboarding gate must not swallow the transport
    // registry — Telegram/Hermes/OpenClaw do not depend on Composio.
    mockState.config = {
      gateway_kinds: ["hermes-agent", "openclaw-http"],
      action_provider: "composio",
      composio_key_set: false,
    };
    render(wrap(<IntegrationsApp />));

    // The onboarding hero renders…
    expect(
      await screen.findByRole("heading", { name: /connect composio/i }),
    ).toBeInTheDocument();
    // …and the transports stay alongside it.
    expect(await screen.findByText("Telegram")).toBeInTheDocument();
    expect(screen.getByText("Hermes")).toBeInTheDocument();
    expect(screen.getByText("OpenClaw")).toBeInTheDocument();
  });
});
