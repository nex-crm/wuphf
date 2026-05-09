import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { ReviewerGrade } from "../../lib/types/lifecycle";
import { SeverityGradeCard } from "./SeverityGradeCard";

const baseGrade: ReviewerGrade = {
  reviewerSlug: "tess",
  severity: "critical",
  suggestion: "Watermark assumes monotonic event order",
  reasoning: "Stale events flip the state machine backward.",
  filePath: "internal/team/broker_actor.go",
  line: 182,
  submittedAt: "2026-05-09T21:58:00Z",
};

describe("<SeverityGradeCard>", () => {
  it("renders the suggestion, reasoning, file path and reviewer slug", () => {
    render(<SeverityGradeCard grade={baseGrade} />);
    expect(screen.getByText(/Watermark assumes/i)).toBeInTheDocument();
    expect(screen.getByText(/Stale events flip/i)).toBeInTheDocument();
    expect(
      screen.getByText("internal/team/broker_actor.go:182"),
    ).toBeInTheDocument();
    expect(screen.getByLabelText(/grade from tess/i)).toBeInTheDocument();
  });

  it("renders the severity tier label as text in addition to color", () => {
    render(<SeverityGradeCard grade={baseGrade} />);
    // Aria label carries the human tier; text inside the pill carries
    // the same so screen-reader + sighted users both see the tier.
    expect(
      screen.getByLabelText(/critical grade from tess/i),
    ).toBeInTheDocument();
    expect(screen.getByText(/^critical$/)).toBeInTheDocument();
  });

  it("hides the line when only filePath is set", () => {
    render(<SeverityGradeCard grade={{ ...baseGrade, line: undefined }} />);
    expect(
      screen.getByText("internal/team/broker_actor.go"),
    ).toBeInTheDocument();
  });

  it("renders a dash for the timestamp on a skipped grade", () => {
    const { container } = render(
      <SeverityGradeCard
        grade={{
          ...baseGrade,
          severity: "skipped",
          suggestion: "Reviewer timed out",
          reasoning: "Process did not submit grade within 10-minute window.",
          filePath: undefined,
          line: undefined,
        }}
      />,
    );
    expect(container.textContent).toContain("—");
    expect(screen.getByText(/^skipped$/)).toBeInTheDocument();
  });
});
