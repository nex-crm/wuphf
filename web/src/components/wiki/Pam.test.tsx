import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { listPamActions, subscribePamEvents } from "../../api/pam";
import { drawKnownPixelAvatar } from "../../lib/pixelAvatar";
import Pam from "./Pam";

vi.mock("../../api/pam", () => ({
  listPamActions: vi.fn(),
  subscribePamEvents: vi.fn(),
}));

vi.mock("../../lib/pixelAvatar", () => ({
  drawKnownPixelAvatar: vi.fn(),
}));

vi.mock("../ui/PixelAvatar", () => ({
  PixelAvatar: ({
    slug,
    size,
    className,
  }: {
    slug: string;
    size: number;
    className?: string;
  }) => (
    <canvas
      className={["pixel-avatar", className].filter(Boolean).join(" ")}
      data-size={size}
      data-testid={`${slug}-pixel-avatar`}
    />
  ),
}));

describe("<Pam>", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.spyOn(Math, "random").mockReturnValue(0);
    vi.mocked(listPamActions).mockResolvedValue({ actions: [] });
    vi.mocked(subscribePamEvents).mockReturnValue(() => {});
  });

  afterEach(() => {
    vi.clearAllTimers();
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it("lets Jim periodically walk to Pam and trade wiki chatter", () => {
    render(<Pam articlePath="team/customers/acme.md" />);

    const visitor = screen.getByTestId("jim-pam-visitor");
    expect(visitor).toHaveClass("is-away");
    const jim = screen.getByTestId("jim-full-body-sprite");
    expect(jim).toHaveClass("pixel-avatar", "jim-pixel");
    expect(drawKnownPixelAvatar).toHaveBeenCalledWith(jim, "hybridJim", 34);

    act(() => {
      vi.advanceTimersByTime(5000);
    });
    expect(visitor).toHaveClass("is-walking-in");

    act(() => {
      vi.advanceTimersByTime(2600);
    });
    expect(visitor).toHaveClass("is-chatting");
    expect(
      screen.getByText("Did you hear? CEO merged 12 PRs. Didn't ask anyone."),
    ).toBeInTheDocument();

    act(() => {
      vi.advanceTimersByTime(3000);
    });
    expect(
      screen.getByText("Twelve? I'd better update the wiki."),
    ).toBeInTheDocument();
    // Tone regression guard (ten-out-of-ten E1): the sarcastic filler line
    // must never come back on a work surface (ICP-eval v3 [17:41:35]).
    expect(
      screen.queryByText("Of course it didn't. Classic."),
    ).not.toBeInTheDocument();
  });
});
