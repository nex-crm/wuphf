// @vitest-environment happy-dom

import type { ThreadView } from "@wuphf/protocol/browser";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ThreadCard, __testing__ } from "../../../src/renderer/work-board/ThreadCard.tsx";

import { sampleThreadView } from "./fixtures.ts";

describe("ThreadCard", () => {
  it("renders title, effective status badge, ULID, and seat icon", () => {
    const thread = sampleThreadView();
    render(<ThreadCard thread={thread} />);

    expect(screen.getByText(thread.title)).toBeInTheDocument();
    expect(screen.getByTestId("thread-card-status-badge")).toHaveTextContent(
      __testing__.effectiveStatusLabel(thread.effectiveStatus),
    );
    expect(screen.getByText(thread.id)).toBeInTheDocument();
    expect(screen.getByText("Agent")).toBeInTheDocument();
  });

  it("renders 'You' when the current seat is human", () => {
    render(
      <ThreadCard thread={sampleThreadView({ currentSeat: "human" })} />,
    );
    expect(screen.getByText("You")).toBeInTheDocument();
  });

  it("renders pending approval pill only when count > 0", () => {
    const { rerender } = render(
      <ThreadCard thread={sampleThreadView({ pendingApprovalCount: 0 })} />,
    );
    expect(screen.queryByText(/pending/i)).not.toBeInTheDocument();

    rerender(<ThreadCard thread={sampleThreadView({ pendingApprovalCount: 3 })} />);
    expect(screen.getByText("3 pending")).toBeInTheDocument();
  });

  it("renders the attention reason pill when present", () => {
    render(
      <ThreadCard
        thread={sampleThreadView({
          effectiveStatus: "needs_attention",
          attentionReason: "stalled",
        })}
      />,
    );
    expect(screen.getByText("Stalled")).toBeInTheDocument();
  });

  it("falls back to 'Untitled thread' when title is empty", () => {
    render(<ThreadCard thread={sampleThreadView({ title: "" })} />);
    expect(screen.getByText("Untitled thread")).toBeInTheDocument();
  });

  it("renders as a non-interactive article when no onSelect handler is supplied", () => {
    render(<ThreadCard thread={sampleThreadView()} />);
    const card = screen.getByTestId("thread-card");
    expect(card.tagName).toBe("ARTICLE");
  });

  it("renders as a button and invokes onSelect when clicked", () => {
    const onSelect = vi.fn<(thread: ThreadView) => void>();
    const thread = sampleThreadView();
    render(<ThreadCard thread={thread} onSelect={onSelect} />);

    const card = screen.getByTestId("thread-card");
    expect(card.tagName).toBe("BUTTON");
    expect(card).toHaveAttribute("type", "button");
    fireEvent.click(card);
    expect(onSelect).toHaveBeenCalledTimes(1);
    expect(onSelect).toHaveBeenCalledWith(thread);
  });
});

describe("ThreadCard tone + label maps", () => {
  it("maps every ThreadEffectiveStatus value to a tone and a label", () => {
    const statuses = [
      "open",
      "in_progress",
      "needs_review",
      "needs_attention",
      "merged",
      "closed",
    ] as const;
    const tones = new Set<string>();
    for (const status of statuses) {
      // Both must return defined non-empty values; no fallthrough.
      const label = __testing__.effectiveStatusLabel(status);
      const tone = __testing__.effectiveStatusTone(status);
      expect(label).not.toBe("");
      tones.add(tone);
    }
    // At least two tones are exercised so we know the mapping is not a
    // single-tone fallback.
    expect(tones.size).toBeGreaterThan(1);
  });

  it("maps every ThreadAttentionReason value to a label", () => {
    const reasons = ["pending_approval", "failed", "stalled"] as const;
    for (const reason of reasons) {
      expect(__testing__.attentionReasonLabel(reason)).not.toBe("");
    }
  });
});
