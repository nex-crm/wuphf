import { fireEvent, render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { RoutinesTab } from "./RoutinesTab";

describe("RoutinesTab", () => {
  it("lists routines as scheduled prompts with per-routine lifecycle", () => {
    const { getByText, getAllByText } = render(
      <RoutinesTab agentName="Pipeline Agent" />,
    );
    expect(getByText("Monday pipeline recap")).toBeTruthy();
    expect(getAllByText("Every Monday 9:00").length).toBeGreaterThan(0);
    // Disable / Publish belong to EACH routine, not the agent.
    expect(getAllByText("Publish new version").length).toBe(3);
    expect(getByText("paused")).toBeTruthy(); // the seeded disabled routine
  });

  it("disables one routine without touching the others", () => {
    const { getAllByText } = render(<RoutinesTab agentName="Pipeline Agent" />);
    const disables = getAllByText("Disable");
    expect(disables.length).toBe(2); // two enabled seeds
    fireEvent.click(disables[0]);
    expect(getAllByText("Disable").length).toBe(1);
    expect(getAllByText("Enable").length).toBe(2);
  });

  it("editing a prompt marks a draft; Publish freezes it as the next version", () => {
    const { getAllByLabelText, getAllByText, getByText } = render(
      <RoutinesTab agentName="Pipeline Agent" />,
    );
    const prompt = getAllByLabelText(/Prompt for/)[0] as HTMLTextAreaElement;
    fireEvent.change(prompt, { target: { value: "New sharper prompt" } });
    expect(getByText(/v3 · draft/)).toBeTruthy();
    const publish = getAllByText("Publish new version")[0];
    fireEvent.click(publish);
    expect(getByText(/v4$/)).toBeTruthy();
  });

  it("adds a new routine from a prompt + schedule", () => {
    const { getByLabelText, getByText, getAllByText } = render(
      <RoutinesTab agentName="Pipeline Agent" />,
    );
    fireEvent.change(getByLabelText("Routine prompt"), {
      target: { value: "Email me anything stuck in legal" },
    });
    fireEvent.click(getByText("Add routine"));
    // The new routine renders (name span + editable prompt).
    expect(
      getAllByText("Email me anything stuck in legal").length,
    ).toBeGreaterThan(0);
  });

  it("opens the routine's chat session", () => {
    const onOpenSession = vi.fn();
    const { getAllByText } = render(
      <RoutinesTab agentName="Pipeline Agent" onOpenSession={onOpenSession} />,
    );
    fireEvent.click(getAllByText("Open its chat")[0]);
    expect(onOpenSession).toHaveBeenCalledWith(
      "sess_recap",
      "Monday pipeline recap",
    );
  });
});
