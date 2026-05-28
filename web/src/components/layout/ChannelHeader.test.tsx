import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ChannelHeader } from "./ChannelHeader";

vi.mock("../../hooks/useChannels", () => ({
  useChannels: () => ({
    data: [{ slug: "general", description: "Office wide" }],
  }),
}));

vi.mock("../../routes/useCurrentRoute", () => ({
  useCurrentRoute: () => ({ kind: "channel", channelSlug: "general" }),
}));

vi.mock("../../hooks/useObjectBreadcrumb", () => ({
  deriveBreadcrumbs: () => [],
}));

describe("<ChannelHeader>", () => {
  it("renders the current channel title + description", () => {
    render(<ChannelHeader />);

    expect(screen.getByText("# general")).toBeInTheDocument();
    expect(screen.getByText("Office wide")).toBeInTheDocument();
  });

  it("no longer exposes the theme switcher or search button (moved to settings / global search)", () => {
    render(<ChannelHeader />);

    // The theme switcher trigger and the per-header search affordance are
    // both gone — settings owns the theme, and search has a global Cmd+K.
    expect(
      screen.queryByRole("button", { name: /Open theme switcher/i }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /Search/i }),
    ).not.toBeInTheDocument();
  });
});
