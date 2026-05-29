import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  type AgentRequest,
  answerRequest,
  getRequests,
} from "../../api/client";
import type { InboxItem } from "../../lib/types/inbox";
import { DecisionInbox } from "./DecisionInbox";

vi.mock("../../api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../../api/client")>();
  return {
    ...actual,
    answerRequest: vi.fn(),
    getRequests: vi.fn(),
  };
});

vi.mock("../../routes/useCurrentRoute", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("../../routes/useCurrentRoute")>();
  return {
    ...actual,
    useFallbackChannelSlug: () => "general",
  };
});

function wrap(ui: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{ui}</QueryClientProvider>;
}

const MIRA_TASK: InboxItem = {
  kind: "task",
  taskId: "task-2741",
  title: "Refactor agent-rail event pill state",
  agentSlug: "mira",
  createdAt: "2026-05-11T13:00:00Z",
  task: {
    taskId: "task-2741",
    title: "Refactor agent-rail event pill state",
    assignment: "Decide whether to ship the refactor",
    state: "decision",
    severityCounts: {
      critical: 0,
      major: 1,
      minor: 0,
      nitpick: 0,
      skipped: 0,
    },
    lastChangedAt: "2026-05-11T13:00:00Z",
    elapsed: "10m",
    isUrgent: false,
  },
};

const ADA_REQUEST: InboxItem = {
  kind: "request",
  requestId: "req-1",
  title: "Bump Postgres to 17?",
  agentSlug: "ada",
  channel: "general",
  createdAt: "2026-05-11T12:30:00Z",
  request: {
    kind: "approval",
    question: "Bump Postgres to 17 in staging?",
    from: "ada",
  },
};

const ADA_FULL_REQUEST: AgentRequest = {
  id: "req-1",
  kind: "approval",
  from: "ada",
  channel: "general",
  title: "Bump Postgres to 17?",
  question: "Bump Postgres to 17 in staging?",
  status: "pending",
  blocking: false,
  options: [{ id: "approve", label: "Approve" }],
};

const WREN_REVIEW: InboxItem = {
  kind: "review",
  reviewId: "rev-1",
  title: "Promote draft to wiki",
  agentSlug: "wren",
  createdAt: "2026-05-11T12:00:00Z",
  review: {
    state: "pending",
    reviewerSlug: "owner",
    sourceSlug: "wren",
    targetPath: "wiki/draft.md",
  },
};

