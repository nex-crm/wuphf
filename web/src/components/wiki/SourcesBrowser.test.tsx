import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { SourceMetadata, SourceRecord } from "../../api/sources";
import SourcesBrowser from "./SourcesBrowser";

const SAMPLE: SourceMetadata[] = [
  {
    id: "task-wup-12",
    kind: "task",
    title: "Ship the pilot",
    origin: "task/wup-12",
    captured_at: "2026-06-20T12:00:00Z",
    content_hash: "a1",
  },
  {
    id: "decision-rrf-1",
    kind: "decision",
    title: "Adopt RRF",
    origin: "decisions/2026-06-15",
    captured_at: "2026-06-15T16:00:00Z",
    content_hash: "b1",
  },
];

const RECORD: SourceRecord = {
  ...SAMPLE[0],
  content: "## Outcome\n\nThe pilot converted.",
};

const noop = () => {};

describe("<SourcesBrowser>", () => {
  it("groups records by kind", async () => {
    render(
      <SourcesBrowser
        selection={null}
        onSelect={noop}
        onBack={noop}
        listSourcesFn={async () => SAMPLE}
      />,
    );
    expect(await screen.findByText("Ship the pilot")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: /Task/ })).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: /Decision/ }),
    ).toBeInTheDocument();
  });

  it("calls onSelect when a row is clicked", async () => {
    const onSelect = vi.fn();
    render(
      <SourcesBrowser
        selection={null}
        onSelect={onSelect}
        onBack={noop}
        listSourcesFn={async () => SAMPLE}
      />,
    );
    fireEvent.click(await screen.findByText("Ship the pilot"));
    expect(onSelect).toHaveBeenCalledWith("task", "task-wup-12");
  });

  it("shows an empty state when there are no sources", async () => {
    render(
      <SourcesBrowser
        selection={null}
        onSelect={noop}
        onBack={noop}
        listSourcesFn={async () => []}
      />,
    );
    expect(await screen.findByTestId("wk-sources-empty")).toBeInTheDocument();
  });

  it("renders a selected record as markdown", async () => {
    render(
      <SourcesBrowser
        selection={{ kind: "task", id: "task-wup-12" }}
        onSelect={noop}
        onBack={noop}
        readSourceFn={async () => RECORD}
      />,
    );
    expect(
      await screen.findByRole("heading", { name: "Ship the pilot" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "Outcome" }),
    ).toBeInTheDocument();
  });

  it("shows 'source not found' for a missing record", async () => {
    render(
      <SourcesBrowser
        selection={{ kind: "task", id: "task-gone" }}
        onSelect={noop}
        onBack={noop}
        readSourceFn={async () => {
          throw Object.assign(new Error("not found"), { status: 404 });
        }}
      />,
    );
    expect(await screen.findByText(/Source not found/)).toBeInTheDocument();
  });
});
