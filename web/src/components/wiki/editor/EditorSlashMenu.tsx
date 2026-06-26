/**
 * Combined slash menu for the Tiptap wiki editor.
 *
 * Two command groups in one floating list:
 *   - "Basic" blocks (text, H1-3, lists, code, quote, divider, table, image)
 *     — direct Tiptap commands or a popover request handled by the parent.
 *   - "Insert" — the WUPHF-specific actions from `inserts/types.ts`
 *     (`SLASH_ACTIONS`), filtered with the shared `filterSlashActions` so the
 *     keyword matching stays identical to the rest of the wiki tooling.
 *
 * The menu is coordinate-positioned (fixed) and portalled to `document.body`,
 * matching the existing `wk-insert-menu` chrome. Keyboard nav reuses the
 * shared `useMenuKeyNav` hook.
 */

import { createPortal } from "react-dom";

import {
  filterSlashActions,
  type SlashAction,
  type SlashActionDef,
} from "./inserts/types";
import { useActiveDescendant } from "./inserts/useActiveDescendant";
import { useMenuKeyNav } from "./inserts/useMenuKeyNav";

/** Stable id for the slash listbox + a deterministic per-option id so the
 *  editor's `aria-activedescendant` can point at the active row. */
const SLASH_LISTBOX_ID = "wk-slash-listbox";
export function slashOptionId(idx: number): string {
  return `wk-slash-opt-${idx}`;
}

/** Identifiers for the basic blocks. */
export type BasicBlock =
  | "text"
  | "h1"
  | "h2"
  | "h3"
  | "bullet"
  | "ordered"
  | "check"
  | "code"
  | "quote"
  | "divider"
  | "table"
  | "image";

interface BasicBlockDef {
  id: BasicBlock;
  title: string;
  description: string;
  keywords: string[];
}

const BASIC_BLOCKS: BasicBlockDef[] = [
  {
    id: "text",
    title: "Text",
    description: "Plain paragraph.",
    keywords: ["text", "paragraph", "p"],
  },
  {
    id: "h1",
    title: "Heading 1",
    description: "Large section heading.",
    keywords: ["h1", "heading", "title"],
  },
  {
    id: "h2",
    title: "Heading 2",
    description: "Medium section heading.",
    keywords: ["h2", "heading", "subtitle"],
  },
  {
    id: "h3",
    title: "Heading 3",
    description: "Small section heading.",
    keywords: ["h3", "heading"],
  },
  {
    id: "bullet",
    title: "Bullet list",
    description: "Unordered list.",
    keywords: ["bullet", "list", "ul"],
  },
  {
    id: "ordered",
    title: "Numbered list",
    description: "Ordered list.",
    keywords: ["numbered", "ordered", "list", "ol"],
  },
  {
    id: "check",
    title: "Checklist",
    description: "Task list with checkboxes.",
    keywords: ["check", "task", "todo", "checkbox"],
  },
  {
    id: "code",
    title: "Code block",
    description: "Fenced code with syntax highlighting.",
    keywords: ["code", "snippet", "fence"],
  },
  {
    id: "quote",
    title: "Quote",
    description: "Blockquote.",
    keywords: ["quote", "blockquote", "callout"],
  },
  {
    id: "divider",
    title: "Divider",
    description: "Horizontal rule.",
    keywords: ["divider", "hr", "rule", "separator"],
  },
  {
    id: "table",
    title: "Table",
    description: "3x3 table with a header row.",
    keywords: ["table", "grid"],
  },
  {
    id: "image",
    title: "Image",
    description: "Embed an image by URL.",
    keywords: ["image", "img", "picture", "photo"],
  },
];

function filterBasicBlocks(query: string): BasicBlockDef[] {
  const q = query.trim().toLowerCase();
  if (!q) return BASIC_BLOCKS;
  return BASIC_BLOCKS.filter((b) => {
    if (b.title.toLowerCase().includes(q)) return true;
    if (b.description.toLowerCase().includes(q)) return true;
    return b.keywords.some((k) => k.includes(q));
  });
}

type Row =
  | { kind: "basic"; def: BasicBlockDef }
  | { kind: "action"; def: SlashActionDef };

export interface EditorSlashMenuProps {
  query: string;
  position: { top: number; left: number };
  onSelectBasic: (block: BasicBlock) => void;
  onSelectAction: (action: SlashAction) => void;
  onClose: () => void;
  /** Active row index, owned by the parent so the keydown watcher and the
   *  menu stay in lockstep. */
  activeIdx: number;
  setActiveIdx: (next: number | ((prev: number) => number)) => void;
  /** The editor's contenteditable DOM node. Focus stays here while the menu
   *  is open, so this is the element that carries `aria-activedescendant`. */
  editorDom?: HTMLElement | null;
}

