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
  it("opens the theme switcher and switches to Nex Dark", () => {
    useAppStore.setState({ theme: "nex" });

    render(<ChannelHeader />);

    const trigger = screen.getByRole("button", {
      name: /Theme: Nex Light\. Open theme switcher\./,
    });
    fireEvent.click(trigger);

    const dark = screen.getByRole("menuitemradio", { name: /Nex Dark/ });
    fireEvent.click(dark);

    expect(useAppStore.getState().theme).toBe("nex-dark");
    expect(document.documentElement.getAttribute("data-theme")).toBe(
      "nex-dark",
    );
  });

  it("marks the active theme as checked", () => {
    useAppStore.setState({ theme: "noir-gold" });

    render(<ChannelHeader />);

    fireEvent.click(
      screen.getByRole("button", {
        name: /Theme: Noir Gold\. Open theme switcher\./,
      }),
    );

    const noir = screen.getByRole("menuitemradio", { name: /Noir Gold/ });
    expect(noir).toHaveAttribute("aria-checked", "true");
  });
});
