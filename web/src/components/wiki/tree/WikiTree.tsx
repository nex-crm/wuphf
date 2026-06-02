import {
  Fragment,
  type DragEvent as ReactDragEvent,
  type ReactNode,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  DndContext,
  type DragEndEvent,
  type DragOverEvent,
  PointerSensor,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import {
  QueryClient,
  QueryClientProvider,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";

import {
  createPage,
  deletePage,
  fetchWikiTree,
  movePage,
  renamePage,
  uploadWikiFile,
  type WikiFSTreeNode,
} from "../../../api/wiki";
import { useFocusTrap } from "../editor/inserts/useFocusTrap";
import { requestOpenInEdit } from "../openInEditTarget";
import {
  baseName,
  dropToMove,
  type FlatTreeRow,
  findNode,
  flattenTree,
  parentDir,
} from "./treeModel";
import { useTreeNavigation } from "./useTreeNavigation";
import WikiTreeNode, { type NodeMenuAction } from "./WikiTreeNode";

const WIKI_TREE_QUERY_KEY = ["wiki-tree"] as const;

/**
 * Navigation sentinel for embedded app/website folders. The tree prepends it to
 * an app/website folder path when a leaf is opened so Wiki can route to the
 * sandboxed WebsiteViewer instead of the article/file view, without changing the
 * shared `(path: string) => void` onNavigate contract. Chosen to never collide
 * with a real wiki path (real paths start with `team/`).
 */
const APP_NAV_PREFIX = "_app:";

interface WikiTreeProps {
  /** Currently-open article path (full team/...md), highlighted in the tree. */
  currentPath?: string | null;
  /** Open a page/leaf — reuses the wiki's navigation. */
  onNavigate: (path: string) => void;
}

interface DeleteState {
  node: WikiFSTreeNode;
}

interface MoveState {
  node: WikiFSTreeNode;
}

interface Note {
  kind: "info" | "error";
  text: string;
}

/**
 * Drag-and-drop wiki file tree. Fetches the team/ content tree, renders
 * folders (expand/collapse) and page/file/app/website leaves, filters by a
 * search box, and exposes a "New page" affordance. Dragging a page onto a
 * folder calls movePage and surfaces how many wikilinks were rewritten.
 *
 * Owns a scoped QueryClient so it works whether or not the host already mounts
 * a `QueryClientProvider` (the live app does; some isolated test/host shells do
 * not). The scoped client keeps the tree's create/move/rename/delete
 * invalidations self-contained.
 */
export default function WikiTree(props: WikiTreeProps) {
  const clientRef = useRef<QueryClient | null>(null);
  if (clientRef.current === null) {
    clientRef.current = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
  }
  return (
    <QueryClientProvider client={clientRef.current}>
      <WikiTreeInner {...props} />
    </QueryClientProvider>
  );
}

function WikiTreeInner({ currentPath, onNavigate }: WikiTreeProps) {
  const queryClient = useQueryClient();
  const { data, isLoading, isError } = useQuery({
    queryKey: WIKI_TREE_QUERY_KEY,
    queryFn: () => fetchWikiTree(),
  });

  const nodes = useMemo(() => data ?? [], [data]);

  const [expanded, setExpanded] = useState<Set<string>>(() => new Set());
  const [query, setQuery] = useState("");
  const [activeDropPath, setActiveDropPath] = useState<string | null>(null);
  const [renamingPath, setRenamingPath] = useState<string | null>(null);
  const [deleteState, setDeleteState] = useState<DeleteState | null>(null);
  const [moveState, setMoveState] = useState<MoveState | null>(null);
  const [newPageOpen, setNewPageOpen] = useState(false);
  const [newSubpageParent, setNewSubpageParent] = useState<string | null>(null);
  // Upload dialog: when open, holds the destination folder it was opened for
  // (null = let the user pick). A drop onto a folder opens it pre-targeted.
  const [uploadOpen, setUploadOpen] = useState(false);
  const [uploadDir, setUploadDir] = useState<string | null>(null);
  const [note, setNote] = useState<Note | null>(null);
  // Roving-tabindex active row: the single treeitem that owns the tab stop.
  const [activePath, setActivePath] = useState<string | null>(null);

  const refetchTree = () =>
    queryClient.invalidateQueries({ queryKey: WIKI_TREE_QUERY_KEY });

  const moveMutation = useMutation({
    mutationFn: movePage,
    onSuccess: async (result) => {
      await refetchTree();
      setNote({
        kind: "info",
        text:
          result.references_rewritten > 0
            ? `Moved. Rewrote ${result.references_rewritten} link${
                result.references_rewritten === 1 ? "" : "s"
              }.`
            : "Moved.",
      });
    },
    onError: (err: unknown) => setNote(errorNote(err, "Move failed")),
  });

  const renameMutation = useMutation({
    mutationFn: renamePage,
    onSuccess: async (result) => {
      await refetchTree();
      setRenamingPath(null);
      setNote({
        kind: "info",
        text:
          result.references_rewritten > 0
            ? `Renamed. Rewrote ${result.references_rewritten} link${
                result.references_rewritten === 1 ? "" : "s"
              }.`
            : "Renamed.",
      });
    },
    onError: (err: unknown) => {
      setRenamingPath(null);
      setNote(errorNote(err, "Rename failed"));
    },
  });

  const deleteMutation = useMutation({
    mutationFn: deletePage,
    onSuccess: async () => {
      await refetchTree();
      setDeleteState(null);
      setNote({ kind: "info", text: "Deleted." });
    },
    onError: (err: unknown) => {
      setDeleteState(null);
      setNote(errorNote(err, "Delete failed"));
    },
  });

  const createMutation = useMutation({
    mutationFn: createPage,
    onSuccess: async (result) => {
      await refetchTree();
      setNewPageOpen(false);
      setMoveState(null);
      setNote({ kind: "info", text: "Page created." });
      // A freshly-created page should land in the WYSIWYG editor, not the
      // empty read view. Park the "open in edit" intent keyed to the new path;
      // WikiArticle pops it on mount and defaults to the editor tab. This keeps
      // the onNavigate(path) contract a plain string (see openInEditTarget.ts).
      requestOpenInEdit(result.path);
      onNavigate(result.path);
    },
    onError: (err: unknown) => setNote(errorNote(err, "Create failed")),
  });

  const uploadMutation = useMutation({
    mutationFn: ({ dir, file }: { dir: string; file: File }) =>
      uploadWikiFile(dir, file),
    onSuccess: async (result) => {
      await refetchTree();
      setUploadOpen(false);
      setUploadDir(null);
      setNote({ kind: "info", text: `Uploaded ${baseName(result.path)}.` });
    },
    onError: (err: unknown) => setNote(errorNote(err, "Upload failed")),
  });

  const busyPath = activeBusyPath(moveMutation, renameMutation, deleteMutation);

  // Open a leaf: pages route to the article view and file leaves to the in-app
  // file viewer (both via onNavigate with the raw path). App/website leaves are
  // embedded surfaces, so they navigate with the APP_NAV_PREFIX sentinel — Wiki
  // strips it back off and mounts WebsiteViewer instead of the article/file
  // view. The sentinel keeps the `(path: string) => void` onNavigate signature
  // intact across the sidebar without threading a separate node-type argument.
  const openLeaf = (node: WikiFSTreeNode) => {
    if (node.type === "app" || node.type === "website") {
      onNavigate(`${APP_NAV_PREFIX}${node.path}`);
      return;
    }
    onNavigate(node.path);
  };
  const openLeafByPath = (path: string) => {
    const node = findNode(nodes, path);
    if (node) openLeaf(node);
    else onNavigate(path);
  };

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
  );

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return nodes;
    return filterTree(nodes, q);
  }, [nodes, query]);

  // While searching, force every folder open so matches deep in the tree are
  // visible without the user expanding each one.
  const searching = query.trim().length > 0;
  const isExpanded = (path: string) => searching || expanded.has(path);

  const rows: FlatTreeRow[] = useMemo(
    () => flattenTree(filtered, (path) => searching || expanded.has(path)),
    [filtered, expanded, searching],
  );

  const toggle = (path: string) =>
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(path)) next.delete(path);
      else next.add(path);
      return next;
    });

  const collapse = (path: string) =>
    setExpanded((prev) => {
      if (!prev.has(path)) return prev;
      const next = new Set(prev);
      next.delete(path);
      return next;
    });

  const expand = (path: string) =>
    setExpanded((prev) => {
      if (prev.has(path)) return prev;
      const next = new Set(prev);
      next.add(path);
      return next;
    });

  const { handleRowKeyDown } = useTreeNavigation({
    rows,
    activePath,
    setActivePath,
    isOpen: (node) =>
      node.type === "dir" && (searching || expanded.has(node.path)),
    expand,
    collapse,
    toggle,
    onOpenLeaf: openLeafByPath,
  });

  const handleDragOver = (event: DragOverEvent) => {
    const overNode = event.over?.data.current?.node as
      | WikiFSTreeNode
      | undefined;
    setActiveDropPath(overNode ? overNode.path : null);
  };

  const handleDragEnd = (event: DragEndEvent) => {
    setActiveDropPath(null);
    const fromPath = String(event.active.id);
    const overNode = event.over?.data.current?.node as
      | WikiFSTreeNode
      | undefined;
    if (!overNode) return;
    const payload = dropToMove(fromPath, overNode);
    if (!payload) return;
    moveMutation.mutate(payload);
  };

  // Native OS file drop onto the tree. dnd-kit only handles internal page
  // drags (those carry no DataTransfer Files), so we read the drop target's
  // enclosing folder from the row under the cursor and open the upload dialog
  // pre-targeted there. A drop outside any folder defaults to the team root.
  const dropTargetDir = (target: EventTarget | null): string => {
    const el =
      target instanceof Element ? target.closest("[data-node-path]") : null;
    const path = el?.getAttribute("data-node-path");
    if (!path) return "team";
    const node = findNode(nodes, path);
    if (!node) return "team";
    return node.type === "dir" ? node.path : parentDir(node.path) || "team";
  };

  const isFileDrag = (event: ReactDragEvent): boolean =>
    Array.from(event.dataTransfer?.types ?? []).includes("Files");

  const handleFileDragOver = (event: ReactDragEvent) => {
    if (!isFileDrag(event)) return;
    event.preventDefault();
    event.dataTransfer.dropEffect = "copy";
  };

  const handleFileDrop = (event: ReactDragEvent) => {
    if (!isFileDrag(event)) return;
    event.preventDefault();
    const file = event.dataTransfer.files?.[0];
    const dir = dropTargetDir(event.target);
    if (!file) {
      setUploadDir(dir);
      setUploadOpen(true);
      return;
    }
    setNote(null);
    uploadMutation.mutate({ dir, file });
  };

  const handleMenuAction = (action: NodeMenuAction, node: WikiFSTreeNode) => {
    setNote(null);
    if (action === "rename") {
      setRenamingPath(node.path);
    } else if (action === "delete") {
      setDeleteState({ node });
    } else if (action === "move") {
      setMoveState({ node });
    } else if (action === "new-subpage") {
      setMoveState(null);
      setNewSubpageParent(node.path);
      setNewPageOpen(true);
    }
  };

  const currentKey = currentPath ?? null;

  return (
    <div className="wk-tree2" data-testid="wk-tree">
      <div className="wk-tree2-toolbar">
        <input
          type="search"
          className="search wk-tree2-search"
          placeholder="Search files…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
        <button
          type="button"
          className="wk-tree2-new"
          onClick={() => {
            setNote(null);
            setNewSubpageParent(null);
            setNewPageOpen(true);
          }}
        >
          + New page
        </button>
        <button
          type="button"
          className="wk-tree2-new wk-tree2-upload"
          onClick={() => {
            setNote(null);
            setUploadDir(null);
            setUploadOpen(true);
          }}
        >
          Upload file
        </button>
      </div>

      {/*
        Live regions are always mounted; we swap their text content rather than
        conditionally mounting the element, so assistive tech reliably announces
        each update. Info → polite status; errors → assertive alert.
      */}
      <div role="status" aria-live="polite" className="sr-only">
        {note?.kind === "info" ? note.text : ""}
      </div>
      <div role="alert" aria-live="assertive" className="sr-only">
        {note?.kind === "error" ? note.text : ""}
      </div>

      {note ? (
        <div className={`wk-tree2-note wk-tree2-note--${note.kind}`}>
          {/*
            The text node is hidden from AT because the always-mounted live
            regions above already announce it; the dismiss control stays in the
            accessibility tree so keyboard users can clear the note.
          */}
          <span aria-hidden="true">{note.text}</span>
          <button
            type="button"
            className="wk-tree2-note-dismiss"
            aria-label="Dismiss notification"
            onClick={() => setNote(null)}
          >
            ×
          </button>
        </div>
      ) : null}

      {isLoading ? (
        <p className="wk-tree2-empty">Loading files…</p>
      ) : isError ? (
        <p className="wk-tree2-empty wk-tree2-empty--error">
          Could not load the file tree.
        </p>
      ) : rows.length === 0 ? (
        <p className="wk-tree2-empty">
          {query.trim() ? "No files match." : "No files yet."}
        </p>
      ) : (
        <DndContext
          sensors={sensors}
          onDragOver={handleDragOver}
          onDragEnd={handleDragEnd}
          onDragCancel={() => setActiveDropPath(null)}
        >
          {/*
            Rendered as div+role rather than ul/li so the interactive ARIA tree
            roles (tree/treeitem/group) live on neutral containers, per the
            WAI-ARIA tree pattern.
          */}
          {/*
            Native OS file-drop target. dnd-kit owns internal page drags; a
            drag carrying DataTransfer Files is an OS upload, which we route to
            uploadMutation against the folder under the cursor.
          */}
          <div
            className="wk-tree2-list"
            role="tree"
            aria-label="Wiki files"
            onDragOver={handleFileDragOver}
            onDrop={handleFileDrop}
          >
            <TreeBranch
              level={filtered}
              depth={0}
              renderNode={(node, depth, subtree) => (
                <WikiTreeNode
                  node={node}
                  depth={depth}
                  expanded={isExpanded(node.path)}
                  selected={currentKey === node.path}
                  active={activePath === node.path}
                  dropTarget={
                    activeDropPath === node.path && node.type === "dir"
                  }
                  busy={busyPath === node.path}
                  onToggle={toggle}
                  onSelect={openLeaf}
                  onMenuAction={handleMenuAction}
                  onRowKeyDown={handleRowKeyDown}
                  onActivate={setActivePath}
                  renaming={renamingPath === node.path}
                  onRenameSubmit={(n, name) =>
                    renameMutation.mutate({ path: n.path, newName: name })
                  }
                  onRenameCancel={() => setRenamingPath(null)}
                >
                  {subtree}
                </WikiTreeNode>
              )}
              isExpanded={isExpanded}
            />
          </div>
        </DndContext>
      )}

      {deleteState ? (
        <ConfirmDelete
          node={deleteState.node}
          pending={deleteMutation.isPending}
          onCancel={() => setDeleteState(null)}
          onConfirm={() => deleteMutation.mutate(deleteState.node.path)}
        />
      ) : null}

      {moveState ? (
        <MoveToDialog
          node={moveState.node}
          nodes={nodes}
          pending={moveMutation.isPending}
          onCancel={() => setMoveState(null)}
          onConfirm={(destDir) => {
            const name = baseName(moveState.node.path);
            const to = destDir ? `${destDir}/${name}` : name;
            if (to === moveState.node.path) {
              setMoveState(null);
              return;
            }
            setMoveState(null);
            moveMutation.mutate({ from: moveState.node.path, to });
          }}
        />
      ) : null}

      {newPageOpen ? (
        <NewPageDialog
          parentDirPath={newSubpageParent}
          nodes={nodes}
          pending={createMutation.isPending}
          onCancel={() => setNewPageOpen(false)}
          onConfirm={(path, title) => createMutation.mutate({ path, title })}
        />
      ) : null}

      {uploadOpen ? (
        <UploadDialog
          initialDir={uploadDir}
          nodes={nodes}
          pending={uploadMutation.isPending}
          onCancel={() => {
            setUploadOpen(false);
            setUploadDir(null);
          }}
          onConfirm={(dir, file) => uploadMutation.mutate({ dir, file })}
        />
      ) : null}
    </div>
  );
}

