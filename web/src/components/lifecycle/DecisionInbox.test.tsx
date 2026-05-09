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
    fireEvent.click(screen.getAllByRole("button", { name: /open task/i })[0]);
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
