import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { computePillState, startEventTimer } from "./agentEventTimer";

describe("computePillState", () => {
  const NOW = 1_000_000;

  it("returns 'stuck' when kind is stuck (overrides everything else)", () => {
    expect(
      computePillState({
        lastEventMs: NOW - 5000,
        nowMs: NOW,
        kind: "stuck",
        haloUntilMs: NOW + 600,
      }),
    ).toBe("stuck");
  });

  it("returns 'halo' when nowMs < haloUntilMs", () => {
    expect(
      computePillState({
        lastEventMs: NOW - 100,
        nowMs: NOW,
        kind: "routine",
        haloUntilMs: NOW + 500,
      }),
    ).toBe("halo");
  });

  it("returns 'holding' for routine within 60s of last event", () => {
    expect(
      computePillState({
        lastEventMs: NOW - 30_000,
        nowMs: NOW,
        kind: "routine",
      }),
    ).toBe("holding");
    expect(
      computePillState({
        lastEventMs: NOW - 60_000,
        nowMs: NOW,
        kind: "routine",
      }),
    ).toBe("holding");
  });

  it("milestone extends hold to 120s", () => {
    // routine would dim at 60s+, milestone still holds.
    expect(
      computePillState({
        lastEventMs: NOW - 90_000,
        nowMs: NOW,
        kind: "milestone",
      }),
    ).toBe("holding");
    expect(
      computePillState({
        lastEventMs: NOW - 120_000,
        nowMs: NOW,
        kind: "milestone",
      }),
    ).toBe("holding");
    // contrast: routine at 90s is dim
    expect(
      computePillState({
        lastEventMs: NOW - 90_000,
        nowMs: NOW,
        kind: "routine",
      }),
    ).toBe("dim");
  });

  it("returns 'dim' between hold expiry and 120s", () => {
    expect(
      computePillState({
        lastEventMs: NOW - 61_000,
        nowMs: NOW,
        kind: "routine",
      }),
    ).toBe("dim");
    expect(
      computePillState({
        lastEventMs: NOW - 119_000,
        nowMs: NOW,
        kind: "routine",
      }),
    ).toBe("dim");
  });

  it("returns 'idle' after 120s with no event", () => {
    expect(
      computePillState({
        lastEventMs: NOW - 121_000,
        nowMs: NOW,
        kind: "routine",
      }),
    ).toBe("idle");
    expect(
      computePillState({
        lastEventMs: NOW - 999_999,
        nowMs: NOW,
        kind: undefined,
      }),
    ).toBe("idle");
  });

  it("undefined kind treated as routine for hold window", () => {
    expect(
      computePillState({
        lastEventMs: NOW - 30_000,
        nowMs: NOW,
        kind: undefined,
      }),
    ).toBe("holding");
  });
});

describe("startEventTimer", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("fires callback roughly every 1000ms", () => {
    const callback = vi.fn();
    const cleanup = startEventTimer(callback);

    expect(callback).toHaveBeenCalledTimes(0);

    vi.advanceTimersByTime(1000);
    expect(callback).toHaveBeenCalledTimes(1);

    vi.advanceTimersByTime(3000);
    expect(callback).toHaveBeenCalledTimes(4);

    cleanup();
  });

  it("passes Date.now() to the callback", () => {
    const callback = vi.fn();
    vi.setSystemTime(new Date("2026-05-05T12:00:00Z"));
    const cleanup = startEventTimer(callback);

    vi.advanceTimersByTime(1000);

    expect(callback).toHaveBeenCalledTimes(1);
    expect(callback).toHaveBeenCalledWith(Date.now());

    cleanup();
  });

  // CRITICAL CLEANUP TEST — flagged as the test plan's regression risk.
  // Without working cleanup, the 1Hz tick continues forever after AgentList
  // unmounts (dev hot-reload, route changes, multi-tab) and silently re-renders
  // unmounted trees.
  it("CRITICAL: cleanup stops the interval (no further callbacks after cleanup)", () => {
    const callback = vi.fn();
    const cleanup = startEventTimer(callback);

    vi.advanceTimersByTime(2000);
    expect(callback).toHaveBeenCalledTimes(2);

    cleanup();
    const callsAtCleanup = callback.mock.calls.length;

    vi.advanceTimersByTime(5000);

    expect(callback.mock.calls.length - callsAtCleanup).toBe(0);
  });
});
