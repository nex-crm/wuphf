import { create } from "zustand";

import type { TreeNode } from "../lib/tree";

/**
 * tree-store shim for the refclone editor.
 *
 * The reference editor reads the page tree to (a) resolve wiki-links + internal
 * links to a target page path, (b) drive the @-mention picker's page list, and
 * (c) render the folder index. It also calls `selectPage` + `expandPath` to
 * navigate after a link click.
 *
 * In WUPHF the page tree + navigation live in the host. The provider pushes the
 * current `nodes` in via {@link bindTreeStore} and supplies navigation
 * callbacks; `selectPage` / `expandPath` route OUT to those callbacks so the
 * host owns routing. Like editor-store this is a real Zustand store because the
 * editor uses both the hook and `.getState()` forms.
 */

export interface TreeStore {
  nodes: TreeNode[];
  /** Navigate to / open a page. Routes through the host's navigate callback. */
  selectPage: (path: string) => void;
  /** Expand an ancestor folder so the selected page is visible in the tree. */
  expandPath: (path: string) => void;

  // Host-supplied callbacks (set by the provider).
  onSelectPage?: (path: string) => void;
  onExpandPath?: (path: string) => void;
}

export const useTreeStore = create<TreeStore>((set, get) => ({
  nodes: [],
  onSelectPage: undefined,
  onExpandPath: undefined,

  selectPage: (path: string) => {
    get().onSelectPage?.(path);
  },
  expandPath: (path: string) => {
    get().onExpandPath?.(path);
  },
}));

export interface TreeStoreBridge {
  onSelectPage?: (path: string) => void;
  onExpandPath?: (path: string) => void;
}

/** Push host tree state + navigation callbacks into the store. */
export function bindTreeStore(next: {
  nodes: TreeNode[];
  bridge: TreeStoreBridge;
}): void {
  useTreeStore.setState({
    nodes: next.nodes,
    onSelectPage: next.bridge.onSelectPage,
    onExpandPath: next.bridge.onExpandPath,
  });
}