// ── Recursive nested render (WAI-ARIA tree + group structure) ───────────────────

interface TreeBranchProps {
  level: WikiFSTreeNode[];
  depth: number;
  /**
   * Render the WikiTreeNode for a node. `subtree` is the rendered children
   * group for an expanded folder (undefined otherwise), which the node places
   * inside its own `<li>` as a `role="group"`.
   */
  renderNode: (
    node: WikiFSTreeNode,
    depth: number,
    subtree: ReactNode,
  ) => ReactNode;
  isExpanded: (path: string) => boolean;
}

/**
 * Render one level of the tree, recursing into expanded folders so each
 * folder's children live inside its `<li>` as a `role="group"` (the WAI-ARIA
 * tree pattern).
 */
function TreeBranch({ level, depth, renderNode, isExpanded }: TreeBranchProps) {
  return (
    <>
      {level.map((node) => {
        const open =
          node.type === "dir" &&
          !!node.children &&
          node.children.length > 0 &&
          isExpanded(node.path);
        const subtree =
          open && node.children ? (
            <TreeBranch
              level={node.children}
              depth={depth + 1}
              renderNode={renderNode}
              isExpanded={isExpanded}
            />
          ) : undefined;
        return (
          <Fragment key={node.path}>
            {renderNode(node, depth, subtree)}
          </Fragment>
        );
      })}
    </>
  );
}

