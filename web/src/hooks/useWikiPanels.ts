import { useCallback, useState } from "react";

/**
 * Collapse state for the wiki's two flanking panels — the left page tree and
 * the right "details" rail. Either can be folded away to a thin rail so the
 * reading column gets the full width, docmost/Notion-style. The preference is
 * a durable, per-browser UI choice (like the app sidebar), so it persists to
 * localStorage and survives reloads + navigations between articles.
 */
export type WikiPanelSide = "left" | "right";

interface WikiPanelState {
  left: boolean;
  right: boolean;
}

const STORAGE_KEY = "wuphf:wiki:panels";

function readState(): WikiPanelState {
  try {
    const raw = globalThis.localStorage?.getItem(STORAGE_KEY);
    if (!raw) return { left: false, right: false };
    const parsed = JSON.parse(raw) as Partial<WikiPanelState> | null;
    return { left: parsed?.left === true, right: parsed?.right === true };
  } catch {
    // Private mode / malformed value — default to both panels open.
    return { left: false, right: false };
  }
}

function writeState(state: WikiPanelState): void {
  try {
    globalThis.localStorage?.setItem(STORAGE_KEY, JSON.stringify(state));
  } catch {
    // Private mode / quota — collapse preference is best-effort.
  }
}

export interface WikiPanelsController {
  leftCollapsed: boolean;
  rightCollapsed: boolean;
  toggleLeft: () => void;
  toggleRight: () => void;
}

export function useWikiPanels(): WikiPanelsController {
  const [state, setState] = useState<WikiPanelState>(readState);

  const toggle = useCallback((side: WikiPanelSide) => {
    setState((prev) => {
      const next: WikiPanelState = { ...prev, [side]: !prev[side] };
      writeState(next);
      return next;
    });
  }, []);

  const toggleLeft = useCallback(() => toggle("left"), [toggle]);
  const toggleRight = useCallback(() => toggle("right"), [toggle]);

  return {
    leftCollapsed: state.left,
    rightCollapsed: state.right,
    toggleLeft,
    toggleRight,
  };
}
