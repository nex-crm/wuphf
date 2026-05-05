import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { useAppStore } from "../../stores/app";
import { ChannelHeader } from "./ChannelHeader";

vi.mock("../../hooks/useChannels", () => ({
  useChannels: () => ({ data: [] }),
}));

vi.mock("../../routes/useCurrentRoute", () => ({
  useCurrentRoute: () => ({ kind: "channel", channelSlug: "general" }),
}));

afterEach(() => {
  useAppStore.setState({ theme: "nex" });
  document.documentElement.setAttribute("data-theme", "nex");
});

describe("<ChannelHeader>", () => {
  it("labels the theme toggle with the next theme action", () => {
    useAppStore.setState({ theme: "nex-dark" });

    render(<ChannelHeader />);

    const button = screen.getByRole("button", {
      name: "Switch theme to Noir Gold",
    });
    expect(button).toHaveAttribute("title", "Switch theme to Noir Gold");

    fireEvent.click(button);

    expect(useAppStore.getState().theme).toBe("noir-gold");
  });
});
