import { fireEvent, render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { ReferencedIntegration } from "../builder/integrationStatus";
import { ConnectionsCard } from "./ConnectionsCard";

// ConnectionsCard reuses WUPHF's built connect flow (ConnectIntegrationCard);
// that card owns the Composio sign-in + OAuth and has its own tests. Here we only
// assert ConnectionsCard raises it for the right integrations and falls back to
// the browser path otherwise. Mock the card so we don't need React Query/network.
vi.mock("../../components/messages/ConnectIntegrationCard", () => ({
  ConnectIntegrationCard: ({
    request,
    onSkip,
  }: {
    request: { platform?: string };
    onSkip: () => void;
  }) => (
    <button type="button" data-testid="connect-card" onClick={onSkip}>
      connect:{request.platform}
    </button>
  ),
}));

function connectable(name: string): ReferencedIntegration {
  return {
    name,
    readiness: "connectable",
    provider: "composio",
    platform: name.toLowerCase(),
  };
}

describe("ConnectionsCard", () => {
  it("renders nothing when no integration needs attention", () => {
    const { container } = render(
      <ConnectionsCard
        integrations={[{ name: "Slack", readiness: "connected" }]}
      />,
    );
    expect(container.firstChild).toBeNull();
  });

  it("raises the built connect card for a connectable integration", () => {
    const { getByTestId } = render(
      <ConnectionsCard integrations={[connectable("HubSpot")]} />,
    );
    expect(getByTestId("connect-card").textContent).toContain("hubspot");
  });

  it("offers the browser fallback for an unavailable integration", () => {
    const { getByText, queryByTestId } = render(
      <ConnectionsCard
        integrations={[{ name: "Acme Internal", readiness: "unavailable" }]}
      />,
    );
    expect(getByText(/set this up in your browser/i)).toBeTruthy();
    expect(queryByTestId("connect-card")).toBeNull();
  });

  it("drops to the browser fallback when the connect card is skipped", () => {
    const { getByTestId, getByText } = render(
      <ConnectionsCard integrations={[connectable("HubSpot")]} />,
    );
    fireEvent.click(getByTestId("connect-card")); // mock fires onSkip
    expect(getByText(/set this up in your browser/i)).toBeTruthy();
  });
});
