/**
 * Roving-tabindex + WAI-ARIA keyboard navigation for the cabinet file tree.
 *
 * Owns the single "active" row (the one treeitem with `tabIndex=0`) and maps
 * ArrowUp/Down/Left/Right, Enter/Space, and Home/End onto the flattened
 * visible-row order. Keeping it in a hook keeps the tree component itself flat.
 */

import { type KeyboardEvent as ReactKeyboardEvent, useEffect } from "react";

import type { WikiFSTreeNode } from "../../../api/wiki";
import { type FlatTreeRow, parentRowPath, rowIndex } from "./treeModel";

interface TreeNavigationOptions {
  /** Flattened visible rows, in render order. */
  rows: FlatTreeRow[];
  activePath: string | null;
  setActivePath: (path: string | null) => void;
  /** Whether a folder path is currently expanded (search forces open). */
  isOpen: (node: WikiFSTreeNode) => boolean;
  expand: (path: string) => void;
  collapse: (path: string) => void;
  toggle: (path: string) => void;
  /** Open a leaf node (navigate to the page). */
  onOpenLeaf: (path: string) => void;
}

interface TreeNavigation {
  handleRowKeyDown: (event: ReactKeyboardEvent, node: WikiFSTreeNode) => void;
}

export function useTreeNavigation({
  rows,
  activePath,
  setActivePath,
  isOpen,
  expand,
  collapse,
  toggle,
  onOpenLeaf,
}: TreeNavigationOptions): TreeNavigation {
  // Keep the active row anchored to a row that still exists. If it scrolled out
  // of the visible set (folder collapsed, search narrowed) fall back to the
  // first visible row so Tab always lands somewhere.
  useEffect(() => {
    if (rows.length === 0) {
      if (activePath !== null) setActivePath(null);
      return;
    }
    const stillVisible = rows.some((r) => r.node.path === activePath);
    if (!stillVisible) setActivePath(rows[0].node.path);
  }, [rows, activePath, setActivePath]);

  const moveByOffset = (offset: number) => {
    if (rows.length === 0) return;
    const idx = rowIndex(rows, activePath);
    const from = idx === -1 ? 0 : idx;
    const next = Math.min(Math.max(from + offset, 0), rows.length - 1);
    setActivePath(rows[next].node.path);
  };

  const moveToEdge = (edge: "first" | "last") => {
    if (rows.length === 0) return;
    setActivePath(rows[edge === "first" ? 0 : rows.length - 1].node.path);
  };

  // ArrowRight: open a collapsed folder, else descend into an open one.
  const stepRight = (node: WikiFSTreeNode) => {
    if (node.type !== "dir") return;
    if (isOpen(node)) moveByOffset(1);
    else expand(node.path);
  };

  // ArrowLeft: collapse an open folder, else move focus to the parent row.
  const stepLeft = (node: WikiFSTreeNode) => {
    if (isOpen(node)) {
      collapse(node.path);
      return;
    }
    const parent = parentRowPath(rows, rowIndex(rows, node.path));
    if (parent) setActivePath(parent);
  };

  // Enter/Space: toggle a folder, otherwise open the leaf.
  const activateRow = (node: WikiFSTreeNode) => {
    if (node.type === "dir") toggle(node.path);
    else onOpenLeaf(node.path);
  };

  const handleRowKeyDown = (
    event: ReactKeyboardEvent,
    node: WikiFSTreeNode,
  ) => {
    const handlers: Record<string, () => void> = {
      ArrowDown: () => moveByOffset(1),
      ArrowUp: () => moveByOffset(-1),
      ArrowRight: () => stepRight(node),
      ArrowLeft: () => stepLeft(node),
      Enter: () => activateRow(node),
      " ": () => activateRow(node),
      Home: () => moveToEdge("first"),
      End: () => moveToEdge("last"),
    };
    const handler = handlers[event.key];
    if (!handler) return;
    event.preventDefault();
    handler();
  };

  return { handleRowKeyDown };
}
