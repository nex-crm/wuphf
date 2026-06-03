import {
  type KeyboardEvent as ReactKeyboardEvent,
  type ReactNode,
  useEffect,
  useRef,
  useState,
} from "react";
import { useDraggable, useDroppable } from "@dnd-kit/core";
import {
  AppWindow,
  File,
  FileCode2,
  FileImage,
  FileSpreadsheet,
  FileText,
  FileVideo,
  Folder,
  GitBranch,
  Globe,
  type LucideIcon,
  NotebookText,
  Presentation,
} from "lucide-react";

import type { WikiFSTreeNode } from "../../../api/wiki";
import { type FileKind, fileKindForPath } from "../viewers/fileKind";
import { baseName } from "./treeModel";

/** Context-menu action requested for a node, handled by the parent tree. */
export type NodeMenuAction = "rename" | "move" | "delete" | "new-subpage";

interface WikiTreeNodeProps {
  node: WikiFSTreeNode;
  depth: number;
  expanded: boolean;
  selected: boolean;
  /**
   * True when this row owns the tree's single tab stop (roving tabindex). All
   * other rows are removed from the tab order; arrow keys move the active row.
   */
  active: boolean;
  /** True while this node is the active drop target for a drag in flight. */
  dropTarget: boolean;
  /** True while a mutation against this node (rename/move/delete) is pending. */
  busy: boolean;
  onToggle: (path: string) => void;
  onSelect: (node: WikiFSTreeNode) => void;
  onMenuAction: (action: NodeMenuAction, node: WikiFSTreeNode) => void;
  /** Keyboard navigation across the flattened visible rows (WAI-ARIA tree). */
  onRowKeyDown: (event: ReactKeyboardEvent, node: WikiFSTreeNode) => void;
  /** Mark this row active (roving focus) when it is focused or clicked. */
  onActivate: (path: string) => void;
  /** Inline rename: when set to this node's path, the row shows an input. */
  renaming: boolean;
  onRenameSubmit: (node: WikiFSTreeNode, newName: string) => void;
  onRenameCancel: () => void;
  /**
   * The child `role="group"` subtree for an expanded folder, rendered inside
   * this treeitem's `<li>` per the WAI-ARIA tree pattern. Undefined for leaves
   * and collapsed folders.
   */
  children?: ReactNode;
}

const ICON_BY_TYPE: Record<WikiFSTreeNode["type"], LucideIcon> = {
  dir: Folder,
  page: FileText,
  file: File,
  app: AppWindow,
  website: Globe,
};

// File leaves get an icon that reflects their file type, classified the same way
// the viewer dispatcher routes them (fileKindForPath is React/viewer-free by
// design). Crisp monochrome line icons (not emoji) so the tree reads as a
// crafted KB surface and inherits the row's text color.
const FILE_GLYPH: Record<FileKind, LucideIcon> = {
  image: FileImage,
  media: FileVideo,
  pdf: FileText,
  csv: FileSpreadsheet,
  xlsx: FileSpreadsheet,
  docx: FileText,
  pptx: Presentation,
  notebook: NotebookText,
  mermaid: GitBranch,
  source: FileCode2,
  google: FileText,
  fallback: File,
};

/**
 * The tree-row icon for a node: file leaves reflect their file type; folders,
 * pages, and embedded apps/websites use their structural icon.
 */
function iconForNode(node: WikiFSTreeNode): LucideIcon {
  if (node.type === "file") return FILE_GLYPH[fileKindForPath(node.path)];
  return ICON_BY_TYPE[node.type];
}

/**
 * One row in the wiki file tree, implemented as a WAI-ARIA `treeitem`. The
 * row is a single tab stop (roving tabindex): exactly one row in the tree has
 * `tabIndex=0` and the rest are `-1`, so the whole tree is one Tab stop and the
 * inner controls (caret, kebab) are reached contextually via the keyboard
 * (arrows) or pointer rather than as separate tab stops.
 *
 * Pages navigate on click; file leaves open the in-app file viewer; app/website
 * leaves open the embedded sandboxed app viewer. The tooltip reflects each
 * destination so the click target is never a surprise.
 */
