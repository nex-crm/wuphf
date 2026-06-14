import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import HatBar from "./HatBar";

function hasDuplicateKeyWarning(calls: unknown[][]) {
  return calls.some((args) =>
    args.some((arg) =>
      String(arg).includes("Encountered two children with the same key"),
    ),
  );
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("<HatBar>", () => {
  it("marks the active tab and renders the four view tabs", () => {
    render(<HatBar active="article" />);
    expect(screen.getByRole("button", { name: "Read" })).toHaveClass("active");
    for (const label of ["Read", "Edit", "History", "Raw markdown"]) {
      expect(screen.getByRole("button", { name: label })).toBeInTheDocument();
    }
    // The Wikipedia-era Talk tab is gone (docmost-minimal tab set).
    expect(screen.queryByRole("button", { name: "Talk" })).toBeNull();
  });

  it("fires onChange when a non-active tab is clicked", () => {
    const onChange = vi.fn();
    render(<HatBar active="article" onChange={onChange} />);
    fireEvent.click(screen.getByRole("button", { name: "History" }));
    expect(onChange).toHaveBeenCalledWith("history");
  });

  it("does not fire onChange when a disabled tab is clicked", () => {
    const onChange = vi.fn();
    render(
      <HatBar active="article" onChange={onChange} disabledTabs={["edit"]} />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    expect(onChange).not.toHaveBeenCalled();
  });

  it("fires onChange when the Edit tab is clicked", () => {
    const onChange = vi.fn();
    render(<HatBar active="article" onChange={onChange} />);
    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    expect(onChange).toHaveBeenCalledWith("edit");
  });

  it("renders right-rail context when provided", () => {
    render(
      <HatBar
        active="article"
        rightRail={["Cincinnati, OH", "Mid-market Logistics"]}
      />,
    );
    expect(screen.getByText(/Cincinnati, OH/)).toBeInTheDocument();
    expect(screen.getByText(/Mid-market Logistics/)).toBeInTheDocument();
  });

  it("renders duplicate suffix-shaped right-rail items without duplicate React keys", () => {
    const errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    render(
      <HatBar active="article" rightRail={["Draft", "Draft", "Draft#1"]} />,
    );

    expect(screen.getAllByText("Draft")).toHaveLength(2);
    expect(screen.getByText("Draft#1")).toBeInTheDocument();
    expect(hasDuplicateKeyWarning(errorSpy.mock.calls)).toBe(false);
  });
});