// ── Mutation-state helpers ─────────────────────────────────────────────────────

interface PendingMutation {
  isPending: boolean;
  variables?: unknown;
}

function activeBusyPath(
  move: PendingMutation,
  rename: PendingMutation,
  del: PendingMutation,
): string | null {
  if (move.isPending && isFromVar(move.variables)) {
    return (move.variables as { from: string }).from;
  }
  if (rename.isPending && isPathVar(rename.variables)) {
    return (rename.variables as { path: string }).path;
  }
  if (del.isPending && typeof del.variables === "string") {
    return del.variables;
  }
  return null;
}

function isFromVar(v: unknown): v is { from: string } {
  return typeof v === "object" && v !== null && "from" in v;
}

function isPathVar(v: unknown): v is { path: string } {
  return typeof v === "object" && v !== null && "path" in v;
}

function errorNote(err: unknown, fallback: string): Note {
  return {
    kind: "error",
    text: err instanceof Error && err.message ? err.message : fallback,
  };
}

// ── Search filter ──────────────────────────────────────────────────────────────

/**
 * Keep nodes whose title/path matches, plus any ancestor folders needed to
 * reach a match. Returns new node objects (immutable) so the source tree is
 * never mutated.
 */
function filterTree(nodes: WikiFSTreeNode[], q: string): WikiFSTreeNode[] {
  const out: WikiFSTreeNode[] = [];
  for (const node of nodes) {
    const selfMatch =
      node.title.toLowerCase().includes(q) ||
      node.path.toLowerCase().includes(q);
    if (node.type === "dir" && node.children) {
      const kids = filterTree(node.children, q);
      if (selfMatch || kids.length > 0) {
        out.push({ ...node, children: kids });
      }
    } else if (selfMatch) {
      out.push(node);
    }
  }
  return out;
}

