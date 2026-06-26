import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { GovernorStatus } from "../../api/governor";
import { GovernorBannerView } from "./GovernorBanner";
import { GovernorControlView } from "./GovernorControl";

function status(overrides: Partial<GovernorStatus> = {}): GovernorStatus {
  return {
    paused: true,
    reason: "budget",
    turnsSinceCheckpoint: 12,
    tokensSinceCheckpoint: 152_000,
    costSinceCheckpoint: 2.1,
    maxTokens: 150_000,
    maxCostUsd: 3,
    maxTurns: 12,
    disabled: false,
    ...overrides,
  };
}

describe("<GovernorBannerView>", () => {
  it("shows the budget reason and spend since checkpoint", () => {
    render(
      <GovernorBannerView
        status={status()}
        busy={false}
        onResume={vi.fn()}
        onResumeMore={vi.fn()}
        onStop={vi.fn()}
      />,
    );
    expect(screen.getByText("Budget checkpoint")).toBeTruthy();
    expect(screen.getByText(/152k tokens/)).toBeTruthy();
    expect(screen.getByText(/\$2\.10/)).toBeTruthy();
  });

  it("fires resume / resume_more / stop from the three buttons", () => {
    const onResume = vi.fn();
    const onResumeMore = vi.fn();
    const onStop = vi.fn();
    render(
      <GovernorBannerView
        status={status()}
        busy={false}
        onResume={onResume}
        onResumeMore={onResumeMore}
        onStop={onStop}
      />,
    );
    fireEvent.click(screen.getByText("Continue"));
    fireEvent.click(screen.getByText("Continue +budget"));
    fireEvent.click(screen.getByText("Stop"));
    expect(onResume).toHaveBeenCalledOnce();
    expect(onResumeMore).toHaveBeenCalledOnce();
    expect(onStop).toHaveBeenCalledOnce();
  });

  it("collapses to a single Resume action once stopped", () => {
    render(
      <GovernorBannerView
        status={status({ reason: "stop" })}
        busy={false}
        onResume={vi.fn()}
        onResumeMore={vi.fn()}
        onStop={vi.fn()}
      />,
    );
    expect(screen.getByText("Resume")).toBeTruthy();
    expect(screen.queryByText("Continue +budget")).toBeNull();
    expect(screen.queryByText("Stop")).toBeNull();
  });

  it("disables actions while a command is in flight", () => {
    render(
      <GovernorBannerView
        status={status()}
        busy={true}
        onResume={vi.fn()}
        onResumeMore={vi.fn()}
        onStop={vi.fn()}
      />,
    );
    expect(screen.getByText("Continue").closest("button")?.disabled).toBe(true);
  });
});

describe("<GovernorControlView>", () => {
  it("renders the live meter and fires pause / stop", () => {
    const onPause = vi.fn();
    const onStop = vi.fn();
    render(
      <GovernorControlView
        status={status({ paused: false, reason: "" })}
        busy={false}
        onPause={onPause}
        onStop={onStop}
      />,
    );
    expect(screen.getByText(/12 turns · 152k tok · \$2\.10/)).toBeTruthy();
    fireEvent.click(screen.getByText("Pause"));
    fireEvent.click(screen.getByText("Stop"));
    expect(onPause).toHaveBeenCalledOnce();
    expect(onStop).toHaveBeenCalledOnce();
  });
});
