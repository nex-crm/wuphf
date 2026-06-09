import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { IntegrationsApp } from "./IntegrationsApp";

vi.mock("../../api/client", () => ({
  getConfig: vi.fn(async () => ({
    gateway_kinds: ["openclaw", "hermes-agent"],
    action_provider: "composio",
    // Composio key present → the catalog renders (no key shows the
    // ComposioOnboarding gate instead, covered in ComposioOnboarding.test.tsx).
    composio_key_set: true,
  })),
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
}));

function wrap(ui: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return <QueryClientProvider client={client}>{ui}</QueryClientProvider>;
}

describe("IntegrationsApp", () => {
  it("renders provider status and dynamic action toolkits", async () => {
    render(wrap(<IntegrationsApp />));

    expect(await screen.findByText("Action Accounts")).toBeInTheDocument();
    expect(screen.getByText("Composio")).toBeInTheDocument();
    expect(screen.getByText("Gmail")).toBeInTheDocument();
    expect(
      screen.getByText("Read and send Gmail messages"),
    ).toBeInTheDocument();
  });
});
