import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { OfficeMember } from "../../api/client";
import { useAppStore } from "../../stores/app";
import { ChannelParticipants } from "./ChannelParticipants";

const mocks = vi.hoisted(() => ({
  invalidateQueries: vi.fn(),
  post: vi.fn(),
  refetchQueries: vi.fn(),
  showNotice: vi.fn(),
  showUndoToast: vi.fn(),
  useChannelMembers: vi.fn(),
  useOfficeMembers: vi.fn(),
}));

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({
    invalidateQueries: mocks.invalidateQueries,
    refetchQueries: mocks.refetchQueries,
  }),
}));

vi.mock("../../api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../../api/client")>();
  return {
    ...actual,
    post: mocks.post,
  };
});

vi.mock("../../hooks/useMembers", () => ({
  useChannelMembers: mocks.useChannelMembers,
  useOfficeMembers: mocks.useOfficeMembers,
}));

vi.mock("../ui/Toast", () => ({
  showNotice: mocks.showNotice,
  showUndoToast: mocks.showUndoToast,
}));

vi.mock("../ui/PixelAvatar", () => ({
  PixelAvatar: ({ slug }: { slug: string }) => (
    <span data-testid={`avatar-${slug}`} />
  ),
}));

function member(
  partial: Partial<OfficeMember> & { slug: string },
): OfficeMember {
  return {
    name: partial.name ?? "",
    role: partial.role ?? "",
    ...partial,
  };
}

