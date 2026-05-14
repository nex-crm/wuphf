import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { POPULATED_PACKET } from "../../lib/mocks/decisionPackets";
import { DecisionPacketView } from "./DecisionPacketView";

function renderView(
  overrides: Partial<Parameters<typeof DecisionPacketView>[0]> = {},
) {
  const handlers = {
    onClose: vi.fn(),
    onMerge: vi.fn(),
    onRequestChanges: vi.fn(),
    onDefer: vi.fn(),
    onBlock: vi.fn(),
    onOpenInWorktree: vi.fn(),
  };
  render(
    <DecisionPacketView
      packet={POPULATED_PACKET}
      {...handlers}
      {...overrides}
    />,
  );
  return handlers;
}

describe("<DecisionPacketView>", () => {
  it("renders three landmark regions (navigation / main / complementary)", () => {
    renderView();
    expect(
      screen.getByRole("navigation", { name: /task context/i }),
    ).toBeInTheDocument();
    expect(screen.getByRole("main")).toBeInTheDocument();
    expect(
      screen.getByRole("complementary", { name: /decision actions/i }),
    ).toBeInTheDocument();
  });

  it("renders the Your call assignment block at the top of the center column", () => {
    renderView();
    expect(screen.getByText(/your call/i)).toBeInTheDocument();
    expect(
      screen.getByText(/Owner agent committed the refactor/i),
    ).toBeInTheDocument();
  });

  it("renders an AC checklist with completion %", () => {
    renderView();
    expect(
      screen.getByText(/5\/6 acceptance criteria done/i),
    ).toBeInTheDocument();
  });

  it("groups reviewer grades by severity in DOM order critical → skipped", () => {
    renderView();
    const grades = screen
      .getAllByLabelText(/grade from/i)
      .map((el) => el.getAttribute("data-severity"));
    expect(grades).toEqual(["critical", "major", "major", "skipped"]);
  });

  it("renders the reviewer-timeout banner when present", () => {
    renderView();
    expect(
      screen.getByText(/Slot filled with skipped placeholder/i),
    ).toBeInTheDocument();
  });

  it("fires onMerge when the user presses 'm' globally", () => {
    const handlers = renderView();
    fireEvent.keyDown(window, { key: "m" });
    expect(handlers.onMerge).toHaveBeenCalledTimes(1);
  });

  it("ignores keyboard shortcuts while streaming", () => {
    const handlers = renderView({ isStreaming: true });
    fireEvent.keyDown(window, { key: "m" });
    expect(handlers.onMerge).not.toHaveBeenCalled();
  });

  it("fires onClose on Escape regardless of streaming state", () => {
    const handlers = renderView({ isStreaming: true });
    fireEvent.keyDown(window, { key: "Escape" });
    expect(handlers.onClose).toHaveBeenCalledTimes(1);
  });

  it("renders the regenerated-from-memory banner when packet flagged", () => {
    renderView({
      packet: { ...POPULATED_PACKET, regeneratedFromMemory: true },
    });
    expect(
      screen.getByText(/Packet regenerated from in-memory state/i),
    ).toBeInTheDocument();
  });

  it("renders the persistence-error banner when prop set", () => {
    renderView({ hasPersistenceError: true });
    expect(
      screen.getByText(/Persistence error on this task/i),
    ).toBeInTheDocument();
  });
});
