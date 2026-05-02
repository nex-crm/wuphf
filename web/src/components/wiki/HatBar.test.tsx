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
  it("marks the active tab and disables the Talk tab by default", () => {
    render(<HatBar active="article" />);
    const talk = screen.getByRole("button", { name: "Talk" });
    expect(talk).toBeDisabled();
    expect(screen.getByRole("button", { name: "Article" })).toHaveClass(
      "active",
    );
  });

  it("fires onChange when a non-active, non-disabled tab is clicked", () => {
    const onChange = vi.fn();
    render(<HatBar active="article" onChange={onChange} />);
    fireEvent.click(screen.getByRole("button", { name: "History" }));
    expect(onChange).toHaveBeenCalledWith("history");
  });

  it("does not fire onChange when a disabled tab is clicked", () => {
    const onChange = vi.fn();
    render(<HatBar active="article" onChange={onChange} />);
    fireEvent.click(screen.getByRole("button", { name: "Talk" }));
    expect(onChange).not.toHaveBeenCalled();
  });

  it("fires onChange when the Edit source tab is clicked", () => {
    const onChange = vi.fn();
    render(<HatBar active="article" onChange={onChange} />);
    fireEvent.click(screen.getByRole("button", { name: "Edit source" }));
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
