import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ApiError } from "../../api/client";
import type { SourceRecord } from "../../api/sources";
import CitationBadge, { CitationNumberContext } from "./CitationBadge";

const RECORD: SourceRecord = {
  id: "task-wup-12",
  kind: "task",
  title: "Ship the pilot",
  origin: "task/wup-12",
  captured_at: "2026-06-20T12:00:00Z",
  content_hash: "h1",
  content: "body",
};

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
    renderBadge({ fetchSource: async () => RECORD });
    expect(screen.getByRole("button", { name: "[3]" })).toBeInTheDocument();
  });

  it("fetches and shows the source on focus", async () => {
    const fetchSource = vi.fn(async () => RECORD);
    renderBadge({ fetchSource });
    fireEvent.focus(screen.getByRole("button"));
    expect(await screen.findByText("Ship the pilot")).toBeInTheDocument();
    expect(screen.getByText("task/wup-12")).toBeInTheDocument();
    expect(fetchSource).toHaveBeenCalledWith("task", "task-wup-12");
  });

  it("offers View source which calls onViewSource", async () => {
    const onViewSource = vi.fn();
    renderBadge({ fetchSource: async () => RECORD, onViewSource });
    fireEvent.focus(screen.getByRole("button"));
    fireEvent.click(await screen.findByRole("button", { name: "View source" }));
    expect(onViewSource).toHaveBeenCalledWith("task", "task-wup-12");
  });

  it("degrades to 'Source not found' on a 404", async () => {
    const fetchSource = vi.fn(async () => {
      throw new ApiError({
        status: 404,
        statusText: "Not Found",
        bodyText: "",
      });
    });
    renderBadge({ fetchSource });
    fireEvent.focus(screen.getByRole("button"));
    expect(await screen.findByText("Source not found")).toBeInTheDocument();
  });

  it("renders a 'not found' state for an id with no known kind prefix", async () => {
    const fetchSource = vi.fn(async () => RECORD);
    renderBadge({ sourceId: "bogus", fetchSource }, new Map());
    fireEvent.focus(screen.getByRole("button"));
    expect(await screen.findByText("Source not found")).toBeInTheDocument();
    expect(fetchSource).not.toHaveBeenCalled();
  });
});