describe("<ChannelParticipants>", () => {
  beforeEach(() => {
    mocks.invalidateQueries.mockReset();
    mocks.post.mockReset().mockResolvedValue({});
    mocks.refetchQueries.mockReset();
    mocks.showNotice.mockReset();
    mocks.showUndoToast.mockReset();
    mocks.useChannelMembers.mockReset();
    mocks.useOfficeMembers.mockReset();
    mocks.useOfficeMembers.mockReturnValue({ data: [] });
    useAppStore.setState({ activeAgentSlug: null });
  });

  it("lists channel agents and filters out human seats", () => {
    mocks.useChannelMembers.mockReturnValue({
      data: [
        member({ slug: "human", name: "Human" }),
        member({ slug: "ceo", name: "CEO", role: "Lead" }),
        member({
          slug: "ops",
          name: "Ops",
          role: "Operations",
          disabled: true,
        }),
      ],
      isLoading: false,
    });
    mocks.useOfficeMembers.mockReturnValue({
      data: [
        member({ slug: "ceo", name: "CEO", role: "Lead", built_in: true }),
        member({ slug: "ops", name: "Ops", role: "Operations" }),
      ],
    });

    render(<ChannelParticipants channelSlug="general" />);

    expect(mocks.useChannelMembers).toHaveBeenCalledWith("general");
    expect(screen.getByText("2 agents")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Open agent panel for CEO" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Open agent panel for Ops" }),
    ).toBeInTheDocument();
    expect(screen.queryByText("Human")).not.toBeInTheDocument();
    expect(screen.getByText("Disabled in this channel")).toBeInTheDocument();
  });

  it("opens the agent panel for a participant", () => {
    mocks.useChannelMembers.mockReturnValue({
      data: [member({ slug: "planner", name: "Planner" })],
      isLoading: false,
    });

    render(<ChannelParticipants channelSlug="strategy" />);

    fireEvent.click(
      screen.getByRole("button", { name: "Open agent panel for Planner" }),
    );

    expect(useAppStore.getState().activeAgentSlug).toBe("planner");
  });

  it("adds an available office member to the channel", async () => {
    mocks.useChannelMembers.mockReturnValue({
      data: [member({ slug: "ceo", name: "CEO" })],
      isLoading: false,
    });
    mocks.useOfficeMembers.mockReturnValue({
      data: [
        member({ slug: "ceo", name: "CEO", built_in: true }),
        member({ slug: "planner", name: "Planner", role: "Planning agent" }),
      ],
    });

    render(<ChannelParticipants channelSlug="strategy" />);

    fireEvent.click(screen.getByTitle("Add participant"));
    fireEvent.click(
      screen.getByRole("button", { name: "Add Planner to #strategy" }),
    );

    await waitFor(() => {
      expect(mocks.post).toHaveBeenCalledWith("/channel-members", {
        channel: "strategy",
        slug: "planner",
        action: "add",
      });
    });
    expect(mocks.refetchQueries).toHaveBeenCalledWith({
      queryKey: ["channel-members", "strategy"],
    });
  });

  it("enables a disabled channel participant", async () => {
    mocks.useChannelMembers.mockReturnValue({
      data: [member({ slug: "ops", name: "Ops", disabled: true })],
      isLoading: false,
    });
    mocks.useOfficeMembers.mockReturnValue({
      data: [member({ slug: "ops", name: "Ops", role: "Operations" })],
    });

    render(<ChannelParticipants channelSlug="general" />);

    fireEvent.click(screen.getByRole("button", { name: "Enable" }));

    await waitFor(() => {
      expect(mocks.post).toHaveBeenCalledWith("/channel-members", {
        channel: "general",
        slug: "ops",
        action: "enable",
      });
    });
  });

  it("removes a channel participant without deleting the office member", async () => {
    mocks.useChannelMembers.mockReturnValue({
      data: [member({ slug: "ops", name: "Ops" })],
      isLoading: false,
    });
    mocks.useOfficeMembers.mockReturnValue({
      data: [member({ slug: "ops", name: "Ops", role: "Operations" })],
    });

    render(<ChannelParticipants channelSlug="general" />);

    fireEvent.click(
      screen.getByRole("button", { name: "Remove Ops from channel" }),
    );

    await waitFor(() => {
      expect(mocks.post).toHaveBeenCalledWith("/channel-members", {
        channel: "general",
        slug: "ops",
        action: "remove",
      });
    });
    expect(mocks.post).not.toHaveBeenCalledWith(
      "/office-members",
      expect.anything(),
    );
    expect(mocks.showUndoToast).toHaveBeenCalledWith(
      "Ops removed from #general",
      expect.any(Function),
      5000,
    );
  });

  it("undoes a removed participant within the undo toast window", async () => {
    mocks.useChannelMembers.mockReturnValue({
      data: [member({ slug: "ops", name: "Ops", disabled: true })],
      isLoading: false,
    });
    mocks.useOfficeMembers.mockReturnValue({
      data: [member({ slug: "ops", name: "Ops", role: "Operations" })],
    });

    render(<ChannelParticipants channelSlug="general" />);

    fireEvent.click(
      screen.getByRole("button", { name: "Remove Ops from channel" }),
    );

    await waitFor(() => expect(mocks.showUndoToast).toHaveBeenCalled());
    const undo = mocks.showUndoToast.mock.calls[0]?.[1] as () => void;
    undo();

    await waitFor(() => {
      expect(mocks.post).toHaveBeenCalledWith("/channel-members", {
        channel: "general",
        slug: "ops",
        action: "add",
      });
      expect(mocks.post).toHaveBeenCalledWith("/channel-members", {
        channel: "general",
        slug: "ops",
        action: "disable",
      });
    });
    expect(mocks.showNotice).toHaveBeenCalledWith(
      "Ops restored to #general",
      "success",
    );
  });

  it("does not expose remove for the lead agent", () => {
    mocks.useChannelMembers.mockReturnValue({
      data: [member({ slug: "ceo", name: "CEO" })],
      isLoading: false,
    });
    mocks.useOfficeMembers.mockReturnValue({
      data: [member({ slug: "ceo", name: "CEO", built_in: true })],
    });

    render(<ChannelParticipants channelSlug="general" />);

    expect(
      screen.getByRole("button", { name: "Remove CEO from channel" }),
    ).toBeDisabled();
  });

  it("renders an empty state when the channel has no agents", () => {
    mocks.useChannelMembers.mockReturnValue({
      data: [member({ slug: "human", name: "Human" })],
      isLoading: false,
    });

    render(<ChannelParticipants channelSlug="solo" />);

    expect(screen.getByText("0 agents")).toBeInTheDocument();
    expect(screen.getByText("No agents in this channel")).toBeInTheDocument();
  });
});
