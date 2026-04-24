import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { ReviewItem } from "../../api/notebook";
import ReviewCard from "./ReviewCard";

const REVIEW: ReviewItem = {
  id: "r1",
  agent_slug: "pm",
  entry_slug: "acme",
  entry_title: "Customer Acme rough notes",
  proposed_wiki_path: "customers/acme-logistics",
  excerpt: "A tiny excerpt that should appear as the card preview.",
  reviewer_slug: "ceo",
  state: "pending",
  submitted_ts: new Date().toISOString(),
  updated_ts: new Date().toISOString(),
  comments: [],
};

describe("<ReviewCard>", () => {
  it("renders title, excerpt, and proposed wiki path", () => {
    render(<ReviewCard review={REVIEW} onOpen={() => {}} />);
    expect(screen.getByText("Customer Acme rough notes")).toBeInTheDocument();
    expect(screen.getByText(/A tiny excerpt/)).toBeInTheDocument();
    expect(screen.getByText("customers/acme-logistics")).toBeInTheDocument();
  });

  it("invokes onOpen when clicked", async () => {
    const onOpen = vi.fn();
    render(<ReviewCard review={REVIEW} onOpen={onOpen} />);
    await userEvent.setup().click(screen.getByRole("button"));
    expect(onOpen).toHaveBeenCalledWith("r1");
  });

  it("shows the active styling when marked active", () => {
    render(<ReviewCard review={REVIEW} active={true} onOpen={() => {}} />);
    expect(screen.getByRole("button").className).toContain("is-active");
  });
});
