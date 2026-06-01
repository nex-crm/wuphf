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

    // The bubble shows the author heading + an italic verb, and carries an
    // accessible "is typing" label for screen readers.
    expect(screen.getByText("CEO")).toBeInTheDocument();
    expect(screen.getByText("is typing")).toBeInTheDocument();
    expect(
      screen.getByRole("status", { name: /CEO is typing/ }),
    ).toBeInTheDocument();
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

    expect(screen.getByText("PM")).toBeInTheDocument();
    expect(
      screen.getByRole("status", { name: /PM is typing/ }),
    ).toBeInTheDocument();
    expect(screen.queryByText(/CEO/)).not.toBeInTheDocument();
  });

  it("surfaces the live progress detail for a single active agent", () => {
    mockUseCurrentRoute.mockReturnValue({
      kind: "dm",
      agentSlug: "pm",
      channelSlug: "pm__human",
    });
    mockUseOfficeMembers.mockReturnValue({
      data: [
        {
          slug: "pm",
          name: "PM",
          status: "active",
          liveActivity: "drafting figure",
        },
      ],
    } as unknown as ReturnType<typeof useOfficeMembers>);
    mockUseChannelMembers.mockReturnValue({
      data: [{ slug: "pm", name: "PM" }],
    } as unknown as ReturnType<typeof useChannelMembers>);

    render(<TypingIndicator />);

    expect(screen.getByText("PM is typing...")).toBeInTheDocument();
    expect(screen.getByText("drafting figure")).toBeInTheDocument();
  });

  it("falls back to lower-priority progress fields when liveActivity is absent", () => {
    mockUseCurrentRoute.mockReturnValue({
      kind: "dm",
      agentSlug: "pm",
      channelSlug: "pm__human",
    });
    mockUseOfficeMembers.mockReturnValue({
      data: [
        { slug: "pm", name: "PM", status: "active", activity: "scoping issue" },
      ],
    } as unknown as ReturnType<typeof useOfficeMembers>);
    mockUseChannelMembers.mockReturnValue({
      data: [{ slug: "pm", name: "PM" }],
    } as unknown as ReturnType<typeof useChannelMembers>);

    render(<TypingIndicator />);

    expect(screen.getByText("scoping issue")).toBeInTheDocument();
  });

  it("suppresses the detail when several agents are active to avoid implying shared progress", () => {
    mockUseCurrentRoute.mockReturnValue({
      kind: "channel",
      channelSlug: "general",
    });
    mockUseOfficeMembers.mockReturnValue({
      data: [
        {
          slug: "pm",
          name: "PM",
          status: "active",
          liveActivity: "drafting figure",
        },
        {
          slug: "eng",
          name: "Eng",
          status: "active",
          liveActivity: "writing code",
        },
      ],
    } as unknown as ReturnType<typeof useOfficeMembers>);
    mockUseChannelMembers.mockReturnValue({
      data: [
        { slug: "pm", name: "PM" },
        { slug: "eng", name: "Eng" },
      ],
    } as unknown as ReturnType<typeof useChannelMembers>);

    render(<TypingIndicator />);

    expect(screen.getByText("PM, Eng are typing...")).toBeInTheDocument();
    expect(screen.queryByText("drafting figure")).not.toBeInTheDocument();
    expect(screen.queryByText("writing code")).not.toBeInTheDocument();
  });
});
