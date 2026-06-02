import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { appUrl } from "../../api/wiki";
import { useAppStore } from "../../stores/app";
import WebsiteViewer from "./WebsiteViewer";

const APP_PATH = "team/site/dashboard";

function frame(): HTMLIFrameElement {
  const el = screen
    .getByTestId("wk-website-viewer")
    .querySelector("iframe.wk-website-frame");
  if (!(el instanceof HTMLIFrameElement)) {
    throw new Error("website frame iframe not found");
  }
  return el;
}

describe("<WebsiteViewer>", () => {
  beforeEach(() => {
    // A known sidebar baseline so the full-screen collapse/restore assertions
    // are deterministic regardless of prior tests.
    useAppStore.setState({ sidebarCollapsed: false });
  });

  afterEach(() => {
    useAppStore.setState({ sidebarCollapsed: false });
    vi.restoreAllMocks();
  });

  it("points the iframe src at appUrl(path)", () => {
    render(<WebsiteViewer path={APP_PATH} />);
    expect(frame()).toHaveAttribute("src", appUrl(APP_PATH));
  });

  it("sandboxes with allow-scripts but NOT allow-same-origin", () => {
    render(<WebsiteViewer path={APP_PATH} />);
    const sandbox = frame().getAttribute("sandbox") ?? "";
    const tokens = sandbox.split(/\s+/);
    expect(tokens).toContain("allow-scripts");
    // Defence in depth: an opaque origin keeps the embedded app off our origin.
    expect(tokens).not.toContain("allow-same-origin");
    expect(frame()).toHaveAttribute("referrerpolicy", "no-referrer");
  });

  it("never embeds the auth token in the iframe src", () => {
    render(<WebsiteViewer path={APP_PATH} />);
    expect(frame().getAttribute("src") ?? "").not.toMatch(/token=/i);
  });

  it("uses the folder leaf as the iframe title when no title prop is given", () => {
    render(<WebsiteViewer path={APP_PATH} />);
    expect(frame()).toHaveAttribute("title", "dashboard");
  });

  it("prefers an explicit title prop over the folder leaf", () => {
    render(<WebsiteViewer path={APP_PATH} title="Revenue Dashboard" />);
    expect(frame()).toHaveAttribute("title", "Revenue Dashboard");
  });

  it("offers an Open-in-new-tab link to appUrl", () => {
    render(<WebsiteViewer path={APP_PATH} />);
    const link = screen.getByRole("link", { name: /open in new tab/i });
    expect(link).toHaveAttribute("href", appUrl(APP_PATH));
    expect(link).toHaveAttribute("target", "_blank");
    expect(link).toHaveAttribute("rel", expect.stringContaining("noopener"));
  });

  it("full-screen toggle collapses the sidebar and restores it on exit", () => {
    render(<WebsiteViewer path={APP_PATH} />);
    expect(useAppStore.getState().sidebarCollapsed).toBe(false);

    const toggle = screen.getByRole("button", { name: /full screen/i });
    fireEvent.click(toggle);

    // Entering full screen collapses the sidebar and marks the viewer.
    expect(useAppStore.getState().sidebarCollapsed).toBe(true);
    expect(screen.getByTestId("wk-website-viewer")).toHaveAttribute(
      "data-fullscreen",
      "true",
    );

    // Exiting restores the prior (uncollapsed) sidebar state.
    fireEvent.click(screen.getByRole("button", { name: /exit full screen/i }));
    expect(useAppStore.getState().sidebarCollapsed).toBe(false);
    expect(screen.getByTestId("wk-website-viewer")).toHaveAttribute(
      "data-fullscreen",
      "false",
    );
  });

  it("restores an already-collapsed sidebar to collapsed on full-screen exit", () => {
    useAppStore.setState({ sidebarCollapsed: true });
    render(<WebsiteViewer path={APP_PATH} />);

    fireEvent.click(screen.getByRole("button", { name: /full screen/i }));
    // Already collapsed, so it stays collapsed in full screen.
    expect(useAppStore.getState().sidebarCollapsed).toBe(true);

    fireEvent.click(screen.getByRole("button", { name: /exit full screen/i }));
    // The user's prior collapsed state is preserved, not force-opened.
    expect(useAppStore.getState().sidebarCollapsed).toBe(true);
  });

  it("Escape exits full screen and restores the sidebar", () => {
    render(<WebsiteViewer path={APP_PATH} />);
    fireEvent.click(screen.getByRole("button", { name: /full screen/i }));
    expect(useAppStore.getState().sidebarCollapsed).toBe(true);

    fireEvent.keyDown(window, { key: "Escape" });

    expect(screen.getByTestId("wk-website-viewer")).toHaveAttribute(
      "data-fullscreen",
      "false",
    );
    expect(useAppStore.getState().sidebarCollapsed).toBe(false);
  });

  it("invokes onExit (and un-maximises) from the Exit control", () => {
    const onExit = vi.fn();
    render(<WebsiteViewer path={APP_PATH} onExit={onExit} />);

    // Maximise first so we can prove Exit also restores the sidebar.
    fireEvent.click(screen.getByRole("button", { name: /full screen/i }));
    expect(useAppStore.getState().sidebarCollapsed).toBe(true);

    fireEvent.click(screen.getByRole("button", { name: /^exit$/i }));
    expect(onExit).toHaveBeenCalledTimes(1);
    expect(useAppStore.getState().sidebarCollapsed).toBe(false);
  });

  it("hides the Exit control when no onExit handler is supplied", () => {
    render(<WebsiteViewer path={APP_PATH} />);
    expect(screen.queryByRole("button", { name: /^exit$/i })).toBeNull();
  });

  it("restores the sidebar when unmounted while full screen", () => {
    const { unmount } = render(<WebsiteViewer path={APP_PATH} />);
    fireEvent.click(screen.getByRole("button", { name: /full screen/i }));
    expect(useAppStore.getState().sidebarCollapsed).toBe(true);

    unmount();
    expect(useAppStore.getState().sidebarCollapsed).toBe(false);
  });

  it("marks the full-screen surface as a modal dialog (and drops the role when inline)", () => {
    render(<WebsiteViewer path={APP_PATH} />);
    const section = screen.getByTestId("wk-website-viewer");

    // Inline: a plain region, not a dialog.
    expect(section).not.toHaveAttribute("role", "dialog");
    expect(section).not.toHaveAttribute("aria-modal");

    fireEvent.click(screen.getByRole("button", { name: /full screen/i }));

    // Full screen: a contained modal dialog so focus/AT treat it as such.
    const dialog = screen.getByRole("dialog", {
      name: `App: ${APP_PATH.split("/").pop()}`,
    });
    expect(dialog).toBe(section);
    expect(dialog).toHaveAttribute("aria-modal", "true");
  });

  it("moves focus into the dialog on enter and restores it to the toggle on Escape", () => {
    render(<WebsiteViewer path={APP_PATH} />);
    const toggle = screen.getByRole("button", { name: /full screen/i });
    toggle.focus();
    expect(toggle).toHaveFocus();

    fireEvent.click(toggle);
    // Focus is contained inside the dialog (it lands on the toggle, which lives
    // in the dialog's toolbar), never stranded behind the overlay.
    const dialog = screen.getByTestId("wk-website-viewer");
    expect(dialog.contains(document.activeElement)).toBe(true);

    fireEvent.keyDown(window, { key: "Escape" });
    // Exiting full screen returns focus to the toggle that opened it.
    expect(screen.getByRole("button", { name: /full screen/i })).toHaveFocus();
  });

  it("restores focus to the toggle when exit-fullscreen is clicked", () => {
    render(<WebsiteViewer path={APP_PATH} />);
    fireEvent.click(screen.getByRole("button", { name: /full screen/i }));

    // The toggle relabels to "Exit full screen" while maximised.
    fireEvent.click(screen.getByRole("button", { name: /exit full screen/i }));
    expect(screen.getByRole("button", { name: /full screen/i })).toHaveFocus();
  });

  it("does not strand focus on <body> when Exit is used from full screen", () => {
    const onExit = vi.fn();
    render(<WebsiteViewer path={APP_PATH} onExit={onExit} />);
    fireEvent.click(screen.getByRole("button", { name: /full screen/i }));

    fireEvent.click(screen.getByRole("button", { name: /^exit$/i }));
    expect(onExit).toHaveBeenCalledTimes(1);
    // Exiting full screen first restores focus to the toggle, so focus is not
    // dropped to the document body when the Exit affordance fires.
    expect(document.activeElement).not.toBe(document.body);
    expect(screen.getByRole("button", { name: /full screen/i })).toHaveFocus();
  });

  it("announces full-screen transitions to assistive tech", () => {
    render(<WebsiteViewer path={APP_PATH} />);
    const section = screen.getByTestId("wk-website-viewer");
    const live = section.querySelector('[aria-live="polite"]');
    if (!(live instanceof HTMLElement)) {
      throw new Error("live region not found");
    }
    expect(live).toHaveTextContent("");

    fireEvent.click(screen.getByRole("button", { name: /full screen/i }));
    expect(live).toHaveTextContent("Entered full screen");

    fireEvent.click(screen.getByRole("button", { name: /exit full screen/i }));
    expect(live).toHaveTextContent("Exited full screen");
  });
});