// ── Confirm-before-delete ──────────────────────────────────────────────────────

interface ConfirmDeleteProps {
  node: WikiFSTreeNode;
  pending: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}

function ConfirmDelete({
  node,
  pending,
  onCancel,
  onConfirm,
}: ConfirmDeleteProps) {
  // Cancel is the first focusable control in the DOM, so the focus trap lands
  // initial focus there — a stray Enter cancels rather than destroying data.
  const trapRef = useFocusTrap<HTMLDivElement>();
  return (
    <div
      ref={trapRef}
      className="wk-modal-backdrop"
      role="dialog"
      aria-modal="true"
      aria-labelledby="wk-tree2-delete-title"
      data-testid="wk-tree-delete-confirm"
      onKeyDown={(e) => {
        if (e.key === "Escape") {
          e.preventDefault();
          onCancel();
        }
      }}
    >
      <div className="wk-modal wk-tree2-modal">
        <h2 id="wk-tree2-delete-title">Delete “{node.title}”?</h2>
        <p className="wk-editor-help">
          This permanently deletes <code>{node.path}</code>. We recommend
          deleting only files you are sure are no longer referenced. This cannot
          be undone.
        </p>
        <div className="wk-editor-actions">
          <button
            type="button"
            className="wk-editor-cancel"
            onClick={onCancel}
            disabled={pending}
          >
            Cancel
          </button>
          <button
            type="button"
            className="wk-editor-save wk-tree2-danger-btn"
            onClick={onConfirm}
            disabled={pending}
          >
            {pending ? "Deleting…" : "Delete"}
          </button>
        </div>
      </div>
    </div>
  );
}