describe("<DecisionInbox> (mail-style)", () => {
  beforeEach(() => {
    vi.mocked(getRequests).mockResolvedValue({ requests: [ADA_FULL_REQUEST] });
    vi.mocked(answerRequest).mockResolvedValue({});
  });

  it("renders one row per item with sender + subject", () => {
    render(
      wrap(
        <DecisionInbox initialItems={[MIRA_TASK, ADA_REQUEST, WREN_REVIEW]} />,
      ),
    );
    // Each sender appears once per row (Mira may also appear in the
    // selected detail header, hence getAllByText for the senders).
    expect(screen.getAllByText("Mira").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Ada").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Wren").length).toBeGreaterThan(0);
    expect(
      screen.getAllByText(/Refactor agent-rail event pill state/i).length,
    ).toBeGreaterThan(0);
    expect(screen.getAllByText(/Bump Postgres to 17/i).length).toBeGreaterThan(
      0,
    );
    expect(screen.getByText(/Promote draft to wiki/i)).toBeInTheDocument();
  });

  it("auto-selects the first row on mount", () => {
    render(
      wrap(
        <DecisionInbox initialItems={[MIRA_TASK, ADA_REQUEST, WREN_REVIEW]} />,
      ),
    );
    const rows = screen.getAllByRole("button", { name: /^Open/i });
    expect(rows[0]).toHaveAttribute("data-selected", "true");
    expect(rows[1]).toHaveAttribute("data-selected", "false");
  });

  // Detail-pane assertions deferred to E2E because the body now
  // embeds DecisionPacketRoute (task) / RequestItem (request) /
  // ReviewDetail (review). Each pulls live data and TanStack router
  // context which the unit harness doesn't provide. See the
  // /qa-only browser run for the equivalent coverage.
  it.skip("renders the detail pane for the selected item", () => {});
  it.skip("calls onOpenItem when a row is clicked", () => {});

  it("shows the empty state when there are no items", () => {
    render(wrap(<DecisionInbox initialItems={[]} forceState="empty" />));
    expect(screen.getAllByText(/inbox zero/i).length).toBeGreaterThanOrEqual(1);
  });

  it("flags unread rows with data-unread + bold subject", () => {
    const unreadItem: InboxItem = { ...MIRA_TASK, isUnread: true };
    const readItem: InboxItem = {
      ...ADA_REQUEST,
      isUnread: false,
    };
    render(wrap(<DecisionInbox initialItems={[unreadItem, readItem]} />));
    const rows = screen.getAllByRole("button", { name: /^Open|^Unread Open/ });
    expect(rows[0]).toHaveAttribute("data-unread", "true");
    expect(rows[1]).toHaveAttribute("data-unread", "false");
  });

  it("defaults to the Needs action filter and excludes rejected + changes_requested rows", () => {
    const rejectedTask: InboxItem = {
      ...MIRA_TASK,
      taskId: "task-rejected",
      title: "Reject ship of the refactor",
      task: { ...MIRA_TASK.task, taskId: "task-rejected", state: "rejected" },
    };
    const changesRequestedReview: InboxItem = {
      ...WREN_REVIEW,
      reviewId: "rev-cr",
      title: "Wiki promotion bounced back",
      review: { ...WREN_REVIEW.review, state: "changes_requested" },
    };
    render(
      wrap(
        <DecisionInbox
          initialItems={[
            MIRA_TASK,
            ADA_REQUEST,
            WREN_REVIEW,
            rejectedTask,
            changesRequestedReview,
          ]}
        />,
      ),
    );
    // Needs action filter is selected by default and excludes both
    // rejected tasks (terminal) and changes_requested reviews (back
    // with the submitter).
    expect(screen.getByTestId("inbox-filter-needs-action")).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(screen.queryByText(/Reject ship of the refactor/i)).toBeNull();
    expect(screen.queryByText(/Wiki promotion bounced back/i)).toBeNull();
    // The three actionable + still-pending rows remain visible.
    expect(
      screen.getAllByText(/Refactor agent-rail event pill state/i).length,
    ).toBeGreaterThan(0);
    expect(screen.getAllByText(/Bump Postgres to 17/i).length).toBeGreaterThan(
      0,
    );
    expect(screen.getByText(/Promote draft to wiki/i)).toBeInTheDocument();
    // The All chip surfaces the terminal rows when clicked.
    fireEvent.click(screen.getByTestId("inbox-filter-all"));
    expect(
      screen.getByText(/Reject ship of the refactor/i),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/Wiki promotion bounced back/i),
    ).toBeInTheDocument();
  });

  it("Unread filter chip narrows the list to unread items only", () => {
    const unreadItem: InboxItem = { ...MIRA_TASK, isUnread: true };
    const readItem: InboxItem = {
      ...ADA_REQUEST,
      isUnread: false,
    };
    render(wrap(<DecisionInbox initialItems={[unreadItem, readItem]} />));
    fireEvent.click(screen.getByTestId("inbox-filter-unread"));
    // After filter is applied the read request row should disappear.
    expect(screen.queryAllByText(/Bump Postgres to 17/i).length).toBe(0);
    expect(
      screen.getAllByText(/Refactor agent-rail event pill state/i).length,
    ).toBeGreaterThan(0);
  });

  it("shows a deterministic error when answering a request fails", async () => {
    vi.mocked(answerRequest).mockRejectedValueOnce(
      new Error("Broker unavailable"),
    );

    render(wrap(<DecisionInbox initialItems={[ADA_REQUEST]} />));

    fireEvent.click(await screen.findByRole("button", { name: "Approve" }));

    await waitFor(() => {
      expect(answerRequest).toHaveBeenCalledWith("req-1", "approve", undefined);
    });
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Broker unavailable",
    );
  });
});
