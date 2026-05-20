// @vitest-environment happy-dom

import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import {
  WORK_BOARD_COLUMN_ORDER,
  WorkBoardColumn,
} from "../../../src/renderer/work-board/WorkBoardColumn.tsx";

import { sampleThreadView } from "./fixtures.ts";

describe("WorkBoardColumn", () => {
  it("renders a heading, count, and empty hint when no threads", () => {
    render(<WorkBoardColumn column="needs_me" threads={[]} />);
    const column = screen.getByTestId("work-board-column");
    expect(column).toHaveAttribute("data-column", "needs_me");
    expect(within(column).getByText("Needs me")).toBeInTheDocument();
    expect(within(column).getByText("0")).toBeInTheDocument();
    expect(within(column).getByText("Nothing waiting on you.")).toBeInTheDocument();
  });

  it("renders one ThreadCard per thread and reflects the count", () => {
    render(
      <WorkBoardColumn
        column="running"
        threads={[sampleThreadView(), sampleThreadView({ title: "Other" })]}
      />,
    );
    const column = screen.getByTestId("work-board-column");
    expect(within(column).getAllByTestId("thread-card")).toHaveLength(2);
    expect(within(column).getByText("2")).toBeInTheDocument();
    expect(within(column).queryByText("No threads in flight.")).not.toBeInTheDocument();
  });

  it("uses the right heading + hint copy per column", () => {
    const expectations: Record<string, { heading: string; emptyHint: string }> = {
      needs_me: { heading: "Needs me", emptyHint: "Nothing waiting on you." },
      running: { heading: "Running", emptyHint: "No threads in flight." },
      review: { heading: "Review", emptyHint: "Nothing waiting on review." },
      done: { heading: "Done", emptyHint: "Nothing closed yet." },
    };
    for (const column of WORK_BOARD_COLUMN_ORDER) {
      const { unmount } = render(<WorkBoardColumn column={column} threads={[]} />);
      const expected = expectations[column];
      if (expected === undefined) throw new Error(`missing expectation for ${column}`);
      expect(screen.getByText(expected.heading)).toBeInTheDocument();
      expect(screen.getByText(expected.emptyHint)).toBeInTheDocument();
      unmount();
    }
  });
});

describe("WORK_BOARD_COLUMN_ORDER", () => {
  it("orders human-attention first, then in-flight, then review, then done", () => {
    expect([...WORK_BOARD_COLUMN_ORDER]).toEqual(["needs_me", "running", "review", "done"]);
  });
});
