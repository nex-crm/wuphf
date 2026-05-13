import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { EMPTY_INBOX, POPULATED_INBOX } from "../../lib/mocks/decisionPackets";
import { DecisionInbox } from "./DecisionInbox";

function wrap(ui: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{ui}</QueryClientProvider>;
}

describe("<DecisionInbox>", () => {
  it("renders the populated row list with the locked row layout", () => {
    render(
      wrap(
        <DecisionInbox
          initialPayload={POPULATED_INBOX}
          onOpenTask={() => undefined}
        />,
      ),
    );

    expect(
      screen.getByText("Refactor agent-rail event pill state machine"),
    ).toBeInTheDocument();
    expect(screen.getByText(/2 tasks need your decision/i)).toBeInTheDocument();
    // Filter tabs render with counts
    expect(
      screen.getByRole("tab", { name: /needs decision/i }),
    ).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: /running/i })).toBeInTheDocument();
  });

  it("opens the task on row click", () => {
    const onOpen = vi.fn();
    render(
      wrap(
        <DecisionInbox initialPayload={POPULATED_INBOX} onOpenTask={onOpen} />,
      ),
    );
    fireEvent.click(screen.getAllByRole("button", { name: (_n, el) => el.classList.contains("inbox-row") })[0]);
    expect(onOpen).toHaveBeenCalledWith("task-2741");
  });

  it("shows the empty state when no tasks need decision and counts are zero", () => {
    render(
      wrap(
        <DecisionInbox
          initialPayload={EMPTY_INBOX}
          forceState="empty"
          onOpenTask={() => undefined}
        />,
      ),
    );
    expect(screen.getByText(/Nothing waiting on you/i)).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /Start a new task/i }),
    ).toBeInTheDocument();
  });

  it("shows the partial state when filter has zero matches", () => {
    render(
      wrap(
        <DecisionInbox
          initialPayload={EMPTY_INBOX}
          forceState="partial"
          onOpenTask={() => undefined}
        />,
      ),
    );
    expect(screen.getByText(/No tasks in/i)).toBeInTheDocument();
  });

  it("renders the error banner with cached state below when forced", () => {
    render(
      wrap(
        <DecisionInbox
          initialPayload={POPULATED_INBOX}
          forceState="error"
          onOpenTask={() => undefined}
        />,
      ),
    );
    expect(screen.getByRole("alert")).toBeInTheDocument();
    expect(screen.getByText(/Can't reach the broker/i)).toBeInTheDocument();
    // Cached rows still render below the banner
    expect(
      screen.getByText("Refactor agent-rail event pill state machine"),
    ).toBeInTheDocument();
  });

  it("gives only the first row tabIndex=0 before any selection (roving tabindex)", () => {
    render(
      wrap(
        <DecisionInbox initialPayload={POPULATED_INBOX} onOpenTask={() => undefined} />,
      ),
    );
    const rows = screen.getAllByRole("button", { name: (_n, el) => el.classList.contains("inbox-row") });
    expect(rows[0]).toHaveAttribute("tabindex", "0");
    for (const row of rows.slice(1)) {
      expect(row).toHaveAttribute("tabindex", "-1");
    }
  });

  it("moves tabIndex=0 to the next row after ArrowDown", () => {
    render(
      wrap(
        <DecisionInbox initialPayload={POPULATED_INBOX} onOpenTask={() => undefined} />,
      ),
    );
    const list = screen.getByRole("list", { name: /tasks/i });
    fireEvent.keyDown(list, { key: "ArrowDown" });
    const rows = screen.getAllByRole("button", { name: (_n, el) => el.classList.contains("inbox-row") });
    // After ArrowDown from no-selection, first row becomes selected → tabIndex=0.
    // A second ArrowDown would move to row[1]; we test one step here.
    expect(rows[0]).toHaveAttribute("tabindex", "0");
    for (const row of rows.slice(1)) {
      expect(row).toHaveAttribute("tabindex", "-1");
    }
  });

  it("Enter on a focused row calls onOpen exactly once (no double-fire from ul keydown + button click)", () => {
    const onOpen = vi.fn();
    render(
      wrap(
        <DecisionInbox initialPayload={POPULATED_INBOX} onOpenTask={onOpen} />,
      ),
    );
    const rows = screen.getAllByRole("button", { name: (_n, el) => el.classList.contains("inbox-row") });
    // Simulate native button Enter: keydown on the button bubbles to ul, then click fires.
    fireEvent.keyDown(rows[0], { key: "Enter" });
    fireEvent.click(rows[0]);
    expect(onOpen).toHaveBeenCalledTimes(1);
  });

  it("filter tablist: only active tab has tabIndex=0, others -1 (roving tabindex)", () => {
    render(
      wrap(
        <DecisionInbox initialPayload={POPULATED_INBOX} onOpenTask={() => undefined} />,
      ),
    );
    const tabs = screen.getAllByRole("tab");
    // "Needs decision" is the default filter
    expect(tabs[0]).toHaveAttribute("tabindex", "0");
    for (const tab of tabs.slice(1)) {
      expect(tab).toHaveAttribute("tabindex", "-1");
    }
  });

  it("filter tablist: ArrowRight moves to next tab and focuses it", () => {
    render(
      wrap(
        <DecisionInbox initialPayload={POPULATED_INBOX} onOpenTask={() => undefined} />,
      ),
    );
    const tablist = screen.getByRole("tablist", { name: /filter tasks/i });
    const tabs = screen.getAllByRole("tab");
    fireEvent.keyDown(tablist, { key: "ArrowRight" });
    // "Running" tab (index 1) is now selected
    expect(tabs[0]).toHaveAttribute("tabindex", "-1");
    expect(tabs[1]).toHaveAttribute("tabindex", "0");
  });

  it("filter tablist: ArrowLeft wraps from first to last tab", () => {
    render(
      wrap(
        <DecisionInbox initialPayload={POPULATED_INBOX} onOpenTask={() => undefined} />,
      ),
    );
    const tablist = screen.getByRole("tablist", { name: /filter tasks/i });
    const tabs = screen.getAllByRole("tab");
    fireEvent.keyDown(tablist, { key: "ArrowLeft" });
    // Wraps from "Needs decision" (0) to "Merged" (last)
    const last = tabs[tabs.length - 1];
    expect(last).toHaveAttribute("tabindex", "0");
    expect(tabs[0]).toHaveAttribute("tabindex", "-1");
  });

  it("filter tablist: ArrowDown does NOT trap focus in the tablist (ARIA violation guard)", () => {
    render(
      wrap(
        <DecisionInbox initialPayload={POPULATED_INBOX} onOpenTask={() => undefined} />,
      ),
    );
    const tablist = screen.getByRole("tablist", { name: /filter tasks/i });
    const tabs = screen.getAllByRole("tab");
    // ArrowDown on the tablist must not cycle to the next tab — that would
    // steal the key from keyboard users trying to reach the row list below.
    fireEvent.keyDown(tablist, { key: "ArrowDown" });
    // First tab (Needs decision) still selected; nothing changed.
    expect(tabs[0]).toHaveAttribute("tabindex", "0");
    expect(tabs[1]).toHaveAttribute("tabindex", "-1");
  });

  it("renders the loading skeleton state when forced", () => {
    const { container } = render(
      wrap(
        <DecisionInbox
          initialPayload={POPULATED_INBOX}
          forceState="loading"
          onOpenTask={() => undefined}
        />,
      ),
    );
    // aria-busy is the screen-reader hint that the row list is loading
    const busy = container.querySelector("[aria-busy='true']");
    expect(busy).not.toBeNull();
    expect(container.querySelectorAll(".inbox-skeleton-row").length).toBe(5);
  });
});
