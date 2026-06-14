import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import { useWikiPanels } from "./useWikiPanels";

const STORAGE_KEY = "wuphf:wiki:panels";

describe("useWikiPanels", () => {
  beforeEach(() => {
    globalThis.localStorage?.clear();
  });

  it("defaults both flanking panels to open", () => {
    const { result } = renderHook(() => useWikiPanels());
    expect(result.current.leftCollapsed).toBe(false);
    expect(result.current.rightCollapsed).toBe(false);
  });

  it("toggles and persists each side independently", () => {
    const { result } = renderHook(() => useWikiPanels());

    act(() => result.current.toggleLeft());
    expect(result.current.leftCollapsed).toBe(true);
    expect(result.current.rightCollapsed).toBe(false);

    act(() => result.current.toggleRight());
    expect(result.current.rightCollapsed).toBe(true);

    expect(globalThis.localStorage.getItem(STORAGE_KEY)).toBe(
      JSON.stringify({ left: true, right: true }),
    );

    act(() => result.current.toggleLeft());
    expect(result.current.leftCollapsed).toBe(false);
    expect(globalThis.localStorage.getItem(STORAGE_KEY)).toBe(
      JSON.stringify({ left: false, right: true }),
    );
  });

  it("restores the persisted collapse state on mount", () => {
    globalThis.localStorage.setItem(
      STORAGE_KEY,
      JSON.stringify({ left: true, right: false }),
    );
    const { result } = renderHook(() => useWikiPanels());
    expect(result.current.leftCollapsed).toBe(true);
    expect(result.current.rightCollapsed).toBe(false);
  });

  it("falls back to open panels when the stored value is malformed", () => {
    globalThis.localStorage.setItem(STORAGE_KEY, "not json");
    const { result } = renderHook(() => useWikiPanels());
    expect(result.current.leftCollapsed).toBe(false);
    expect(result.current.rightCollapsed).toBe(false);
  });
});
