import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { POPULATED_PACKET } from "../../lib/mocks/decisionPackets";
import { PacketActionSidebar } from "./PacketActionSidebar";

describe("<PacketActionSidebar>", () => {
  function setup(isDecisionLocked = false) {
    const handlers = {
      onApprove: vi.fn(),
      onRequestChanges: vi.fn(),
      onDefer: vi.fn(),
      onBlock: vi.fn(),
      onOpenInWorktree: vi.fn(),
    };
    render(
      <PacketActionSidebar
        packet={POPULATED_PACKET}
        isDecisionLocked={isDecisionLocked}
        {...handlers}
      />,
    );
    return handlers;
  }

  it("renders all five action buttons in the locked hierarchy", () => {
    setup();
    expect(
      screen.getByRole("button", { name: /approve/i }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /request changes/i }),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /defer/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /block/i })).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /open in worktree/i }),
    ).toBeInTheDocument();
  });

  it("uses role=complementary so the sidebar reads as an aside landmark", () => {
    setup();
    const aside = screen.getByRole("complementary", {
      name: /decision actions/i,
    });
    expect(aside).toBeInTheDocument();
  });

  it("disables decision actions while the owner is still streaming", () => {
    setup(true);
    expect(screen.getByRole("button", { name: /approve/i })).toBeDisabled();
    expect(
      screen.getByRole("button", { name: /request changes/i }),
    ).toBeDisabled();
    expect(screen.getByRole("button", { name: /block/i })).toBeDisabled();
    // Open in worktree is the read-only action — never disabled.
    expect(
      screen.getByRole("button", { name: /open in worktree/i }),
    ).toBeEnabled();
  });

  it("invokes the approve handler when Approve is clicked", () => {
    const { onApprove } = setup();
    fireEvent.click(screen.getByRole("button", { name: /approve/i }));
    expect(onApprove).toHaveBeenCalledTimes(1);
  });
});
