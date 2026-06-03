import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import {
  OFFICE_TOUR_DONE_KEY,
  OFFICE_TOUR_SHOW_EVENT,
  requestShowOfficeTour,
  useOfficeTour,
} from "./useOfficeTour";

// Each test starts from a clean browser: no "done" flag persisted, so the
// auto-open gate behaves as it would on a genuinely first run. localStorage is
// the in-memory polyfill installed in tests/setup.ts.
beforeEach(() => {
  window.localStorage.clear();
});

afterEach(() => {
  window.localStorage.clear();
});

describe("useOfficeTour auto-open", () => {
  it("auto-opens once when enabled and the browser has not seen the tour", () => {
    const { result } = renderHook(() => useOfficeTour(true));
    expect(result.current.open).toBe(true);
    // First-run auto-open is NOT a replay: the caller renders it as the final
    // act of onboarding (no office Shell behind it).
    expect(result.current.replay).toBe(false);
  });

  it("stays closed while disabled, then auto-opens once enabled flips true", () => {
    const { result, rerender } = renderHook(
      ({ enabled }: { enabled: boolean }) => useOfficeTour(enabled),
      { initialProps: { enabled: false } },
    );

    expect(result.current.open).toBe(false);

    rerender({ enabled: true });
    expect(result.current.open).toBe(true);
  });

  it("does NOT auto-open when the done flag is already persisted", () => {
    window.localStorage.setItem(OFFICE_TOUR_DONE_KEY, "1");

    const { result } = renderHook(() => useOfficeTour(true));
    expect(result.current.open).toBe(false);
  });

  it("does not re-auto-open after markDone persists, even on remount", () => {
    const first = renderHook(() => useOfficeTour(true));
    expect(first.result.current.open).toBe(true);

    act(() => {
      first.result.current.markDone();
    });
    expect(first.result.current.open).toBe(false);
    expect(window.localStorage.getItem(OFFICE_TOUR_DONE_KEY)).toBe("1");

    // A fresh mount (simulating a reload) sees the persisted flag and stays
    // closed: the one-shot auto-open does not fire twice per browser.
    const second = renderHook(() => useOfficeTour(true));
    expect(second.result.current.open).toBe(false);
  });
});

describe("useOfficeTour persistence", () => {
  it("markDone persists the done flag and closes the tour", () => {
    const { result } = renderHook(() => useOfficeTour(true));
    expect(result.current.open).toBe(true);

    act(() => {
      result.current.markDone();
    });

    expect(result.current.open).toBe(false);
    expect(window.localStorage.getItem(OFFICE_TOUR_DONE_KEY)).toBe("1");
  });

  it("skip is an alias for markDone: closes and persists done", () => {
    const { result } = renderHook(() => useOfficeTour(true));
    expect(result.current.open).toBe(true);

    act(() => {
      result.current.skip();
    });

    expect(result.current.open).toBe(false);
    expect(window.localStorage.getItem(OFFICE_TOUR_DONE_KEY)).toBe("1");
  });
});

describe("useOfficeTour replay", () => {
  it("re-opens when the wuphf:show-office-tour event fires", () => {
    // Already seen, so the tour starts closed.
    window.localStorage.setItem(OFFICE_TOUR_DONE_KEY, "1");
    const { result } = renderHook(() => useOfficeTour(true));
    expect(result.current.open).toBe(false);

    act(() => {
      window.dispatchEvent(new CustomEvent(OFFICE_TOUR_SHOW_EVENT));
    });

    expect(result.current.open).toBe(true);
    // Reopening from the event is a replay: the caller overlays it on the
    // already-mounted office Shell rather than replacing the office.
    expect(result.current.replay).toBe(true);
  });

  it("requestShowOfficeTour() dispatches the replay event and re-opens the tour", () => {
    window.localStorage.setItem(OFFICE_TOUR_DONE_KEY, "1");
    const { result } = renderHook(() => useOfficeTour(true));
    expect(result.current.open).toBe(false);

    act(() => {
      requestShowOfficeTour();
    });

    expect(result.current.open).toBe(true);
  });

  it("replay does NOT clear the done flag, so it stays a one-shot auto-open", () => {
    window.localStorage.setItem(OFFICE_TOUR_DONE_KEY, "1");
    const { result } = renderHook(() => useOfficeTour(true));

    act(() => {
      requestShowOfficeTour();
    });
    expect(result.current.open).toBe(true);
    expect(window.localStorage.getItem(OFFICE_TOUR_DONE_KEY)).toBe("1");
  });

  it("show() forces the tour open without touching the done flag", () => {
    window.localStorage.setItem(OFFICE_TOUR_DONE_KEY, "1");
    const { result } = renderHook(() => useOfficeTour(true));
    expect(result.current.open).toBe(false);

    act(() => {
      result.current.show();
    });

    expect(result.current.open).toBe(true);
    expect(window.localStorage.getItem(OFFICE_TOUR_DONE_KEY)).toBe("1");
  });

  it("removes the replay listener on unmount (no leak after teardown)", () => {
    window.localStorage.setItem(OFFICE_TOUR_DONE_KEY, "1");
    const { result, unmount } = renderHook(() => useOfficeTour(true));

    unmount();

    // Firing the event after unmount must not throw and must not resurrect a
    // detached hook. We assert no error and that the last observed state is
    // still closed.
    act(() => {
      window.dispatchEvent(new CustomEvent(OFFICE_TOUR_SHOW_EVENT));
    });
    expect(result.current.open).toBe(false);
  });
});
