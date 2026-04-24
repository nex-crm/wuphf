import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import ByLineStrip from "./ByLineStrip";

describe("<ByLineStrip>", () => {
  const baseProps = {
    authorSlug: "pm",
    lastEditedTs: new Date(Date.now() - 120_000).toISOString(),
    revisions: 3,
    reviewerSlug: "ceo",
  };

  it("renders author name and draft pill for a draft entry", () => {
    render(<ByLineStrip {...baseProps} status="draft" />);
    expect(screen.getByText("PM")).toBeInTheDocument();
    expect(screen.getByText("Draft")).toBeInTheDocument();
    expect(screen.getByText("Not yet reviewed")).toBeInTheDocument();
  });

  it("announces draft state in aria-label for screen readers", () => {
    render(<ByLineStrip {...baseProps} status="draft" />);
    expect(
      screen.getByLabelText(/Entry by PM.*Draft, not yet reviewed/),
    ).toBeInTheDocument();
  });

  it("shows the review pill and reviewer when status is in-review", () => {
    render(<ByLineStrip {...baseProps} status="in-review" />);
    expect(screen.getByText("In review")).toBeInTheDocument();
    expect(screen.getByText(/Reviewing: CEO/)).toBeInTheDocument();
  });

  it("shows the Promoted pill once the entry lands", () => {
    render(<ByLineStrip {...baseProps} status="promoted" />);
    // Label appears in both the pill and the right-aligned meta; both are OK.
    expect(screen.getAllByText("Promoted").length).toBeGreaterThanOrEqual(1);
  });

  it("uses singular revision when revisions=1", () => {
    render(<ByLineStrip {...baseProps} status="draft" revisions={1} />);
    expect(screen.getByText(/1 revision(?!s)/)).toBeInTheDocument();
  });
});