// ── Move-to picker ──────────────────────────────────────────────────────────────

interface MoveToDialogProps {
  node: WikiFSTreeNode;
  nodes: WikiFSTreeNode[];
  pending: boolean;
  onCancel: () => void;
  onConfirm: (destDir: string) => void;
}

function MoveToDialog({
  node,
  nodes,
  pending,
  onCancel,
  onConfirm,
}: MoveToDialogProps) {
  const dirs = useMemo(
    () => collectFolderDirs(nodes, node.path),
    [nodes, node],
  );
  const currentParent = parentDir(node.path);
  const [dest, setDest] = useState<string>(dirs[0]?.path ?? "");
  const trapRef = useFocusTrap<HTMLDivElement>();

  return (
    <div
      ref={trapRef}
      className="wk-modal-backdrop"
      role="dialog"
      aria-modal="true"
      aria-labelledby="wk-tree2-move-title"
      data-testid="wk-tree-move-dialog"
      onKeyDown={(e) => {
        if (e.key === "Escape") {
          e.preventDefault();
          onCancel();
        }
      }}
    >
      <div className="wk-modal wk-tree2-modal">
        <h2 id="wk-tree2-move-title">Move “{node.title}”</h2>
        <label className="wk-editor-label" htmlFor="wk-tree2-move-dest">
          Destination folder
        </label>
        <select
          id="wk-tree2-move-dest"
          className="wk-editor-commit"
          value={dest}
          onChange={(e) => setDest(e.target.value)}
        >
          {dirs.map((d) => (
            <option key={d.path || "__root__"} value={d.path}>
              {d.label}
              {d.path === currentParent ? " (current)" : ""}
            </option>
          ))}
        </select>
        <div className="wk-editor-actions">
          <button
            type="button"
            className="wk-editor-cancel"
            onClick={onCancel}
            disabled={pending}
          >
            Cancel
          </button>
          <button
            type="button"
            className="wk-editor-save"
            onClick={() => onConfirm(dest)}
            disabled={pending || dest === currentParent}
          >
            {pending ? "Moving…" : "Move"}
          </button>
        </div>
      </div>
    </div>
  );
}

