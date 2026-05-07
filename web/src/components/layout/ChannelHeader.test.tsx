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

  it("closes the menu on Escape", () => {
    useAppStore.setState({ theme: "nex" });

    render(<ChannelHeader />);

    fireEvent.click(
      screen.getByRole("button", {
        name: /Theme: Nex Light\. Open theme switcher\./,
      }),
    );
    expect(screen.getByRole("menu")).toBeInTheDocument();

    fireEvent.keyDown(document, { key: "Escape" });
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
  });

  it("closes the menu on outside pointerdown", () => {
    useAppStore.setState({ theme: "nex" });

    render(<ChannelHeader />);

    fireEvent.click(
      screen.getByRole("button", {
        name: /Theme: Nex Light\. Open theme switcher\./,
      }),
    );
    expect(screen.getByRole("menu")).toBeInTheDocument();

    fireEvent.pointerDown(document.body);
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
  });

  it("navigates menu items with arrow keys", () => {
    useAppStore.setState({ theme: "nex" });

    render(<ChannelHeader />);

    fireEvent.click(
      screen.getByRole("button", {
        name: /Theme: Nex Light\. Open theme switcher\./,
      }),
    );

    const menu = screen.getByRole("menu");
    const items = screen.getAllByRole("menuitemradio");

    // Menu opens with focus on the active item (Nex Light, index 0).
    expect(document.activeElement).toBe(items[0]);

    fireEvent.keyDown(menu, { key: "ArrowDown" });
    expect(document.activeElement).toBe(items[1]);

    fireEvent.keyDown(menu, { key: "End" });
    expect(document.activeElement).toBe(items[items.length - 1]);

    fireEvent.keyDown(menu, { key: "ArrowDown" });
    expect(document.activeElement).toBe(items[0]);

    fireEvent.keyDown(menu, { key: "ArrowUp" });
    expect(document.activeElement).toBe(items[items.length - 1]);

    fireEvent.keyDown(menu, { key: "Home" });
    expect(document.activeElement).toBe(items[0]);
  });
});
