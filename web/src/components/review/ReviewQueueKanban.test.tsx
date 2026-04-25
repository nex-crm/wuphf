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
    await waitFor(() =>
      expect(
        screen.getByRole("heading", { name: "Reviews" }),
      ).toBeInTheDocument(),
    );
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
      expect(updateSpy).toHaveBeenCalledWith("r1", "approved");
    });
  });

  it("surfaces an error state + Retry button on fetch failure", async () => {
    vi.spyOn(api, "fetchReviews").mockRejectedValue(new Error("down"));
    render(<ReviewQueueKanban />);
    await waitFor(() => expect(screen.getByRole("alert")).toBeInTheDocument());
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
  });
});
