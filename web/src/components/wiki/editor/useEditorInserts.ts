/**
 * Insert-action state machine for the Tiptap wiki editor.
 *
 * Owns the active dialog (citation / fact / decision / related / mention
 * picker) plus the markdown-splice helpers the WUPHF block inserts use:
 *
 *   - BLOCK inserts (fact, decision, related, citation definition) cannot be
 *     reliably inserted as live ProseMirror nodes from a fenced markdown
 *     string — Tiptap would treat the backticks as literal text inside the
 *     active paragraph. Instead we read the whole doc as markdown, apply the
 *     pure emitter from `inserts/markdownShapes.ts`, and reload the doc via
 *     `setContent(wikiMarkdownToHtml(...))`. The reload re-parses at the
 *     document level so fenced blocks land as real block nodes.
 *
 *   - INLINE inserts (citation `[^id]` reference, wikilink, mention) are
 *     inserted at the caret directly by the editor component.
 *
 * The hook is editor-aware but holds no React-render concerns; the editor
 * component renders the matching dialog for `dialog`.
 */

import { useCallback, useState } from "react";
import type { Editor } from "@tiptap/react";

import {
  wikiHtmlToMarkdown,
  wikiMarkdownToHtml,
} from "../../../lib/wiki/markdown";
import {
  appendBlockToTail,
  appendCitationDefinition,
  type BuiltCitation,
} from "./inserts/markdownShapes";

export type InsertDialog =
  | null
  | "citation"
  | "fact"
  | "decision"
  | "related"
  | "mention-picker";

export interface EditorInserts {
  dialog: InsertDialog;
  openDialog: (next: Exclude<InsertDialog, null>) => void;
  closeDialog: () => void;
  /** Reload the document from a new markdown string (block-splice path). */
  reloadMarkdown: (nextMarkdown: string) => void;
  /** Append a fenced/heading block to the doc tail, then reload. */
  appendBlock: (block: string) => void;
  /** Insert a citation: `[^id]` at the caret + definition appended to tail. */
  insertCitation: (built: BuiltCitation) => void;
}

export interface UseEditorInsertsArgs {
  /** The live Tiptap editor, or null before mount. */
  getEditor: () => Editor | null;
  /** Resolver so the reloaded HTML flags broken wikilinks consistently. */
  resolver: (slug: string) => boolean;
  /** Notify the controller of the new markdown after a splice/reload. */
  onChange: (markdown: string) => void;
  /** Reads the markdown the editor last emitted. The inline citation insert
   *  reuses this instead of re-serialising `editor.getHTML()` a second time —
   *  Tiptap's `onUpdate` has already produced it. */
  getLastEmitted: () => string;
}

export function useEditorInserts({
  getEditor,
  resolver,
  onChange,
  getLastEmitted,
}: UseEditorInsertsArgs): EditorInserts {
  const [dialog, setDialog] = useState<InsertDialog>(null);

  const openDialog = useCallback((next: Exclude<InsertDialog, null>) => {
    setDialog(next);
  }, []);
  const closeDialog = useCallback(() => setDialog(null), []);

  const reloadMarkdown = useCallback(
    (nextMarkdown: string) => {
      const editor = getEditor();
      if (!editor) return;
      // Emit FIRST so the loop guard (`lastEmittedRef` inside `onChange`) is
      // primed before `setContent` fires Tiptap's `onUpdate`. The update path
      // serialises the new doc, sees it already matches the last emitted
      // markdown, and short-circuits — otherwise the controller would receive
      // two onChange calls (and write two drafts) for one logical insert.
      onChange(nextMarkdown);
      editor.commands.setContent(wikiMarkdownToHtml(nextMarkdown, resolver));
    },
    [getEditor, resolver, onChange],
  );

  const appendBlock = useCallback(
    (block: string) => {
      const editor = getEditor();
      if (!editor) return;
      const current = wikiHtmlToMarkdown(editor.getHTML());
      reloadMarkdown(appendBlockToTail(current, block));
      setDialog(null);
    },
    [getEditor, reloadMarkdown],
  );

  const insertCitation = useCallback(
    (built: BuiltCitation) => {
      const editor = getEditor();
      if (!editor) return;
      // Inline reference at the caret first. The `.run()` fires Tiptap's
      // `onUpdate`, which serialises the post-insert doc into the loop guard.
      editor.chain().focus().insertContent(built.reference).run();
      // Reuse that serialisation instead of converting `editor.getHTML()`
      // again — same bytes, one fewer HTML->markdown pass.
      const withRef = getLastEmitted();
      reloadMarkdown(appendCitationDefinition(withRef, built.definition));
      setDialog(null);
    },
    [getEditor, getLastEmitted, reloadMarkdown],
  );

  return {
    dialog,
    openDialog,
    closeDialog,
    reloadMarkdown,
    appendBlock,
    insertCitation,
  };
}
