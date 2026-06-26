import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import CitationBadge, { CitationNumberContext } from "./CitationBadge";

function renderBadge(
  props: Partial<React.ComponentProps<typeof CitationBadge>> = {},
  numbers = new Map([["task-wup-12", 3]]),
) {
  return render(
    <CitationNumberContext.Provider value={numbers}>
      <CitationBadge sourceId="task-wup-12" {...props} />
    </CitationNumberContext.Provider>,
  );
}

describe("<CitationBadge>", () => {
  it("shows the Wikipedia-style [n] label from the numbering context", () => {
    renderBadge();
    expect(screen.getByRole("button", { name: "[3]" })).toBeInTheDocument();
  });

  it("falls back to a generic marker when the id is unnumbered", () => {
    renderBadge({ sourceId: "task-wup-12" }, new Map());
    expect(screen.getByRole("button", { name: "[cite]" })).toBeInTheDocument();
  });

  it("reveals the raw citation id on focus without hitting a backend", () => {
    renderBadge();
    fireEvent.focus(screen.getByRole("button"));
    const popover = screen.getByRole("tooltip");
    expect(popover).toHaveTextContent("task-wup-12");
  });

  it("hides the popover on blur", () => {
    renderBadge();
    const button = screen.getByRole("button");
    fireEvent.focus(button);
    expect(screen.getByRole("tooltip")).toBeInTheDocument();
    fireEvent.blur(button);
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  });
});