/**
 * Flatten the two groups into one ordered list. Exported so the parent's
 * keydown handler can resolve the active row to a command without
 * duplicating the filter logic.
 */
export function buildSlashRows(query: string): Row[] {
  const rows: Row[] = [];
  for (const def of filterBasicBlocks(query)) rows.push({ kind: "basic", def });
  for (const def of filterSlashActions(query)) {
    rows.push({ kind: "action", def });
  }
  return rows;
}

export function EditorSlashMenu({
  query,
  position,
  onSelectBasic,
  onSelectAction,
  onClose,
  activeIdx,
  setActiveIdx,
  editorDom,
}: EditorSlashMenuProps): React.ReactElement | null {
  const rows = buildSlashRows(query);

  const commit = (row: Row): void => {
    if (row.kind === "basic") onSelectBasic(row.def.id);
    else onSelectAction(row.def.id);
  };

  useMenuKeyNav<Row>({
    items: rows,
    activeIdx,
    setActiveIdx,
    onCommit: commit,
    onClose,
  });

  // Mirror the active option onto the editor (which keeps focus) so AT tracks
  // the highlighted row. Null when nothing matches the query.
  const activeOptionId =
    rows.length > 0 && activeIdx >= 0 && activeIdx < rows.length
      ? slashOptionId(activeIdx)
      : null;
  useActiveDescendant(editorDom, activeOptionId);

  const style: React.CSSProperties = {
    position: "fixed",
    top: `${position.top}px`,
    left: `${position.left}px`,
    zIndex: 50,
  };

  if (rows.length === 0) {
    return createPortal(
      <div
        className="wk-insert-menu wk-insert-menu--empty"
        data-testid="wk-slash-menu-empty"
        id={SLASH_LISTBOX_ID}
        style={style}
        role="listbox"
        aria-label="Insert block"
      >
        <div className="wk-insert-menu__empty">No matching blocks</div>
      </div>,
      document.body,
    );
  }

  const basics = rows.filter((r) => r.kind === "basic");
  const actions = rows.filter((r) => r.kind === "action");
  const idxOf = (row: Row): number => rows.indexOf(row);

  return createPortal(
    <div
      className="wk-insert-menu"
      data-testid="wk-slash-menu"
      id={SLASH_LISTBOX_ID}
      style={style}
      role="listbox"
      aria-label="Insert block"
    >
      {basics.length > 0 ? (
        <div className="wk-insert-menu__group">
          <div className="wk-insert-menu__group-label">Basic</div>
          <ul className="wk-insert-menu__list">
            {basics.map((row) => (
              <SlashRow
                key={row.def.id}
                optionId={slashOptionId(idxOf(row))}
                title={row.def.title}
                description={row.def.description}
                testId={`wk-slash-basic-${row.def.id}`}
                active={idxOf(row) === activeIdx}
                onActivate={() => setActiveIdx(idxOf(row))}
                onCommit={() => commit(row)}
              />
            ))}
          </ul>
        </div>
      ) : null}
      {actions.length > 0 ? (
        <div className="wk-insert-menu__group">
          <div className="wk-insert-menu__group-label">Insert</div>
          <ul className="wk-insert-menu__list">
            {actions.map((row) => (
              <SlashRow
                key={row.def.id}
                optionId={slashOptionId(idxOf(row))}
                title={row.def.title}
                description={row.def.description}
                testId={`wk-slash-action-${row.def.id}`}
                active={idxOf(row) === activeIdx}
                onActivate={() => setActiveIdx(idxOf(row))}
                onCommit={() => commit(row)}
              />
            ))}
          </ul>
        </div>
      ) : null}
    </div>,
    document.body,
  );
}

interface SlashRowProps {
  optionId: string;
  title: string;
  description: string;
  testId: string;
  active: boolean;
  onActivate: () => void;
  onCommit: () => void;
}

function SlashRow({
  optionId,
  title,
  description,
  testId,
  active,
  onActivate,
  onCommit,
}: SlashRowProps): React.ReactElement {
  return (
    <li className={`wk-insert-menu__item${active ? " is-active" : ""}`}>
      <button
        type="button"
        className="wk-insert-menu__btn"
        id={optionId}
        role="option"
        aria-selected={active}
        data-testid={testId}
        onMouseEnter={onActivate}
        onMouseDown={(e) => {
          // mousedown so the editor keeps the caret we insert at.
          e.preventDefault();
          onCommit();
        }}
      >
        <span className="wk-insert-menu__title">{title}</span>
        <span className="wk-insert-menu__desc">{description}</span>
      </button>
    </li>
  );
}
