import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import ImageViewer from "./ImageViewer";

// The viewer composes its element `src` from `wikiFileUrl`; mock it so the
// test never depends on the real auth/token plumbing and asserts only the
// viewer's own behavior.
vi.mock("../../../api/wiki", () => ({
  wikiFileUrl: vi.fn(
    (path: string) => `https://broker.test/wiki/file?path=${path}`,
  ),
}));

describe("<ImageViewer>", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("shows the loading state before the image reports load", () => {
    render(<ImageViewer path="team/assets/diagram.png" />);
    expect(screen.getByText("Loading image…")).toBeInTheDocument();
  });

  it("renders the image with the tokened src and reveals it on load", () => {
    render(<ImageViewer path="team/assets/diagram.png" />);
    const img = screen.getByAltText("diagram.png");
    expect(img).toHaveAttribute(
      "src",
      "https://broker.test/wiki/file?path=team/assets/diagram.png",
    );
    // The zoom wrapper around the image is `hidden` until the native load
    // event fires, so the affordance is absent from the a11y tree at first.
    const zoom = img.closest("button");
    expect(zoom).toHaveAttribute("hidden");
    fireEvent.load(img);
    expect(zoom).not.toHaveAttribute("hidden");
    expect(screen.queryByText("Loading image…")).not.toBeInTheDocument();
  });

  it("renders the error state when the image fails to load", () => {
    render(<ImageViewer path="team/assets/broken.png" />);
    const img = screen.getByAltText("broken.png");
    fireEvent.error(img);
    expect(screen.getByRole("alert")).toHaveTextContent(
      /could not load image “broken\.png”/i,
    );
  });

  it("opens a lightbox via the zoom button and closes it with Escape", () => {
    render(<ImageViewer path="team/assets/diagram.png" />);
    // Drive the image to the ready state so the zoom button is exposed.
    fireEvent.load(screen.getByAltText("diagram.png"));
    fireEvent.click(
      screen.getByRole("button", { name: /zoom image diagram\.png/i }),
    );

    const dialog = screen.getByRole("dialog", {
      name: /image preview: diagram\.png/i,
    });
    expect(dialog).toBeInTheDocument();

    fireEvent.keyDown(window, { key: "Escape" });
    expect(
      screen.queryByRole("dialog", { name: /image preview/i }),
    ).not.toBeInTheDocument();
  });

  it("traps focus in the lightbox: moves focus in on open, restores it on close", () => {
    render(<ImageViewer path="team/assets/diagram.png" />);
    fireEvent.load(screen.getByAltText("diagram.png"));

    const zoom = screen.getByRole("button", {
      name: /zoom image diagram\.png/i,
    });
    zoom.focus();
    expect(zoom).toHaveFocus();

    fireEvent.click(zoom);
    const dialog = screen.getByRole("dialog", {
      name: /image preview: diagram\.png/i,
    });
    // useFocusTrap moves focus to the first focusable control inside the dialog.
    expect(dialog.contains(document.activeElement)).toBe(true);

    // Closing the lightbox restores focus to the control that opened it.
    fireEvent.keyDown(window, { key: "Escape" });
    expect(
      screen.queryByRole("dialog", { name: /image preview/i }),
    ).not.toBeInTheDocument();
    expect(zoom).toHaveFocus();
  });

  it("hides the zoom affordance until the image has loaded", () => {
    render(<ImageViewer path="team/assets/diagram.png" />);
    // While loading the zoom button is hidden, so it is absent from the a11y
    // tree and the lightbox can never open.
    expect(
      screen.queryByRole("button", { name: /zoom image diagram\.png/i }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("dialog", { name: /image preview/i }),
    ).not.toBeInTheDocument();
  });
});
