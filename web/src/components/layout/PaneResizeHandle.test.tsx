import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { PaneResizeHandle } from "./PaneResizeHandle";

interface Captured {
  reset: ReturnType<typeof vi.fn>;
  step: ReturnType<typeof vi.fn>;
}

function setup(edge: "right" | "left", isResizing = false): Captured {
  const reset = vi.fn();
  const step = vi.fn();
  render(
    <PaneResizeHandle
      edge={edge}
      ariaLabel={`Resize ${edge} pane`}
      onPointerDown={() => {}}
      isResizing={isResizing}
      onReset={reset}
      onStepResize={step}
      valueNow={220}
      valueMin={180}
      valueMax={420}
    />,
  );
  return { reset, step };
}

afterEach(() => {
  // Clear the DOM between tests without resorting to innerHTML
  // assignment, which security tooling rightly flags as a sink.
  while (document.body.firstChild) {
    document.body.removeChild(document.body.firstChild);
  }
});

describe("<PaneResizeHandle> keyboard", () => {
  it("ArrowRight widens a right-edge pane (+16px)", () => {
    const { step } = setup("right");
    fireEvent.keyDown(screen.getByRole("separator"), { key: "ArrowRight" });
    expect(step).toHaveBeenCalledWith(16);
  });

  it("ArrowLeft narrows a right-edge pane (-16px)", () => {
    const { step } = setup("right");
    fireEvent.keyDown(screen.getByRole("separator"), { key: "ArrowLeft" });
    expect(step).toHaveBeenCalledWith(-16);
  });

  it("Shift+Arrow uses the coarse step (64px)", () => {
    const { step } = setup("right");
    fireEvent.keyDown(screen.getByRole("separator"), {
      key: "ArrowRight",
      shiftKey: true,
    });
    expect(step).toHaveBeenCalledWith(64);
  });

  it("ArrowRight narrows a left-edge pane (the user's left widens)", () => {
    const { step } = setup("left");
    fireEvent.keyDown(screen.getByRole("separator"), { key: "ArrowRight" });
    expect(step).toHaveBeenCalledWith(-16);
  });

  it("Home snaps to the minimum", () => {
    const { step } = setup("right");
    fireEvent.keyDown(screen.getByRole("separator"), { key: "Home" });
    expect(step).toHaveBeenCalledWith(Number.NEGATIVE_INFINITY);
  });

  it("End snaps to the maximum", () => {
    const { step } = setup("right");
    fireEvent.keyDown(screen.getByRole("separator"), { key: "End" });
    expect(step).toHaveBeenCalledWith(Number.POSITIVE_INFINITY);
  });

  it("Enter resets to the default", () => {
    const { reset, step } = setup("right");
    fireEvent.keyDown(screen.getByRole("separator"), { key: "Enter" });
    expect(reset).toHaveBeenCalledTimes(1);
    expect(step).not.toHaveBeenCalled();
  });

  it("exposes aria-valuenow/min/max", () => {
    setup("right");
    const sep = screen.getByRole("separator");
    expect(sep.getAttribute("aria-valuenow")).toBe("220");
    expect(sep.getAttribute("aria-valuemin")).toBe("180");
    expect(sep.getAttribute("aria-valuemax")).toBe("420");
  });

  it("applies is-active class while resizing", () => {
    setup("right", true);
    expect(screen.getByRole("separator").className).toMatch(/is-active/);
  });
});
