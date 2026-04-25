import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { HelpModal } from "./HelpModal";

// rAF is used to defer the close-button focus until after React commits.
// happy-dom supports it, but run once per tick so the effect's rAF
// callback fires before we assert.
async function flushRAF() {
  await new Promise<void>((resolve) => {
    requestAnimationFrame(() => resolve());
  });
}

afterEach(() => {
  while (document.body.firstChild) {
    document.body.removeChild(document.body.firstChild);
  }
});

beforeEach(() => {
  // Clear any stray focus
  (document.activeElement as HTMLElement | null)?.blur?.();
});

describe("<HelpModal>", () => {
  it("renders nothing when open=false", () => {
    const { container } = render(<HelpModal open={false} onClose={vi.fn()} />);
    expect(container.querySelector(".help-modal")).toBeNull();
  });

  it("renders the three new sections when open", () => {
    render(<HelpModal open={true} onClose={vi.fn()} />);
    expect(screen.getByText("Global")).toBeInTheDocument();
    expect(screen.getByText("Command palette")).toBeInTheDocument();
    expect(screen.getByText("Onboarding wizard")).toBeInTheDocument();
  });

  it('applies aria-modal="true" and role="dialog"', () => {
    const { container } = render(<HelpModal open={true} onClose={vi.fn()} />);
    const dialog = container.querySelector('[role="dialog"]');
    expect(dialog).not.toBeNull();
    expect(dialog?.getAttribute("aria-modal")).toBe("true");
  });

  it("focuses the close button on open", async () => {
    render(<HelpModal open={true} onClose={vi.fn()} />);
    await flushRAF();
    await waitFor(() => {
      expect(document.activeElement?.getAttribute("aria-label")).toBe(
        "Close help",
      );
    });
  });

  it("restores focus to the previously-focused element on close", async () => {
    const trigger = document.createElement("button");
    trigger.textContent = "trigger";
    document.body.appendChild(trigger);
    trigger.focus();
    expect(document.activeElement).toBe(trigger);

    const { rerender } = render(<HelpModal open={true} onClose={vi.fn()} />);
    await flushRAF();
    // Modal opened — close button now focused.
    expect(document.activeElement?.getAttribute("aria-label")).toBe(
      "Close help",
    );

    // Close the modal — cleanup should send focus back to `trigger`.
    rerender(<HelpModal open={false} onClose={vi.fn()} />);
    await waitFor(() => {
      expect(document.activeElement).toBe(trigger);
    });
  });

  it("does not throw when the previously-focused element was unmounted while open", async () => {
    const trigger = document.createElement("button");
    document.body.appendChild(trigger);
    trigger.focus();

    const { rerender } = render(<HelpModal open={true} onClose={vi.fn()} />);
    await flushRAF();
    // Unmount the trigger while the modal is open.
    document.body.removeChild(trigger);
    expect(trigger.isConnected).toBe(false);

    // Closing the modal must not throw even though the prior focus target
    // is detached. The isConnected guard is the whole point.
    expect(() => {
      rerender(<HelpModal open={false} onClose={vi.fn()} />);
    }).not.toThrow();
  });

  it("calls onClose when Escape is pressed", async () => {
    const onClose = vi.fn();
    render(<HelpModal open={true} onClose={onClose} />);
    await flushRAF();
    fireEvent.keyDown(document, { key: "Escape" });
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("calls onClose when the overlay backdrop (not the modal body) is clicked", () => {
    const onClose = vi.fn();
    const { container } = render(<HelpModal open={true} onClose={onClose} />);
    const overlay = container.querySelector(".help-overlay") as HTMLElement;
    // Click the overlay itself — target === currentTarget triggers close.
    fireEvent.click(overlay, { target: overlay, currentTarget: overlay });
    expect(onClose).toHaveBeenCalled();
  });

  it("calls onClose when the explicit Esc button is clicked", () => {
    const onClose = vi.fn();
    render(<HelpModal open={true} onClose={onClose} />);
    const closeBtn = screen.getByLabelText("Close help");
    fireEvent.click(closeBtn);
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
