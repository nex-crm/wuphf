import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { ReviewItem } from "../../api/notebook";
import * as api from "../../api/notebook";
import ReviewQueueKanban from "./ReviewQueueKanban";

function mkReview(
  id: string,
  state: ReviewItem["state"],
  title: string,
): ReviewItem {
  return {
    id,
    agent_slug: "pm",
    entry_slug: "e",
    entry_title: title,
    proposed_wiki_path: "p/q",
    excerpt: "x",
    reviewer_slug: "ceo",
    state,
    submitted_ts: new Date().toISOString(),
    updated_ts: new Date().toISOString(),
    comments: [],
  };
}

const MOCK_REVIEWS: ReviewItem[] = [
  mkReview("r1", "pending", "Pending one"),
  mkReview("r2", "in-review", "In review one"),
  mkReview("r3", "changes-requested", "Changes one"),
  mkReview("r4", "approved", "Approved one"),
];

describe("<ReviewQueueKanban>", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(api, "subscribeNotebookEvents").mockImplementation(() => () => {});
  });

  it("renders five state columns with their cards", async () => {
    vi.spyOn(api, "fetchReviews").mockResolvedValue(MOCK_REVIEWS);
    render(<ReviewQueueKanban />);
    await screen.findByText("Pending one");
    expect(
      screen.getByRole("heading", { name: "Pending" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "In review" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "Changes requested" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "Approved" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "Archived" }),
    ).toBeInTheDocument();

    expect(screen.getByText("Pending one")).toBeInTheDocument();
    expect(screen.getByText("In review one")).toBeInTheDocument();
  });

  it("opens the notebook entry in place when a card is clicked with a navigator", async () => {
    vi.spyOn(api, "fetchReviews").mockResolvedValue(MOCK_REVIEWS);
    const onOpenEntry = vi.fn();
    render(<ReviewQueueKanban onOpenEntry={onOpenEntry} />);
    await waitFor(() =>
      expect(screen.getByText("Pending one")).toBeInTheDocument(),
    );
    await userEvent.setup().click(screen.getByText("Pending one"));
    expect(onOpenEntry).toHaveBeenCalledWith("pm", "e");
    // No drawer — the click target is the notebook item itself.
    expect(screen.queryByTestId("nb-review-drawer")).toBeNull();
  });

  it("opens the detail drawer when a card is clicked", async () => {
    vi.spyOn(api, "fetchReviews").mockResolvedValue(MOCK_REVIEWS);
    render(<ReviewQueueKanban />);
    await waitFor(() =>
      expect(screen.getByText("Pending one")).toBeInTheDocument(),
    );
    await userEvent.setup().click(screen.getByText("Pending one"));
    await waitFor(() =>
      expect(screen.getByTestId("nb-review-drawer")).toBeInTheDocument(),
    );
  });

  it("optimistically moves a card when approved from the drawer", async () => {
    vi.spyOn(api, "fetchReviews").mockResolvedValue([
      mkReview("r1", "pending", "My card"),
    ]);
    const updateSpy = vi.spyOn(api, "updateReviewState").mockResolvedValue({
      ...mkReview("r1", "approved", "My card"),
    });
    render(<ReviewQueueKanban />);
    await waitFor(() =>
      expect(screen.getByText("My card")).toBeInTheDocument(),
    );
    const user = userEvent.setup();
    await user.click(screen.getByText("My card"));
    await waitFor(() =>
      expect(screen.getByTestId("nb-review-drawer")).toBeInTheDocument(),
    );
    await user.click(screen.getByRole("button", { name: "Approve" }));
    await waitFor(() => {
      expect(updateSpy).toHaveBeenCalledWith("r1", "approved", {
        rationale: undefined,
      });
    });
  });

  it("passes the typed rationale through on request-changes", async () => {
    vi.spyOn(api, "fetchReviews").mockResolvedValue([
      mkReview("r1", "in-review", "My card"),
    ]);
    const updateSpy = vi.spyOn(api, "updateReviewState").mockResolvedValue({
      ...mkReview("r1", "changes-requested", "My card"),
    });
    render(<ReviewQueueKanban />);
    await waitFor(() =>
      expect(screen.getByText("My card")).toBeInTheDocument(),
    );
    const user = userEvent.setup();
    await user.click(screen.getByText("My card"));
    await waitFor(() =>
      expect(screen.getByTestId("nb-review-drawer")).toBeInTheDocument(),
    );
    await user.click(screen.getByRole("button", { name: "Request changes" }));
    await user.type(
      screen.getByTestId("nb-review-rationale-input"),
      "Merge, don't duplicate.",
    );
    await user.click(screen.getByTestId("nb-review-rationale-submit"));
    await waitFor(() => {
      expect(updateSpy).toHaveBeenCalledWith("r1", "changes-requested", {
        rationale: "Merge, don't duplicate.",
      });
    });
  });

  it("surfaces a visible error when a review action fails", async () => {
    vi.spyOn(api, "fetchReviews").mockResolvedValue([
      mkReview("r1", "in-review", "My card"),
    ]);
    vi.spyOn(api, "updateReviewState").mockRejectedValue(
      new Error('{"error":"rationale is required"}'),
    );
    render(<ReviewQueueKanban />);
    await waitFor(() =>
      expect(screen.getByText("My card")).toBeInTheDocument(),
    );
    const user = userEvent.setup();
    await user.click(screen.getByText("My card"));
    await waitFor(() =>
      expect(screen.getByTestId("nb-review-drawer")).toBeInTheDocument(),
    );
    await user.click(screen.getByRole("button", { name: "Approve" }));
    await waitFor(() => {
      expect(screen.getByTestId("nb-review-action-error")).toBeInTheDocument();
    });
    // The drawer stays open on failure so the human sees the error.
    expect(screen.getByTestId("nb-review-drawer")).toBeInTheDocument();
  });

  it("surfaces an error state + Retry button on fetch failure", async () => {
    vi.spyOn(api, "fetchReviews").mockRejectedValue(new Error("down"));
    render(<ReviewQueueKanban />);
    await waitFor(() => expect(screen.getByRole("alert")).toBeInTheDocument());
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
  });

  it("never renders zero counts as fact while the load is pending (C4)", () => {
    // The v3 eval caught the header claiming "0 reviews · 0 open · 0
    // recently approved" over a hung /review/list. Pending must read as
    // pending — keep the promise unresolved and assert no zeros render.
    vi.spyOn(api, "fetchReviews").mockImplementation(
      () => new Promise(() => {}),
    );
    render(<ReviewQueueKanban />);
    expect(screen.getByText(/Loading reviews/)).toBeInTheDocument();
    expect(screen.queryByText(/0 reviews/)).toBeNull();
    expect(screen.getByText("Loading…")).toBeInTheDocument();
  });

  it("marks the header counts unavailable on a failed load (C4)", async () => {
    vi.spyOn(api, "fetchReviews").mockRejectedValue(
      new Error("Broker not responding — request timed out."),
    );
    render(<ReviewQueueKanban />);
    await waitFor(() => expect(screen.getByRole("alert")).toBeInTheDocument());
    expect(screen.queryByText(/0 reviews/)).toBeNull();
    expect(screen.getByText("Unavailable")).toBeInTheDocument();
  });

  it("shows true empty copy when the load succeeds with zero reviews (C4)", async () => {
    vi.spyOn(api, "fetchReviews").mockResolvedValue([]);
    render(<ReviewQueueKanban />);
    await waitFor(() =>
      expect(screen.getByTestId("review-queue-empty")).toBeInTheDocument(),
    );
    // Counts may honestly read zero now — the load actually completed.
    expect(screen.getByText(/0 reviews/)).toBeInTheDocument();
  });

  it("renders the grade badge inside the Kanban for a card aged >= 12 hours", async () => {
    const oldCard = {
      ...mkReview("r5", "pending", "Old pending"),
      submitted_ts: new Date(Date.now() - 13 * 3_600_000).toISOString(),
    };
    vi.spyOn(api, "fetchReviews").mockResolvedValue([oldCard]);
    render(<ReviewQueueKanban />);
    await waitFor(() =>
      expect(screen.getByText("Old pending")).toBeInTheDocument(),
    );
    expect(screen.getByText("Waiting")).toBeInTheDocument();
  });
});
