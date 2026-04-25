import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import PromoteButton from "./PromoteButton";

describe("<PromoteButton>", () => {
  it("renders the primary action when not pending", () => {
    render(
      <PromoteButton reviewerSlug="ceo" pending={false} onPromote={() => {}} />,
    );
    expect(
      screen.getByRole("button", {
        name: /Submit this draft for review by CEO/,
      }),
    ).toBeInTheDocument();
  });

  it("renders pending pill and disables the button when pending", () => {
    render(
      <PromoteButton reviewerSlug="ceo" pending={true} onPromote={() => {}} />,
    );
    const btn = screen.getByRole("button", { name: /Pending review by CEO/ });
    expect(btn).toBeDisabled();
  });

  it("calls onPromote when clicked", async () => {
    const onPromote = vi.fn();
    render(
      <PromoteButton
        reviewerSlug="ceo"
        pending={false}
        onPromote={onPromote}
      />,
    );
    await userEvent.setup().click(screen.getByText("Promote to wiki →"));
    expect(onPromote).toHaveBeenCalled();
  });

  it("shows discard link only when a handler is provided", () => {
    const onDiscard = vi.fn();
    const { rerender } = render(
      <PromoteButton
        reviewerSlug="ceo"
        pending={false}
        onPromote={() => {}}
        onDiscard={onDiscard}
      />,
    );
    expect(screen.getByText("Discard entry")).toBeInTheDocument();

    rerender(
      <PromoteButton reviewerSlug="ceo" pending={false} onPromote={() => {}} />,
    );
    expect(screen.queryByText("Discard entry")).not.toBeInTheDocument();
  });

  it("hides discard link while pending", () => {
    render(
      <PromoteButton
        reviewerSlug="ceo"
        pending={true}
        onPromote={() => {}}
        onDiscard={() => {}}
      />,
    );
    expect(screen.queryByText("Discard entry")).not.toBeInTheDocument();
  });

  it('phrases "human reviewer" when reviewer is human-only', () => {
    render(
      <PromoteButton
        reviewerSlug="human-only"
        pending={false}
        onPromote={() => {}}
      />,
    );
    expect(
      screen.getByRole("button", {
        name: /Submit this draft for review by a human reviewer/,
      }),
    ).toBeInTheDocument();
  });
});
