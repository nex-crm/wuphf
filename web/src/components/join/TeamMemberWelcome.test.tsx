import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { getHumanMe } from "../../api/platform";
import { TeamMemberWelcome } from "./TeamMemberWelcome";

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

describe("TeamMemberWelcome", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    window.localStorage.clear();
  });

  afterEach(() => {
    window.localStorage.clear();
  });

  it("renders welcome card for a team-member session", async () => {
    getHumanMeMock.mockResolvedValue({
      human: {
        role: "member",
        display_name: "Maya",
        invite_id: "invite-1",
      },
    });
    render(wrap(<TeamMemberWelcome />));

    expect(
      await screen.findByLabelText("Team member session welcome"),
    ).toBeInTheDocument();
    expect(screen.getByText("Maya")).toBeInTheDocument();
    expect(
      screen.getByText(/scoped team-member browser session/i),
    ).toBeInTheDocument();
  });

  it("does not render for a host session", async () => {
    getHumanMeMock.mockResolvedValue({
      human: {
        role: "host",
        display_name: "Sam",
      },
    });
    render(wrap(<TeamMemberWelcome />));

    // Wait for the query to resolve, then assert nothing rendered.
    await waitFor(() => {
      expect(getHumanMeMock).toHaveBeenCalled();
    });
    expect(
      screen.queryByLabelText("Team member session welcome"),
    ).not.toBeInTheDocument();
  });

  it("does not render while role is unknown (loading)", () => {
    getHumanMeMock.mockReturnValue(new Promise(() => {})); // never resolves
    render(wrap(<TeamMemberWelcome />));

    expect(
      screen.queryByLabelText("Team member session welcome"),
    ).not.toBeInTheDocument();
  });

  it("dismisses and persists the dismissal across remounts", async () => {
    getHumanMeMock.mockResolvedValue({
      human: {
        role: "member",
        display_name: "Maya",
        invite_id: "invite-1",
      },
    });
    const user = userEvent.setup();
    const { unmount } = render(wrap(<TeamMemberWelcome />));

    await user.click(
      await screen.findByRole("button", { name: /dismiss welcome message/i }),
    );
    expect(
      screen.queryByLabelText("Team member session welcome"),
    ).not.toBeInTheDocument();

    unmount();
    render(wrap(<TeamMemberWelcome />));

    // Wait for the new query to resolve before asserting the welcome stays
    // hidden — otherwise we are asserting on the pre-data state.
    await waitFor(() => {
      expect(getHumanMeMock).toHaveBeenCalledTimes(2);
    });
    expect(
      screen.queryByLabelText("Team member session welcome"),
    ).not.toBeInTheDocument();
  });

  it("falls back to a default name when display_name is empty", async () => {
    getHumanMeMock.mockResolvedValue({
      human: {
        role: "member",
        display_name: "   ",
        invite_id: "invite-2",
      },
    });
    render(wrap(<TeamMemberWelcome />));

    expect(await screen.findByText("team member")).toBeInTheDocument();
  });
});
