import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import PromotedBackCallout from "./PromotedBackCallout";

const LINK = {
  section: "onboarding gotchas",
  promoted_to_path: "playbooks/customer-onboarding",
  promoted_by_slug: "ceo",
  promoted_ts: new Date(Date.now() - 60 * 60 * 1000).toISOString(),
};

describe("<PromotedBackCallout>", () => {
  it("describes what was promoted and to where", () => {
    render(<PromotedBackCallout link={LINK} />);
    expect(screen.getByText("onboarding gotchas")).toBeInTheDocument();
    expect(
      screen.getByText(/Team Wiki · playbooks\/customer-onboarding/),
    ).toBeInTheDocument();
    expect(screen.getByText(/by CEO/)).toBeInTheDocument();
  });

  it("invokes onNavigate when the wiki link is clicked", async () => {
    const onNavigate = vi.fn();
    render(<PromotedBackCallout link={LINK} onNavigate={onNavigate} />);
    await userEvent.setup().click(screen.getByText(/Team Wiki · playbooks/));
    expect(onNavigate).toHaveBeenCalledWith("playbooks/customer-onboarding");
  });

  it("exposes a role=note for assistive tech", () => {
    render(<PromotedBackCallout link={LINK} />);
    expect(screen.getByRole("note")).toBeInTheDocument();
  });
});
