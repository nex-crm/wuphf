import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type {
  NotebookAgentSummary,
  NotebookEntry,
  ReviewItem,
} from "../../api/notebook";
import * as api from "../../api/notebook";
import AgentNotebookView from "./AgentNotebookView";

const PM_AGENT: NotebookAgentSummary = {
  agent_slug: "pm",
  name: "PM",
  role: "Product Manager · agent",
  entries: [],
  total: 2,
  promoted_count: 0,
  last_updated_ts: new Date().toISOString(),
};

const PM_ENTRIES: NotebookEntry[] = [
  {
    agent_slug: "pm",
    entry_slug: "acme",
    title: "Customer Acme rough notes",
    body_md: "Body.",
    last_edited_ts: new Date().toISOString(),
    revisions: 1,
    status: "draft",
    file_path: "acme.md",
    reviewer_slug: "ceo",
  },
  {
    agent_slug: "pm",
    entry_slug: "pricing",
    title: "Pricing objections",
    body_md: "Body 2.",
    last_edited_ts: new Date(Date.now() - 60_000).toISOString(),
    revisions: 1,
    status: "draft",
    file_path: "pricing.md",
    reviewer_slug: "ceo",
  },
];

describe("<AgentNotebookView>", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("loads the agent and renders the most recent entry by default", async () => {
    vi.spyOn(api, "fetchAgentEntries").mockResolvedValue({
      agent: PM_AGENT,
      entries: PM_ENTRIES,
      reviews: [],
    });
    render(
      <AgentNotebookView
        agentSlug="pm"
        entrySlug={null}
        onNavigateCatalog={() => {}}
        onSelectEntry={() => {}}
      />,
    );
    await waitFor(() =>
      expect(
        screen.getByRole("heading", { name: "Customer Acme rough notes" }),
      ).toBeInTheDocument(),
    );
    expect(
      screen.getByRole("heading", { name: "PM's notebook" }),
    ).toBeInTheDocument();
  });

  it("renders the specified entry when entrySlug is provided", async () => {
    vi.spyOn(api, "fetchAgentEntries").mockResolvedValue({
      agent: PM_AGENT,
      entries: PM_ENTRIES,
      reviews: [],
    });
    render(
      <AgentNotebookView
        agentSlug="pm"
        entrySlug="pricing"
        onNavigateCatalog={() => {}}
        onSelectEntry={() => {}}
      />,
    );
    await waitFor(() =>
      expect(
        screen.getByRole("heading", { name: "Pricing objections" }),
      ).toBeInTheDocument(),
    );
  });

  it("shows landing prompt when agent has no entries", async () => {
    vi.spyOn(api, "fetchAgentEntries").mockResolvedValue({
      agent: { ...PM_AGENT, total: 0 },
      entries: [],
      reviews: [],
    });
    render(
      <AgentNotebookView
        agentSlug="pm"
        entrySlug={null}
        onNavigateCatalog={() => {}}
        onSelectEntry={() => {}}
      />,
    );
    await waitFor(() =>
      expect(
        screen.getByText(/PM has not written anything/),
      ).toBeInTheDocument(),
    );
  });

  it("renders an error state with retry button when fetch fails", async () => {
    vi.spyOn(api, "fetchAgentEntries").mockRejectedValue(new Error("boom"));
    render(
      <AgentNotebookView
        agentSlug="pm"
        entrySlug={null}
        onNavigateCatalog={() => {}}
        onSelectEntry={() => {}}
      />,
    );
    await waitFor(() => expect(screen.getByRole("alert")).toBeInTheDocument());
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
  });

  it("shows agent-not-found fallback when API returns null agent", async () => {
    vi.spyOn(api, "fetchAgentEntries").mockResolvedValue({
      agent: null,
      entries: [],
      reviews: [],
    });
    render(
      <AgentNotebookView
        agentSlug="zzz"
        entrySlug={null}
        onNavigateCatalog={() => {}}
        onSelectEntry={() => {}}
      />,
    );
    await waitFor(() =>
      expect(screen.getByText(/Agent not found/)).toBeInTheDocument(),
    );
  });

  // ── In-place review bar (founder directive: review the notebook item
  // itself — Approve / Request changes live on the entry view). ──────────

  const ACME_REVIEW: ReviewItem = {
    id: "r-acme",
    agent_slug: "pm",
    entry_slug: "acme",
    entry_title: "Customer Acme rough notes",
    proposed_wiki_path: "team/accounts/acme.md",
    excerpt: "Body.",
    reviewer_slug: "ceo",
    state: "in-review",
    submitted_ts: new Date().toISOString(),
    updated_ts: new Date().toISOString(),
    comments: [],
  };

  function renderWithActiveReview() {
    vi.spyOn(api, "fetchAgentEntries").mockResolvedValue({
      agent: PM_AGENT,
      entries: [{ ...PM_ENTRIES[0], status: "in-review" }],
      reviews: [ACME_REVIEW],
    });
    render(
      <AgentNotebookView
        agentSlug="pm"
        entrySlug="acme"
        onNavigateCatalog={() => {}}
        onSelectEntry={() => {}}
      />,
    );
  }

  it("renders the in-place review bar when the entry has an actionable review", async () => {
    renderWithActiveReview();
    await waitFor(() =>
      expect(screen.getByTestId("nb-inline-review")).toBeInTheDocument(),
    );
    expect(screen.getByRole("button", { name: "Approve" })).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Request changes" }),
    ).toBeInTheDocument();
  });

  it("approves the review in place via the existing approve action", async () => {
    const updateSpy = vi
      .spyOn(api, "updateReviewState")
      .mockResolvedValue({ ...ACME_REVIEW, state: "approved" });
    renderWithActiveReview();
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Approve" }),
      ).toBeInTheDocument(),
    );
    await userEvent
      .setup()
      .click(screen.getByRole("button", { name: "Approve" }));
    await waitFor(() => {
      expect(updateSpy).toHaveBeenCalledWith("r-acme", "approved", {
        rationale: undefined,
      });
    });
  });

  it("request-changes requires text and submits the typed comment", async () => {
    const updateSpy = vi
      .spyOn(api, "updateReviewState")
      .mockResolvedValue({ ...ACME_REVIEW, state: "changes-requested" });
    renderWithActiveReview();
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Request changes" }),
      ).toBeInTheDocument(),
    );
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Request changes" }));
    // Empty submit is rejected with a visible error and NO call.
    await user.click(screen.getByTestId("nb-review-rationale-submit"));
    expect(screen.getByText(/Say what needs to change/)).toBeInTheDocument();
    expect(updateSpy).not.toHaveBeenCalled();
    // Typing the comment and submitting fires the review call — the broker
    // composes the owner task from this one request.
    await user.type(
      screen.getByTestId("nb-review-rationale-input"),
      "Fold the access steps into one checklist.",
    );
    await user.click(screen.getByTestId("nb-review-rationale-submit"));
    await waitFor(() => {
      expect(updateSpy).toHaveBeenCalledWith("r-acme", "changes-requested", {
        rationale: "Fold the access steps into one checklist.",
      });
    });
  });

  it("surfaces a visible error when an in-place review action fails", async () => {
    vi.spyOn(api, "updateReviewState").mockRejectedValue(
      new Error("rationale is required"),
    );
    renderWithActiveReview();
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Approve" }),
      ).toBeInTheDocument(),
    );
    await userEvent
      .setup()
      .click(screen.getByRole("button", { name: "Approve" }));
    await waitFor(() =>
      expect(screen.getByTestId("nb-review-action-error")).toBeInTheDocument(),
    );
  });
});