interface FolderChoice {
  path: string;
  label: string;
}

/**
 * Every folder the node could be moved into: all `dir` nodes plus the top-level
 * `team/` root, minus the node itself and its own subtree (illegal targets).
 */
function collectFolderDirs(
  nodes: WikiFSTreeNode[],
  movingPath: string,
): FolderChoice[] {
  const out: FolderChoice[] = [{ path: "team", label: "team/ (root)" }];
  const walk = (list: WikiFSTreeNode[]) => {
    for (const n of list) {
      if (n.type === "dir") {
        const illegal =
          n.path === movingPath || n.path.startsWith(`${movingPath}/`);
        if (!illegal && n.path !== "team") {
          out.push({ path: n.path, label: n.path });
        }
        if (n.children) walk(n.children);
      }
    }
  };
  walk(nodes);
  return out;
}

// ── New-page dialog ──────────────────────────────────────────────────────────────

interface NewPageDialogProps {
  /** Folder to create the page in, or null for a top-level picker. */
  parentDirPath: string | null;
  nodes: WikiFSTreeNode[];
  pending: boolean;
  onCancel: () => void;
  onConfirm: (path: string, title: string) => void;
}

function NewPageDialog({
  parentDirPath,
  nodes,
  pending,
  onCancel,
  onConfirm,
}: NewPageDialogProps) {
  const dirs = useMemo(() => collectFolderDirs(nodes, " never"), [nodes]);
  const [dest, setDest] = useState<string>(
    parentDirPath ?? dirs[0]?.path ?? "team",
  );
  const [title, setTitle] = useState("");
  const [slug, setSlug] = useState("");
  const [error, setError] = useState<string | null>(null);

  const effectiveSlug = (slug || slugify(title)).trim();
  const path = effectiveSlug ? `${dest}/${effectiveSlug}.md` : "";
  const titleRef = useRef<HTMLInputElement | null>(null);

  // Land initial focus on the Title field — the primary input — before the
  // focus trap runs. The trap only steals focus when none is inside the
  // dialog, so it leaves this field focused.
  useEffect(() => {
    titleRef.current?.focus();
  }, []);
  const trapRef = useFocusTrap<HTMLDivElement>();

  const submit = () => {
    setError(null);
    if (!title.trim()) {
      setError("Title is required.");
      return;
    }
    if (!(effectiveSlug && /^[a-z0-9][a-z0-9-]*$/.test(effectiveSlug))) {
      setError("Slug must be lowercase letters, numbers, and hyphens.");
      return;
    }
    onConfirm(path, title.trim());
  };

  return (
    <div
      ref={trapRef}
      className="wk-modal-backdrop"
      role="dialog"
      aria-modal="true"
      aria-labelledby="wk-tree2-new-title"
      data-testid="wk-tree-new-page"
      onKeyDown={(e) => {
        if (e.key === "Escape") {
          e.preventDefault();
          onCancel();
        }
      }}
    >
      <div className="wk-modal wk-tree2-modal">
        <h2 id="wk-tree2-new-title">New page</h2>

        <label className="wk-editor-label" htmlFor="wk-tree2-new-dest">
          Folder
        </label>
        <select
          id="wk-tree2-new-dest"
          className="wk-editor-commit"
          value={dest}
          onChange={(e) => setDest(e.target.value)}
        >
          {dirs.map((d) => (
            <option key={d.path || "__root__"} value={d.path}>
              {d.label}
            </option>
          ))}
        </select>

        <label className="wk-editor-label" htmlFor="wk-tree2-new-title-input">
          Title
        </label>
        <input
          ref={titleRef}
          id="wk-tree2-new-title-input"
          className="wk-editor-commit"
          type="text"
          placeholder="Sarah Chen"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
        />

        <label className="wk-editor-label" htmlFor="wk-tree2-new-slug">
          Slug <span className="wk-editor-optional">(optional)</span>
        </label>
        <input
          id="wk-tree2-new-slug"
          className="wk-editor-commit"
          type="text"
          placeholder={slugify(title) || "sarah-chen"}
          value={slug}
          onChange={(e) =>
            setSlug(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, "-"))
          }
        />

        {path ? (
          <p className="wk-editor-help">
            Will create <code>{path}</code>
          </p>
        ) : null}

        {error ? (
          <div
            className="wk-editor-banner wk-editor-banner--error"
            role="alert"
          >
            {error}
          </div>
        ) : null}

        <div className="wk-editor-actions">
          <button
            type="button"
            className="wk-editor-save"
            onClick={submit}
            disabled={pending}
          >
            {pending ? "Creating…" : "Create page"}
          </button>
          <button
            type="button"
            className="wk-editor-cancel"
            onClick={onCancel}
            disabled={pending}
          >
            Cancel
          </button>
        </div>
      </div>
    </div>
  );
}

