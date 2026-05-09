import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { LifecycleState } from "../../lib/types/lifecycle";
import { LifecycleStatePill } from "./LifecycleStatePill";

describe("<LifecycleStatePill>", () => {
  it("renders the state label and an aria-label so color is never the only signal", () => {
    render(<LifecycleStatePill state="decision" />);
    const pill = screen.getByLabelText(/state: decision/i);
    expect(pill).toBeInTheDocument();
    expect(pill.textContent).toContain("decision");
  });

  it.each<LifecycleState>([
    "intake",
    "ready",
    "running",
    "review",
    "decision",
    "blocked_on_pr_merge",
    "changes_requested",
    "merged",
  ])("renders without crashing for state=%s", (state) => {
    render(<LifecycleStatePill state={state} />);
    expect(screen.getByLabelText(/^state:/i)).toBeInTheDocument();
  });

  it("uses the locked label for blocked_on_pr_merge ('blocked')", () => {
    render(<LifecycleStatePill state="blocked_on_pr_merge" />);
    expect(screen.getByLabelText(/state: blocked/i)).toBeInTheDocument();
  });
});
