/**
 * Minimal tree-node shape the refclone editor operates on. The host app
 * declares a richer tree node in its own type graph; this is a self-contained,
 * structurally-compatible subset so the editor primitives compile without
 * pulling in the host app's types.
 */
export interface TreeNode {
  name: string;
  path: string;
  type:
    | "file"
    | "directory"
    | "website"
    | "app"
    | "pdf"
    | "csv"
    | "code"
    | "image"
    | "video"
    | "audio"
    | "mermaid"
    | "docx"
    | "xlsx"
    | "pptx"
    | "notebook"
    | "unknown";
  children?: TreeNode[];
  [key: string]: unknown;
}

/** Depth-first lookup of the node whose `path` exactly matches. */
export function findNodeByPath(
  nodes: TreeNode[],
  path: string,
): TreeNode | null {
  for (const node of nodes) {
    if (node.path === path) return node;
    if (node.children) {
      const found = findNodeByPath(node.children, path);
      if (found) return found;
    }
  }

  return null;
}
