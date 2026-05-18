import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { useResizablePane } from "./useResizablePane";

const STORAGE_KEY = "test-pane-width";

const baseOpts = {
  storageKey: STORAGE_KEY,
  defaultWidth: 220,
  minWidth: 180,
  maxWidth: 420,
  edge: "right" as const,
};

let store: Map<string, string>;
beforeEach(() => {
  store = new Map();
  vi.stubGlobal("localStorage", {
    getItem: (k: string) => store.get(k) ?? null,
    setItem: (k: string, v: string) => {
      store.set(k, v);
    },
    removeItem: (k: string) => {
      store.delete(k);
    },
    clear: () => {
      store.clear();
    },
  });
});

afterEach(() => {
  vi.unstubAllGlobals();
  document.body.className = "";
});

describe("useResizablePane", () => {
  it("starts at the default width when nothing is stored", () => {
    const { result } = renderHook(() => useResizablePane(baseOpts));
    expect(result.current.width).toBe(220);
  });

  it("hydrates from localStorage when a value is present", () => {
    store.set(STORAGE_KEY, "300");
    const { result } = renderHook(() => useResizablePane(baseOpts));
    expect(result.current.width).toBe(300);
  });

  it("clamps a hydrated value above the max", () => {
    store.set(STORAGE_KEY, "9999");
    const { result } = renderHook(() => useResizablePane(baseOpts));
    expect(result.current.width).toBe(420);
  });

  it("clamps a hydrated value below the min", () => {
    store.set(STORAGE_KEY, "50");
    const { result } = renderHook(() => useResizablePane(baseOpts));
    expect(result.current.width).toBe(180);
  });

  it("ignores non-numeric stored values", () => {
    store.set(STORAGE_KEY, "junk");
    const { result } = renderHook(() => useResizablePane(baseOpts));
    expect(result.current.width).toBe(220);
  });

  it("persists width changes to localStorage", () => {
    const { result, rerender } = renderHook(() => useResizablePane(baseOpts));
    act(() => {
      result.current.reset();
    });
    rerender();
    expect(store.get(STORAGE_KEY)).toBe("220");
  });

  it("reset() restores the default width within bounds", () => {
    store.set(STORAGE_KEY, "300");
    const { result } = renderHook(() => useResizablePane(baseOpts));
    expect(result.current.width).toBe(300);
    act(() => {
      result.current.reset();
    });
    expect(result.current.width).toBe(220);
  });

  it("drag widens the pane for a right-edge handle when moving right", () => {
    const { result } = renderHook(() => useResizablePane(baseOpts));
    act(() => {
      runDrag(result.current.onPointerDown, 100, 150);
    });
    expect(result.current.width).toBe(270);
    expect(result.current.isResizing).toBe(false);
  });

  it("drag narrows a left-edge pane when the pointer moves right", () => {
    const { result } = renderHook(() =>
      useResizablePane({ ...baseOpts, edge: "left", defaultWidth: 340 }),
    );
    act(() => {
      runDrag(result.current.onPointerDown, 500, 560);
    });
    // left edge: pointer moving right shrinks (delta sign flipped)
    expect(result.current.width).toBe(280);
  });

  it("drag clamps to the configured maximum", () => {
    const { result } = renderHook(() => useResizablePane(baseOpts));
    act(() => {
      runDrag(result.current.onPointerDown, 100, 9999);
    });
    expect(result.current.width).toBe(420);
  });

  it("drag clamps to the configured minimum", () => {
    const { result } = renderHook(() => useResizablePane(baseOpts));
    act(() => {
      runDrag(result.current.onPointerDown, 1000, 0);
    });
    expect(result.current.width).toBe(180);
  });

  it("non-primary button pointerdown is ignored", () => {
    const { result } = renderHook(() => useResizablePane(baseOpts));
    act(() => {
      // Simulate right-click — should not start a drag.
      const target = makeHandleTarget();
      const event = {
        button: 2,
        clientX: 100,
        pointerId: 1,
        currentTarget: target,
        preventDefault: () => {},
      } as unknown as React.PointerEvent<HTMLElement>;
      result.current.onPointerDown(event);
    });
    expect(result.current.isResizing).toBe(false);
    // A subsequent move on window must not change width.
    act(() => {
      window.dispatchEvent(
        new PointerEvent("pointermove", { clientX: 9999, pointerId: 1 }),
      );
    });
    expect(result.current.width).toBe(220);
  });

  it("adds a body class while dragging and removes it on pointerup", () => {
    const { result } = renderHook(() => useResizablePane(baseOpts));
    act(() => {
      startDrag(result.current.onPointerDown, 100);
    });
    expect(document.body.classList.contains("resizing-pane")).toBe(true);
    act(() => {
      finishDrag(1);
    });
    expect(document.body.classList.contains("resizing-pane")).toBe(false);
  });

  it("cleans drag listeners and body class when unmounted mid-drag", () => {
    const { result, unmount } = renderHook(() => useResizablePane(baseOpts));
    act(() => {
      startDrag(result.current.onPointerDown, 100);
    });
    expect(document.body.classList.contains("resizing-pane")).toBe(true);

    const widthBeforeUnmount = result.current.width;
    unmount();
    expect(document.body.classList.contains("resizing-pane")).toBe(false);

    // Listeners must have been removed: subsequent pointermove/up events
    // must not throw or mutate state on the stale hook instance.
    expect(() => {
      window.dispatchEvent(
        new PointerEvent("pointermove", { clientX: 9999, pointerId: 1 }),
      );
      window.dispatchEvent(new PointerEvent("pointerup", { pointerId: 1 }));
    }).not.toThrow();
    // The captured value never advanced past mount because the listener
    // was already torn down before the move arrived.
    expect(result.current.width).toBe(widthBeforeUnmount);
  });

  it("stepResize widens a right-edge pane by a positive delta", () => {
    const { result } = renderHook(() => useResizablePane(baseOpts));
    act(() => {
      result.current.stepResize(16);
    });
    expect(result.current.width).toBe(236);
  });

  it("stepResize clamps to the max with +Infinity", () => {
    const { result } = renderHook(() => useResizablePane(baseOpts));
    act(() => {
      result.current.stepResize(Number.POSITIVE_INFINITY);
    });
    expect(result.current.width).toBe(420);
  });

  it("stepResize clamps to the min with -Infinity", () => {
    const { result } = renderHook(() => useResizablePane(baseOpts));
    act(() => {
      result.current.stepResize(Number.NEGATIVE_INFINITY);
    });
    expect(result.current.width).toBe(180);
  });

  it("stepResize never escapes the configured bounds", () => {
    const { result } = renderHook(() => useResizablePane(baseOpts));
    act(() => {
      result.current.stepResize(10_000);
    });
    expect(result.current.width).toBe(420);
    act(() => {
      result.current.stepResize(-10_000);
    });
    expect(result.current.width).toBe(180);
  });
});

// Helpers --------------------------------------------------------------------

function makeHandleTarget(): HTMLElement {
  const el = document.createElement("div");
  el.setPointerCapture = () => {};
  el.releasePointerCapture = () => {};
  return el;
}

function startDrag(
  onPointerDown: (e: React.PointerEvent<HTMLElement>) => void,
  fromX: number,
): HTMLElement {
  const target = makeHandleTarget();
  const event = {
    button: 0,
    clientX: fromX,
    pointerId: 1,
    currentTarget: target,
    preventDefault: () => {},
  } as unknown as React.PointerEvent<HTMLElement>;
  onPointerDown(event);
  return target;
}

function finishDrag(pointerId: number): void {
  window.dispatchEvent(new PointerEvent("pointerup", { pointerId }));
}

function runDrag(
  onPointerDown: (e: React.PointerEvent<HTMLElement>) => void,
  fromX: number,
  toX: number,
): void {
  startDrag(onPointerDown, fromX);
  window.dispatchEvent(
    new PointerEvent("pointermove", { clientX: toX, pointerId: 1 }),
  );
  finishDrag(1);
}