export default function WikiTreeNode({
  node,
  depth,
  expanded,
  selected,
  active,
  dropTarget,
  busy,
  onToggle,
  onSelect,
  onMenuAction,
  onRowKeyDown,
  onActivate,
  renaming,
  onRenameSubmit,
  onRenameCancel,
  children,
}: WikiTreeNodeProps) {
  const isDir = node.type === "dir";
  const rowRef = useRef<HTMLDivElement | null>(null);

  // Roving focus: when this row becomes the active one, pull DOM focus to it so
  // arrow-key navigation lands on a real focusable element. Skip while renaming
  // (the rename input owns focus) so we never yank focus out of the field.
  useEffect(() => {
    if (active && !renaming && document.activeElement !== rowRef.current) {
      rowRef.current?.focus();
    }
  }, [active, renaming]);

  const {
    attributes,
    listeners,
    setNodeRef: setDragRef,
  } = useDraggable({
    id: node.path,
    // Only pages are movable for now — folders/leaves stay anchored so the
    // backend never has to reparent a whole subtree in this slice.
    disabled: node.type !== "page",
    data: { node },
  });
  const { setNodeRef: setDropRef } = useDroppable({
    id: node.path,
    data: { node },
  });

  const handleRowClick = () => {
    if (renaming) return;
    onActivate(node.path);
    if (isDir) {
      onToggle(node.path);
      return;
    }
    onSelect(node);
  };

  // Pages open the article view; file leaves open the in-app file viewer;
  // app/website leaves open the embedded sandboxed app viewer.
  const leafTitle = rowTooltip(node);

  // dnd-kit's useDraggable injects aria-roledescription="draggable" plus an
  // aria-describedby pointing at its drag instructions. We do NOT register a
  // KeyboardSensor (the keyboard alternative is the explicit "Move to…" menu,
  // satisfying WCAG SC 2.5.7), so announcing "draggable, press space to lift"
  // would mislead AT users into trying a gesture they cannot complete. Strip
  // those two attributes while keeping the pointer listeners + role/tabindex we
  // set ourselves.
  const {
    role: _dragRole,
    tabIndex: _dragTabIndex,
    "aria-roledescription": _dragRoleDesc,
    "aria-describedby": _dragDescribedBy,
    ...dragAttributes
  } = attributes;
  void _dragRole;
  void _dragTabIndex;
  void _dragRoleDesc;
  void _dragDescribedBy;

  const pageDragProps =
    node.type === "page" ? { ...dragAttributes, ...listeners } : {};

  return (
    // Rendered as a div with role="treeitem" (not <li>) so the interactive tree
    // role lives on a neutral element, per the WAI-ARIA tree pattern.
    <div
      ref={(el) => {
        rowRef.current = el;
        setDropRef(el);
      }}
      role="treeitem"
      aria-level={depth + 1}
      aria-selected={selected}
      aria-expanded={isDir ? expanded : undefined}
      tabIndex={active ? 0 : -1}
      className={[
        "wk-tree2-row",
        selected ? "is-selected" : "",
        dropTarget ? "is-drop-target" : "",
        busy ? "is-busy" : "",
      ]
        .filter(Boolean)
        .join(" ")}
      style={{ paddingLeft: `${depth * 14 + 8}px` }}
      data-node-type={node.type}
      data-node-path={node.path}
      onFocus={(e) => {
        if (e.target === rowRef.current) onActivate(node.path);
      }}
      onKeyDown={(e) => {
        if (renaming) return;
        onRowKeyDown(e, node);
      }}
    >
      <div className="wk-tree2-row-inner" ref={setDragRef} {...pageDragProps}>
        <Caret
          isDir={isDir}
          expanded={expanded}
          title={node.title}
          onToggle={() => {
            onActivate(node.path);
            onToggle(node.path);
          }}
        />

        {renaming ? (
          <RenameInput
            initial={stripExt(baseName(node.path))}
            onSubmit={(name) => onRenameSubmit(node, name)}
            onCancel={onRenameCancel}
          />
        ) : (
          <button
            type="button"
            className="wk-tree2-label"
            title={leafTitle}
            // Inside the row's single tab stop — reachable contextually, not as
            // its own Tab stop.
            tabIndex={-1}
            onClick={handleRowClick}
          >
            <span className="wk-tree2-icon" aria-hidden="true">
              {(() => {
                const Icon = iconForNode(node);
                return <Icon size={15} strokeWidth={1.75} />;
              })()}
            </span>
            <span className="wk-tree2-title">{node.title}</span>
            {/*
              App/website leaves share the generic paperclip glyph with file
              leaves, so for AT the title alone would not say they open as an
              embedded app. Append a visually-hidden suffix so screen readers
              announce e.g. "Dashboard (app)" while the visible title stays
              clean. The icon is aria-hidden, so this is the only naming cue.
            */}
            <LeafTypeSuffix node={node} />
            {busy ? (
              <span
                className="wk-spinner wk-tree2-spinner"
                aria-hidden="true"
              />
            ) : null}
          </button>
        )}

        {!renaming ? (
          <RowMenu
            node={node}
            isDir={isDir}
            onMenuAction={onMenuAction}
            kebabTabIndex={active ? 0 : -1}
          />
        ) : null}
      </div>
      {isDir && expanded && children ? (
        <div className="wk-tree2-group" role="group">
          {children}
        </div>
      ) : null}
    </div>
  );
}

