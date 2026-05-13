import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { InboxRow as InboxRowType } from "../../lib/types/lifecycle";
import { InboxRow } from "./InboxRow";

const row: InboxRowType = {
  taskId: "task-2741",
  title: "Refactor agent-rail event pill state machine",
  assignment: "Owner agent finished. Merge or request changes.",
  state: "decision",
  severityCounts: {
    critical: 1,
    major: 2,
    minor: 4,
    nitpick: 0,
    skipped: 0,
  },
  lastChangedAt: "2026-05-09T21:52:00Z",
  elapsed: "8m",
  isUrgent: true,
};

describe("<InboxRow>", () => {
  it("renders the title, assignment, state pill, and elapsed time", () => {
    render(
      <InboxRow
        row={row}
        isSelected={false}
        onOpen={() => undefined}
        onSelect={() => undefined}
      />,
    );
    expect(screen.getByText(row.title)).toBeInTheDocument();
    expect(screen.getByText(/Owner agent finished/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/state: decision/i)).toBeInTheDocument();
    expect(screen.getByTitle(/last changed 8m ago/i)).toBeInTheDocument();
  });

  it("invokes onOpen when the row is clicked", () => {
    const onOpen = vi.fn();
    render(
      <InboxRow
        row={row}
        isSelected={false}
        onOpen={onOpen}
        onSelect={() => undefined}
      />,
    );
    fireEvent.click(
      screen.getByRole("button", { name: (_n, el) => el.classList.contains("inbox-row") }),
    );
    expect(onOpen).toHaveBeenCalledWith("task-2741");
  });

  it("renders a button so the row is keyboard-activatable without ARIA glue", () => {
    render(
      <InboxRow
        row={row}
        isSelected={false}
        onOpen={() => undefined}
        onSelect={() => undefined}
      />,
    );
    const btn = screen.getByRole("button", { name: (_n, el) => el.classList.contains("inbox-row") });
    expect(btn.tagName).toBe("BUTTON");
  });
});
