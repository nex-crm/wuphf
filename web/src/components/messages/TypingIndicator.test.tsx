import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { useChannelMembers, useOfficeMembers } from "../../hooks/useMembers";
import { useCurrentRoute } from "../../routes/useCurrentRoute";
import { TypingIndicator } from "./TypingIndicator";

vi.mock("../../hooks/useMembers", () => ({
  useOfficeMembers: vi.fn(),
  useChannelMembers: vi.fn(),
}));

vi.mock("../../routes/useCurrentRoute", () => ({
  useCurrentRoute: vi.fn(),
}));

const mockUseOfficeMembers = vi.mocked(useOfficeMembers);
const mockUseChannelMembers = vi.mocked(useChannelMembers);
const mockUseCurrentRoute = vi.mocked(useCurrentRoute);

describe("<TypingIndicator>", () => {
  beforeEach(() => {
    mockUseOfficeMembers.mockReturnValue({ data: [] } as unknown as ReturnType<
      typeof useOfficeMembers
    >);
    mockUseChannelMembers.mockReturnValue({ data: [] } as unknown as ReturnType<
      typeof useChannelMembers
    >);
    mockUseCurrentRoute.mockReturnValue({
      kind: "channel",
      channelSlug: "general",
    });
  });

  it("shows the active DM agent as typing", () => {
    mockUseCurrentRoute.mockReturnValue({
      kind: "dm",
      agentSlug: "ceo",
      channelSlug: "ceo__human",
    });
    mockUseOfficeMembers.mockReturnValue({
      data: [
        { slug: "ceo", name: "CEO", status: "active" },
        { slug: "pm", name: "PM", status: "active" },
      ],
    } as unknown as ReturnType<typeof useOfficeMembers>);
    mockUseChannelMembers.mockReturnValue({
      data: [{ slug: "ceo", name: "CEO" }],
    } as unknown as ReturnType<typeof useChannelMembers>);

    render(<TypingIndicator />);

    expect(screen.getByText("CEO is typing...")).toBeInTheDocument();
    expect(screen.queryByText(/PM/)).not.toBeInTheDocument();
  });

  it("limits public channel typing to channel members", () => {
    mockUseCurrentRoute.mockReturnValue({
      kind: "channel",
      channelSlug: "product",
    });
    mockUseOfficeMembers.mockReturnValue({
      data: [
        { slug: "ceo", name: "CEO", status: "active" },
        { slug: "pm", name: "PM", status: "active" },
      ],
    } as unknown as ReturnType<typeof useOfficeMembers>);
    mockUseChannelMembers.mockReturnValue({
      data: [{ slug: "pm", name: "PM" }],
    } as unknown as ReturnType<typeof useChannelMembers>);

    render(<TypingIndicator />);

    expect(screen.getByText("PM is typing...")).toBeInTheDocument();
    expect(screen.queryByText(/CEO/)).not.toBeInTheDocument();
  });
});
