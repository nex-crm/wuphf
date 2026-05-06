import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { useAppStore } from "../stores/app";
import {
  __internal,
  useAgentEventPeek,
  usePeekIsOpen,
} from "./useAgentEventPeek";

const { usePeekOpenStore } = __internal;

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function resetStores() {
  usePeekOpenStore.setState({ openSlug: null });
  useAppStore.setState({
    agentActivitySnapshots: {},
    agentActivityHistory: {},
  });
}

// Options that make timers instant-ish for determinism without real delays.
const FAST: Parameters<typeof useAgentEventPeek>[1] = {
  hoverIntentMs: 300,
  closeGraceMs: 80,
  longPressMs: 500,
};

beforeEach(() => {
  vi.useFakeTimers();
  resetStores();
});

afterEach(() => {
  vi.useRealTimers();
  resetStores();
});

// ---------------------------------------------------------------------------
// Initial state
// ---------------------------------------------------------------------------

describe("initial state", () => {
  it("isOpen is false before any interaction", () => {
    const { result } = renderHook(() => useAgentEventPeek("tess", FAST));
    expect(result.current.isOpen).toBe(false);
  });

  it("current is undefined and history is empty when no snapshot exists", () => {
    const { result } = renderHook(() => useAgentEventPeek("tess", FAST));
    expect(result.current.current).toBeUndefined();
    expect(result.current.history).toEqual([]);
  });
});

// ---------------------------------------------------------------------------
// Hover-intent open
// ---------------------------------------------------------------------------

