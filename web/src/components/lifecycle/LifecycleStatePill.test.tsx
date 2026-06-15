import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { LifecycleState } from "../../lib/types/lifecycle";
import {
  isAwaitingStaffing,
  LifecycleStatePill,
  StaffingStatePill,
} from "./LifecycleStatePill";

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
    "blocked",
    "changes_requested",
    "approved",
  ])("renders without crashing for state=%s", (state) => {
    render(<LifecycleStatePill state={state} />);
    expect(screen.getByLabelText(/^state:/i)).toBeInTheDocument();
  });

  it("uses the locked label for blocked ('blocked')", () => {
    render(<LifecycleStatePill state="blocked" />);
    expect(screen.getByLabelText(/state: blocked/i)).toBeInTheDocument();
  });
});

// Regression (live smoke run gap #1): an ownerless "auto" task rendered
// "parked — Owner @auto", which read exactly like the removed approval
// wall. The header must say what is actually happening: the CEO is
// picking the owner.
describe("staffing state", () => {
  it("flags an auto-owned pre-run task as awaiting staffing", () => {
    expect(
      isAwaitingStaffing({ ownerSlug: "auto", lifecycleState: "drafting" }),
    ).toBe(true);
    expect(
      isAwaitingStaffing({ ownerSlug: "Auto ", lifecycleState: "intake" }),
    ).toBe(true);
    expect(
      isAwaitingStaffing({ ownerSlug: "auto", lifecycleState: "ready" }),
    ).toBe(true);
  });

  it("does NOT flag owned, unassigned, or running tasks", () => {
    expect(
      isAwaitingStaffing({ ownerSlug: "builder", lifecycleState: "drafting" }),
    ).toBe(false);
    expect(
      isAwaitingStaffing({ ownerSlug: undefined, lifecycleState: "drafting" }),
    ).toBe(false);
    expect(
      isAwaitingStaffing({ ownerSlug: "auto", lifecycleState: "running" }),
    ).toBe(false);
    expect(isAwaitingStaffing(undefined)).toBe(false);
  });

  it("renders the staffing pill with honest copy, never 'parked'", () => {
    render(<StaffingStatePill />);
    const pill = screen.getByTestId("staffing-state-pill");
    expect(pill.textContent).toMatch(/staffing/i);
    expect(pill.textContent).not.toMatch(/parked/i);
    expect(pill).toHaveAttribute("title", "The CEO is picking the owner");
  });
});