function slugify(value: string): string {
  return value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "");
}

// ── Upload dialog ────────────────────────────────────────────────────────────

interface UploadDialogProps {
  /** Folder a drop pre-targeted, or null to let the user pick. */
  initialDir: string | null;
  nodes: WikiFSTreeNode[];
  pending: boolean;
  onCancel: () => void;
  onConfirm: (dir: string, file: File) => void;
}

function UploadDialog({
  initialDir,
  nodes,
  pending,
  onCancel,
  onConfirm,
}: UploadDialogProps) {
  const dirs = useMemo(() => collectFolderDirs(nodes, " never"), [nodes]);
  const [dest, setDest] = useState<string>(
    initialDir ?? dirs[0]?.path ?? "team",
  );
  const [file, setFile] = useState<File | null>(null);
  const [error, setError] = useState<string | null>(null);
  const trapRef = useFocusTrap<HTMLDivElement>();

  const submit = () => {
    setError(null);
    if (!file) {
      setError("Choose a file to upload.");
      return;
    }
    onConfirm(dest, file);
  };

  return (
    <div
      ref={trapRef}
      className="wk-modal-backdrop"
      role="dialog"
      aria-modal="true"
      aria-labelledby="wk-tree2-upload-title"
      data-testid="wk-tree-upload"
      onKeyDown={(e) => {
        if (e.key === "Escape") {
          e.preventDefault();
          onCancel();
        }
      }}
    >
      <div className="wk-modal wk-tree2-modal">
        <h2 id="wk-tree2-upload-title">Upload file</h2>

        <label className="wk-editor-label" htmlFor="wk-tree2-upload-dest">
          Folder
        </label>
        <select
          id="wk-tree2-upload-dest"
          className="wk-editor-commit"
          value={dest}
          onChange={(e) => setDest(e.target.value)}
        >
          {dirs.map((d) => (
            <option key={d.path || "__root__"} value={d.path}>
              {d.label}
            </option>
          ))}
        </select>

        <span className="wk-editor-label">File</span>
        <div className="wk-filepick">
          <label className="wk-filepick__btn" htmlFor="wk-tree2-upload-file">
            Choose file
          </label>
          <span
            id="wk-tree2-upload-file-name"
            className={`wk-filepick__name${file ? " wk-filepick__name--set" : ""}`}
          >
            {file ? file.name : "No file chosen"}
          </span>
          <input
            id="wk-tree2-upload-file"
            className="wk-filepick__input"
            type="file"
            aria-describedby="wk-tree2-upload-file-name"
            onChange={(e) => {
              setError(null);
              setFile(e.target.files?.[0] ?? null);
            }}
          />
        </div>

        {file ? (
          <p className="wk-editor-help">
            Will upload <code>{file.name}</code> to <code>{dest}</code>
          </p>
        ) : null}

        {error ? (
          <div
            className="wk-editor-banner wk-editor-banner--error"
            role="alert"
          >
            {error}
          </div>
        ) : null}

        <div className="wk-editor-actions">
          <button
            type="button"
            className="wk-editor-save"
            onClick={submit}
            disabled={pending}
          >
            {pending ? "Uploading…" : "Upload"}
          </button>
          <button
            type="button"
            className="wk-editor-cancel"
            onClick={onCancel}
            disabled={pending}
          >
            Cancel
          </button>
        </div>
      </div>
    </div>
  );
}

// Re-export the query key + findNode for callers/tests that want to seed or
// assert against the tree query. APP_NAV_PREFIX is shared with Wiki so the
// sentinel that routes app/website folders to the embedded viewer stays defined
// in exactly one place.
export { APP_NAV_PREFIX, findNode, WIKI_TREE_QUERY_KEY };
