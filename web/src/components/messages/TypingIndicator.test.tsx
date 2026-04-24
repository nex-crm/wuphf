import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { useChannelMembers, useOfficeMembers } from "../../hooks/useMembers";
import { useAppStore } from "../../stores/app";
import { TypingIndicator } from "./TypingIndicator";

vi.mock("../../hooks/useMembers", () => ({
  useOfficeMembers: vi.fn(),
  useChannelMembers: vi.fn(),
}));

const mockUseOfficeMembers = vi.mocked(useOfficeMembers);
const mockUseChannelMembers = vi.mocked(useChannelMembers);

describe("<TypingIndicator>", () => {
  beforeEach(() => {
    useAppStore.setState({
      currentChannel: "general",
      currentApp: null,
      channelMeta: {},
    });
    mockUseOfficeMembers.mockReturnValue({ data: [] } as unknown as ReturnType<
      typeof useOfficeMembers
    >);
    mockUseChannelMembers.mockReturnValue({ data: [] } as unknown as ReturnType<
      typeof useChannelMembers
    >);
  });

  it("shows the active DM agent as typing", () => {
    useAppStore.getState().enterDM("ceo", "ceo__human");
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
    useAppStore.getState().setCurrentChannel("product");
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