describe("hover-intent open", () => {
  it("does NOT open before 300ms", () => {
    const { result } = renderHook(() => useAgentEventPeek("tess", FAST));
    act(() => {
      result.current.hoverHandlers.onMouseEnter();
    });
    act(() => {
      vi.advanceTimersByTime(299);
    });
    expect(result.current.isOpen).toBe(false);
  });

  it("opens after exactly 300ms hover", () => {
    const { result } = renderHook(() => useAgentEventPeek("tess", FAST));
    act(() => {
      result.current.hoverHandlers.onMouseEnter();
    });
    act(() => {
      vi.advanceTimersByTime(300);
    });
    expect(result.current.isOpen).toBe(true);
  });

  it("stays closed when cursor leaves before 300ms", () => {
    const { result } = renderHook(() => useAgentEventPeek("tess", FAST));
    act(() => {
      result.current.hoverHandlers.onMouseEnter();
    });
    act(() => {
      vi.advanceTimersByTime(200);
    });
    act(() => {
      result.current.hoverHandlers.onMouseLeave();
    });
    act(() => {
      vi.advanceTimersByTime(200);
    });
    expect(result.current.isOpen).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// Close-grace
// ---------------------------------------------------------------------------

describe("close-grace on leave", () => {
  it("closes after leave + 80ms", () => {
    const { result } = renderHook(() => useAgentEventPeek("tess", FAST));
    // Open via hover-intent.
    act(() => {
      result.current.hoverHandlers.onMouseEnter();
      vi.advanceTimersByTime(300);
    });
    expect(result.current.isOpen).toBe(true);

    act(() => {
      result.current.hoverHandlers.onMouseLeave();
    });
    act(() => {
      vi.advanceTimersByTime(80);
    });
    expect(result.current.isOpen).toBe(false);
  });

  it("stays open when cursor re-enters within the 80ms close-grace window", () => {
    const { result } = renderHook(() => useAgentEventPeek("tess", FAST));
    act(() => {
      result.current.hoverHandlers.onMouseEnter();
      vi.advanceTimersByTime(300);
    });
    expect(result.current.isOpen).toBe(true);

    act(() => {
      result.current.hoverHandlers.onMouseLeave();
    });
    act(() => {
      vi.advanceTimersByTime(40);
    });
    // Cursor returns before grace expires.
    act(() => {
      result.current.hoverHandlers.onMouseEnter();
    });
    act(() => {
      vi.advanceTimersByTime(80);
    });
    expect(result.current.isOpen).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// Toggle / open / close (keyboard-style, no timers)
// ---------------------------------------------------------------------------

describe("toggle, open, close", () => {
  it("toggle flips false -> true instantly", () => {
    const { result } = renderHook(() => useAgentEventPeek("tess", FAST));
    act(() => {
      result.current.toggle();
    });
    expect(result.current.isOpen).toBe(true);
  });

  it("toggle flips true -> false instantly", () => {
    const { result } = renderHook(() => useAgentEventPeek("tess", FAST));
    act(() => {
      result.current.toggle();
    });
    act(() => {
      result.current.toggle();
    });
    expect(result.current.isOpen).toBe(false);
  });

  it("open() sets isOpen to true", () => {
    const { result } = renderHook(() => useAgentEventPeek("tess", FAST));
    act(() => {
      result.current.open();
    });
    expect(result.current.isOpen).toBe(true);
  });

  it("close() sets isOpen to false after open", () => {
    const { result } = renderHook(() => useAgentEventPeek("tess", FAST));
    act(() => {
      result.current.open();
    });
    act(() => {
      result.current.close();
    });
    expect(result.current.isOpen).toBe(false);
  });

  it("toggle does not involve any timers — no pending callbacks after flip", () => {
    const { result } = renderHook(() => useAgentEventPeek("tess", FAST));
    act(() => {
      result.current.toggle();
    });
    // Advancing time should not change state (no close-grace triggered).
    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(result.current.isOpen).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// Long-press (touch)
// ---------------------------------------------------------------------------

describe("long-press touch", () => {
  it("opens after 500ms touchstart without move or end", () => {
    const { result } = renderHook(() => useAgentEventPeek("ava", FAST));
    act(() => {
      result.current.longPressHandlers.onTouchStart();
    });
    act(() => {
      vi.advanceTimersByTime(500);
    });
    expect(result.current.isOpen).toBe(true);
  });

  it("does not open when touchend fires before 500ms", () => {
    const { result } = renderHook(() => useAgentEventPeek("ava", FAST));
    act(() => {
      result.current.longPressHandlers.onTouchStart();
    });
    act(() => {
      vi.advanceTimersByTime(400);
    });
    act(() => {
      result.current.longPressHandlers.onTouchEnd();
    });
    act(() => {
      vi.advanceTimersByTime(200);
    });
    expect(result.current.isOpen).toBe(false);
  });

  it("does not open when touchcancel fires before 500ms", () => {
    const { result } = renderHook(() => useAgentEventPeek("ava", FAST));
    act(() => {
      result.current.longPressHandlers.onTouchStart();
    });
    act(() => {
      vi.advanceTimersByTime(300);
    });
    act(() => {
      result.current.longPressHandlers.onTouchCancel();
    });
    act(() => {
      vi.advanceTimersByTime(300);
    });
    expect(result.current.isOpen).toBe(false);
  });

  it("touchmove during press cancels the long-press timer", () => {
    const { result } = renderHook(() => useAgentEventPeek("ava", FAST));
    act(() => {
      result.current.longPressHandlers.onTouchStart();
    });
    act(() => {
      vi.advanceTimersByTime(200);
    });
    act(() => {
      result.current.longPressHandlers.onTouchMove();
    });
    act(() => {
      vi.advanceTimersByTime(400);
    });
    expect(result.current.isOpen).toBe(false);
  });

  it("touchend after open does NOT close the peek (touch dismiss by tap-outside)", () => {
    const { result } = renderHook(() => useAgentEventPeek("ava", FAST));
    act(() => {
      result.current.longPressHandlers.onTouchStart();
      vi.advanceTimersByTime(500);
    });
    expect(result.current.isOpen).toBe(true);

    act(() => {
      result.current.longPressHandlers.onTouchEnd();
    });
    expect(result.current.isOpen).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// Single-instance discipline
// ---------------------------------------------------------------------------

describe("single-instance: opening one slug closes another", () => {
  it("opening slug B leaves slug A closed", () => {
    const hookA = renderHook(() => useAgentEventPeek("tess", FAST));
    const hookB = renderHook(() => useAgentEventPeek("ava", FAST));

    act(() => {
      hookA.result.current.open();
    });
    expect(hookA.result.current.isOpen).toBe(true);
    expect(hookB.result.current.isOpen).toBe(false);

    act(() => {
      hookB.result.current.open();
    });
    expect(hookA.result.current.isOpen).toBe(false);
    expect(hookB.result.current.isOpen).toBe(true);
  });

  it("hover-intent on B clears A's open state", () => {
    const hookA = renderHook(() => useAgentEventPeek("tess", FAST));
    const hookB = renderHook(() => useAgentEventPeek("ava", FAST));

    act(() => {
      hookA.result.current.open();
    });
    expect(hookA.result.current.isOpen).toBe(true);

    act(() => {
      hookB.result.current.hoverHandlers.onMouseEnter();
      vi.advanceTimersByTime(300);
    });
    expect(hookA.result.current.isOpen).toBe(false);
    expect(hookB.result.current.isOpen).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// Unmount cleanup
// ---------------------------------------------------------------------------

describe("unmount cleanup", () => {
  it("cancels pending hover-open timer on unmount without error", () => {
    const { result, unmount } = renderHook(() =>
      useAgentEventPeek("tess", FAST),
    );
    act(() => {
      result.current.hoverHandlers.onMouseEnter();
    });
    // Unmount before the 300ms fires.
    unmount();
    // Advancing time after unmount must not throw or open the peek.
    act(() => {
      vi.advanceTimersByTime(400);
    });
    // No assertion on result (unmounted), just verify no errors were thrown.
    // The peek open store should remain untouched.
    expect(usePeekOpenStore.getState().openSlug).toBeNull();
  });

  it("cancels pending close-grace timer on unmount without error", () => {
    const { result, unmount } = renderHook(() =>
      useAgentEventPeek("tess", FAST),
    );
    act(() => {
      result.current.open();
      result.current.hoverHandlers.onMouseLeave();
    });
    unmount();
    act(() => {
      vi.advanceTimersByTime(200);
    });
    // The peek is open via direct open(); grace timer was set on leave.
    // After unmount and timer flush the store should not have been null-ified
    // by the grace timer callback (the timer was cancelled).
    expect(usePeekOpenStore.getState().openSlug).toBe("tess");
  });

  it("cancels pending long-press timer on unmount without error", () => {
    const { result, unmount } = renderHook(() =>
      useAgentEventPeek("ava", FAST),
    );
    act(() => {
      result.current.longPressHandlers.onTouchStart();
    });
    unmount();
    act(() => {
      vi.advanceTimersByTime(600);
    });
    expect(usePeekOpenStore.getState().openSlug).toBeNull();
  });
});

// ---------------------------------------------------------------------------
// usePeekIsOpen helper
// ---------------------------------------------------------------------------

describe("usePeekIsOpen", () => {
  it("returns false when nothing is open", () => {
    const { result } = renderHook(() => usePeekIsOpen("tess"));
    expect(result.current).toBe(false);
  });

  it("returns true for the open slug", () => {
    const { result } = renderHook(() => usePeekIsOpen("tess"));
    act(() => {
      usePeekOpenStore.getState().setOpenSlug("tess");
    });
    expect(result.current).toBe(true);
  });

  it("returns false for a slug that is NOT the open one", () => {
    const { result } = renderHook(() => usePeekIsOpen("ava"));
    act(() => {
      usePeekOpenStore.getState().setOpenSlug("tess");
    });
    expect(result.current).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// Snapshot data passthrough
// ---------------------------------------------------------------------------

describe("snapshot data from store", () => {
  it("exposes current snapshot when one exists for the slug", () => {
    const snap = {
      slug: "tess",
      activity: "running tests",
      receivedAtMs: Date.now(),
      haloUntilMs: Date.now() + 600,
    };
    useAppStore.setState({
      agentActivitySnapshots: { tess: snap },
    });

    const { result } = renderHook(() => useAgentEventPeek("tess", FAST));
    expect(result.current.current).toMatchObject({
      slug: "tess",
      activity: "running tests",
    });
  });

  it("exposes history array for the slug", () => {
    const snap1 = {
      slug: "tess",
      activity: "merging branch",
      receivedAtMs: Date.now() - 10_000,
      haloUntilMs: 0,
    };
    const snap2 = {
      slug: "tess",
      activity: "running tests",
      receivedAtMs: Date.now(),
      haloUntilMs: Date.now() + 600,
    };
    useAppStore.setState({
      agentActivitySnapshots: { tess: snap2 },
      agentActivityHistory: { tess: [snap1] },
    });

    const { result } = renderHook(() => useAgentEventPeek("tess", FAST));
    expect(result.current.history).toHaveLength(1);
    expect(result.current.history[0]).toMatchObject({
      activity: "merging branch",
    });
  });
});
