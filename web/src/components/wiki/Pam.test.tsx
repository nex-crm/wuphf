import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { listPamActions, subscribePamEvents } from "../../api/pam";
import Pam from "./Pam";

vi.mock("../../api/pam", () => ({
  listPamActions: vi.fn(),
  subscribePamEvents: vi.fn(),
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
    const jim = screen.getByTestId("jim-pixel-avatar");
    expect(jim).toHaveClass("pixel-avatar", "jim-pixel");
    expect(jim).toHaveAttribute("data-size", "46");

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
      screen.getByText("Of course it didn't. Classic."),
    ).toBeInTheDocument();
  });
});
