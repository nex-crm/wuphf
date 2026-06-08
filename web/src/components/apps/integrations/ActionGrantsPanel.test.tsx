import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { ActionGrant } from "../../../api/client";

const getGrants = vi.fn();
const revoke = vi.fn();
vi.mock("../../../api/client", async () => {
  const actual =
    await vi.importActual<typeof import("../../../api/client")>(
      "../../../api/client",
    );
  return {
    ...actual,
    getActionGrants: () => getGrants(),
    revokeActionGrant: (id: string) => revoke(id),
  };
});

import { ActionGrantsPanel } from "./ActionGrantsPanel";

function wrap(ui: ReactNode) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={qc}>{ui}</QueryClientProvider>;
}

function grant(overrides: Partial<ActionGrant> = {}): ActionGrant {
  return {
    id: "grant-1",
    agent_slug: "growthops",
    platform: "gmail",
    action_scope: "GMAIL_SEND_EMAIL",
    granted_by: "you",
    granted_at: "2026-06-08T12:00:00Z",
    ...overrides,
  };
}

describe("<ActionGrantsPanel>", () => {
  it("renders nothing when there are no active grants", async () => {
    getGrants.mockResolvedValue({ grants: [] });
    const { container } = render(wrap(<ActionGrantsPanel />));
    await waitFor(() => expect(getGrants).toHaveBeenCalled());
    expect(container).toBeEmptyDOMElement();
  });

  it("lists each active grant with its scope, agent, and platform", async () => {
    getGrants.mockResolvedValue({
      grants: [
        grant(),
        grant({
          id: "grant-2",
          agent_slug: "support",
          platform: "slack",
          action_scope: "SLACK_SEND_MESSAGE",
        }),
      ],
    });
    render(wrap(<ActionGrantsPanel />));
    expect(await screen.findByText("GMAIL_SEND_EMAIL")).toBeInTheDocument();
    expect(screen.getByText("SLACK_SEND_MESSAGE")).toBeInTheDocument();
    expect(screen.getByText("@growthops")).toBeInTheDocument();
    expect(screen.getByText("@support")).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "Revoke" })).toHaveLength(2);
  });

  it("revokes a grant by id", async () => {
    getGrants.mockResolvedValue({ grants: [grant()] });
    revoke.mockResolvedValue({ grant: grant({ revoked_at: "2026-06-09T00:00:00Z" }) });
    render(wrap(<ActionGrantsPanel />));
    fireEvent.click(await screen.findByRole("button", { name: "Revoke" }));
    await waitFor(() => expect(revoke).toHaveBeenCalledWith("grant-1"));
  });
});