interface CaretProps {
  isDir: boolean;
  expanded: boolean;
  title: string;
  onToggle: () => void;
}

/** Expand/collapse caret for folders; an inert spacer for leaves. */
function Caret({ isDir, expanded, title, onToggle }: CaretProps) {
  if (!isDir) {
    return (
      <span
        className="wk-tree2-caret wk-tree2-caret--leaf"
        aria-hidden="true"
      />
    );
  }
  return (
    <button
      type="button"
      className="wk-tree2-caret"
      // Caret is decorative within the treeitem — the treeitem's own
      // aria-expanded + ArrowRight/Left handle expand/collapse for AT. Keep it
      // out of the tab order so the row stays a single tab stop.
      tabIndex={-1}
      aria-label={expanded ? `Collapse ${title}` : `Expand ${title}`}
      aria-expanded={expanded}
      onClick={(e) => {
        e.stopPropagation();
        onToggle();
      }}
    >
      <span aria-hidden="true">{expanded ? "▾" : "▸"}</span>
    </button>
  );
}

interface RowMenuProps {
  node: WikiFSTreeNode;
  isDir: boolean;
  onMenuAction: (action: NodeMenuAction, node: WikiFSTreeNode) => void;
  /**
   * Tab order for the kebab. The tree's roving model keeps exactly one row in
   * the tab order; that row's kebab is reachable contextually (0), every other
   * row's kebab stays out of the tab order (-1).
   */
  kebabTabIndex: number;
}

const MENU_ACTIONS: {
  action: NodeMenuAction;
  label: string;
  dirOnly?: boolean;
}[] = [
  { action: "new-subpage", label: "New sub-page", dirOnly: true },
  { action: "rename", label: "Rename" },
  { action: "move", label: "Move to…" },
  { action: "delete", label: "Delete" },
];

/**
 * Kebab button + dropdown for one row. Owns its open + click-outside state and
 * the menu's keyboard contract: on open focus moves to the first menuitem;
 * ArrowUp/Down roam between items; Escape closes and returns focus to the
 * kebab; blur out of the menu closes it.
 */
