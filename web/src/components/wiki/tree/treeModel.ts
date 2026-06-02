/**
 * Pure tree helpers for the wiki file tree. Kept free of React so the
 * drag-and-drop drop→move mapping (the part DnD makes hard to exercise through
 * the DOM) can be unit-tested directly.
 */

import type { WikiFSTreeNode } from "../../../api/wiki";

/** A node together with the chain of folder paths above it. */
export interface FlatTreeRow {
  node: WikiFSTreeNode;
  /** Depth from the rendered root (top-level nodes are depth 0). */
  depth: number;
  /** Parent folder path, or null for a top-level node. */
  parentPath: string | null;
}

/**
 * Walk the tree depth-first, yielding every node with its depth + parent path.
 * Folder children appear immediately after their folder. `expanded` gates which
 * folders' children are emitted; pass an always-true predicate to flatten the
 * whole tree (used by the move-target picker).
 */
export function flattenTree(
  nodes: WikiFSTreeNode[],
  isExpanded: (path: string) => boolean,
): FlatTreeRow[] {
  const out: FlatTreeRow[] = [];
  const walk = (
    list: WikiFSTreeNode[],
    depth: number,
    parentPath: string | null,
  ): void => {
    for (const node of list) {
      out.push({ node, depth, parentPath });
      if (
        node.type === "dir" &&
        node.children &&
        node.children.length > 0 &&
        isExpanded(node.path)
      ) {
        walk(node.children, depth + 1, node.path);
      }
    }
  };
  walk(nodes, 0, null);
  return out;
}

/**
 * Index of `path` in a flattened visible-order row list, or -1 when absent.
 * Used by keyboard navigation to find the row to move focus relative to.
 */
export function rowIndex(rows: FlatTreeRow[], path: string | null): number {
  if (path === null) return -1;
  return rows.findIndex((row) => row.node.path === path);
}

/**
 * The parent-folder path of the row at `index`, or null when the row is
 * top-level. Walks backwards to the nearest preceding row at a shallower depth
 * so ArrowLeft can move focus to the enclosing folder.
 */
export function parentRowPath(
  rows: FlatTreeRow[],
  index: number,
): string | null {
  if (index < 0 || index >= rows.length) return null;
  return rows[index].parentPath;
}

/** Find a node anywhere in the tree by its full path. */
export function findNode(
  nodes: WikiFSTreeNode[],
  path: string,
): WikiFSTreeNode | null {
  for (const node of nodes) {
    if (node.path === path) return node;
    if (node.children) {
      const hit = findNode(node.children, path);
      if (hit) return hit;
    }
  }
  return null;
}

/** True when `node` is a navigable leaf (page/file/app/website, not a folder). */
export function isLeaf(node: WikiFSTreeNode): boolean {
  return node.type !== "dir";
}

/** The directory path a node lives in (everything before the last segment). */
export function parentDir(path: string): string {
  const idx = path.lastIndexOf("/");
  return idx === -1 ? "" : path.slice(0, idx);
}

/** The final path segment (file or folder name with any extension). */
export function baseName(path: string): string {
  const idx = path.lastIndexOf("/");
  return idx === -1 ? path : path.slice(idx + 1);
}

/**
 * Map a drag (source page path) dropped onto a target node into the
 * `{ from, to }` payload for `movePage`. Returns null when the move is a no-op
 * or illegal so the caller can skip the network round-trip:
 *
 *   - dropping a node onto itself,
 *   - dropping a node into the folder it already lives in,
 *   - dropping a folder into one of its own descendants (would orphan it),
 *   - a target that resolves to no destination directory.
 *
 * The destination directory is the target node itself when the target is a
 * folder, otherwise the folder the target leaf lives in. The moved file keeps
 * its own base name under the new directory.
 */
export function dropToMove(
  fromPath: string,
  target: WikiFSTreeNode,
): { from: string; to: string } | null {
  if (!fromPath) return null;
  if (fromPath === target.path) return null;

  const destDir = target.type === "dir" ? target.path : parentDir(target.path);

  // Already lives here — nothing to do.
  if (parentDir(fromPath) === destDir) return null;

  // Refuse to move a folder into its own subtree.
  if (destDir === fromPath || destDir.startsWith(`${fromPath}/`)) return null;

  const name = baseName(fromPath);
  const to = destDir ? `${destDir}/${name}` : name;
  if (to === fromPath) return null;
  return { from: fromPath, to };
}
