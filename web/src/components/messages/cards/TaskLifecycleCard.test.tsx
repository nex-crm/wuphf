import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { TaskLifecycleCard } from "./TaskLifecycleCard";

const navigate = vi.fn();
vi.mock("../../../lib/router", () => ({
  router: { navigate: (...args: unknown[]) => navigate(...args) },
}));

const BASE = {
  task_id: "OFFICE-7",
  title: "Ship the migration",
  owner: "pam",
  from_state: "drafting",
  to_state: "in_progress",
  transition: "started" as const,
};

describe("TaskLifecycleCard", () => {
  afterEach(() => {
    cleanup();
    navigate.mockReset();
  });

  it("renders a clickable card with an Open CTA when pointing elsewhere", () => {
    render(<TaskLifecycleCard payload={BASE} />);
    const card = screen.getByTestId("issue-lifecycle-card");
    // Interactive variant is a <button> with the navigation affordance.
    expect(card.tagName).toBe("BUTTON");
    expect(card).not.toHaveAttribute("data-static");
    expect(card.textContent).toMatch(/Open →/);
    expect(card.textContent).toMatch(/drafting → in_progress/);

    fireEvent.click(card);
    expect(navigate).toHaveBeenCalledWith({
      to: "/tasks/$taskId",
      params: { taskId: "OFFICE-7" },
    });
  });

  it("renders an inert status row (no Open, not clickable) for the same task", () => {
    render(<TaskLifecycleCard payload={BASE} sameTask={true} />);
    const card = screen.getByTestId("issue-lifecycle-card");
    // Inert variant is a non-button element with no Open CTA.
    expect(card.tagName).not.toBe("BUTTON");
    expect(card).toHaveAttribute("data-static", "true");
    expect(card.className).toMatch(/issue-lifecycle-card--static/);
    expect(card.textContent).not.toMatch(/Open →/);
    // The transition itself is still surfaced as inline history.
    expect(card.textContent).toMatch(/drafting → in_progress/);

    fireEvent.click(card);
    expect(navigate).not.toHaveBeenCalled();
  });
});