function RowMenu({ node, isDir, onMenuAction, kebabTabIndex }: RowMenuProps) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement | null>(null);
  const kebabRef = useRef<HTMLButtonElement | null>(null);
  const menuRef = useRef<HTMLDivElement | null>(null);

  const items = MENU_ACTIONS.filter((m) => isDir || !m.dirOnly);

  useEffect(() => {
    if (!open) return;
    const onDocClick = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    document.addEventListener("mousedown", onDocClick);
    return () => document.removeEventListener("mousedown", onDocClick);
  }, [open]);

  // On open, move focus to the first menuitem (WAI-ARIA menu pattern).
  useEffect(() => {
    if (!open) return;
    const first =
      menuRef.current?.querySelector<HTMLElement>('[role="menuitem"]');
    first?.focus();
  }, [open]);

  const close = (returnFocus: boolean) => {
    setOpen(false);
    if (returnFocus) kebabRef.current?.focus();
  };

  const run = (action: NodeMenuAction) => {
    close(false);
    onMenuAction(action, node);
  };

  const menuItems = () =>
    Array.from(
      menuRef.current?.querySelectorAll<HTMLElement>('[role="menuitem"]') ?? [],
    );

  const focusByOffset = (delta: number) => {
    const nodes = menuItems();
    if (nodes.length === 0) return;
    const active =
      document.activeElement instanceof HTMLElement
        ? document.activeElement
        : null;
    const current = active ? nodes.indexOf(active) : -1;
    const next = (current + delta + nodes.length) % nodes.length;
    nodes[next]?.focus();
  };

  const focusEdge = (edge: "first" | "last") => {
    const nodes = menuItems();
    nodes[edge === "first" ? 0 : nodes.length - 1]?.focus();
  };

  const handleMenuKeyDown = (e: ReactKeyboardEvent<HTMLDivElement>) => {
    switch (e.key) {
      case "Escape":
        e.preventDefault();
        e.stopPropagation();
        close(true);
        break;
      case "ArrowDown":
        e.preventDefault();
        focusByOffset(1);
        break;
      case "ArrowUp":
        e.preventDefault();
        focusByOffset(-1);
        break;
      case "Home":
        e.preventDefault();
        focusEdge("first");
        break;
      case "End":
        e.preventDefault();
        focusEdge("last");
        break;
      default:
        break;
    }
  };

  return (
    <div className="wk-tree2-menu-wrap" ref={ref}>
      <button
        type="button"
        ref={kebabRef}
        className="wk-tree2-kebab"
        // Contextual tab stop: only the active row's kebab is in the tab order,
        // so the tree stays a tight set of stops while the kebab remains
        // keyboard-reachable (and shows a focus-visible ring).
        tabIndex={kebabTabIndex}
        aria-label={`Actions for ${node.title}`}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={(e) => {
          e.stopPropagation();
          setOpen((v) => !v);
        }}
      >
        ⋯
      </button>
      {open ? (
        <div
          className="wk-tree2-menu"
          role="menu"
          ref={menuRef}
          aria-label={`Actions for ${node.title}`}
          onKeyDown={handleMenuKeyDown}
          onBlur={(e) => {
            // Close when focus leaves the menu entirely (not when moving
            // between menuitems).
            if (!e.currentTarget.contains(e.relatedTarget as Node | null)) {
              setOpen(false);
            }
          }}
        >
          {items.map((item) => (
            <button
              key={item.action}
              type="button"
              role="menuitem"
              tabIndex={-1}
              className={[
                "wk-tree2-menu-item",
                item.action === "delete" ? "wk-tree2-menu-item--danger" : "",
              ]
                .filter(Boolean)
                .join(" ")}
              onClick={() => run(item.action)}
            >
              {item.label}
            </button>
          ))}
        </div>
      ) : null}
    </div>
  );
}

function stripExt(name: string): string {
  return name.replace(/\.md$/, "");
}

/**
 * Tooltip text for a row's label. App/website leaves open the embedded
 * sandboxed app viewer, so their tooltip says so; pages and file leaves both
 * open in-app (article view / file viewer), so they just show their path.
 */
function rowTooltip(node: WikiFSTreeNode): string {
  if (node.type === "app" || node.type === "website") {
    return `Opens as an app — ${node.path}`;
  }
  return node.path;
}

/**
 * Visually-hidden suffix appended to a leaf's accessible name so AT can tell
 * app/website leaves apart from plain files (they all render the same paperclip
 * glyph). Renders nothing for every other node type, so the accessible name is
 * unchanged. Kept as its own component so the branch stays out of the row's
 * render path.
 */
function LeafTypeSuffix({ node }: { node: WikiFSTreeNode }) {
  if (node.type === "app") return <span className="sr-only"> (app)</span>;
  if (node.type === "website") {
    return <span className="sr-only"> (website)</span>;
  }
  return null;
}

interface RenameInputProps {
  initial: string;
  onSubmit: (name: string) => void;
  onCancel: () => void;
}

function RenameInput({ initial, onSubmit, onCancel }: RenameInputProps) {
  const [value, setValue] = useState(initial);
  const ref = useRef<HTMLInputElement | null>(null);

  useEffect(() => {
    ref.current?.focus();
    ref.current?.select();
  }, []);

  return (
    <form
      className="wk-tree2-rename"
      onSubmit={(e) => {
        e.preventDefault();
        const trimmed = value.trim();
        if (trimmed) onSubmit(trimmed);
      }}
    >
      <input
        ref={ref}
        className="wk-tree2-rename-input"
        aria-label="New name"
        value={value}
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Escape") {
            e.preventDefault();
            onCancel();
          }
        }}
        onBlur={() => {
          // Commit on blur when the edit is valid (and actually changed), so a
          // click away keeps the user's work instead of silently discarding it.
          // An empty or unchanged value falls back to cancel.
          const trimmed = value.trim();
          if (trimmed && trimmed !== initial) {
            onSubmit(trimmed);
          } else {
            onCancel();
          }
        }}
      />
    </form>
  );
}
