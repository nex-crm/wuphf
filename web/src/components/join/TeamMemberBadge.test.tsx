import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { getHumanMe } from "../../api/platform";
import { TeamMemberBadge } from "./TeamMemberBadge";

vi.mock("../../api/platform", () => ({
  getHumanMe: vi.fn(),
}));

const getHumanMeMock = vi.mocked(getHumanMe);

function wrap(ui: ReactNode) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
  return <QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>;
}

describe("TeamMemberBadge", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders for a team-member session", async () => {
    getHumanMeMock.mockResolvedValue({
      human: { role: "member", display_name: "Maya" },
    });
    render(wrap(<TeamMemberBadge />));

    expect(
      await screen.findByLabelText("Team-member session"),
    ).toBeInTheDocument();
  });

  it("does not render for a host session", async () => {
    getHumanMeMock.mockResolvedValue({
      human: { role: "host", display_name: "Sam" },
    });
    render(wrap(<TeamMemberBadge />));

    await waitFor(() => {
      expect(getHumanMeMock).toHaveBeenCalled();
    });
    expect(
      screen.queryByLabelText("Team-member session"),
    ).not.toBeInTheDocument();
  });

  it("does not render while role is unknown (loading)", () => {
    getHumanMeMock.mockReturnValue(new Promise(() => {}));
    render(wrap(<TeamMemberBadge />));
    expect(
      screen.queryByLabelText("Team-member session"),
    ).not.toBeInTheDocument();
  });
});
