/**
 * Owns the Tiptap editor instance for `TiptapWikiEditor`.
 *
 * Centralises the round-trip loop guard, the build-once extension set, and the
 * external-content re-seed so the editor component stays a thin presentation
 * layer. Splitting this out also keeps that component under Biome's
 * lines-per-function ceiling.
 *
 * The extension set is built exactly once (via a lazy `useState` initializer):
 * rebuilding it would remount the whole ProseMirror view and lose cursor state
 * on every catalog / resolver change. Live data (catalog, link handler) reaches
 * the long-lived extensions through refs instead.
 */

import { useCallback, useEffect, useRef, useState } from "react";
import type { Extensions } from "@tiptap/core";
import { type Editor, useEditor } from "@tiptap/react";

import {
  wikiHtmlToMarkdown,
  wikiMarkdownToHtml,
} from "../../../lib/wiki/markdown";
import { buildMentionRender } from "./editorMentionRender";
import { type MentionItem, searchMentionItems } from "./inserts/mentionCatalog";
import { buildWikiEditorExtensions } from "./tiptap/extensions";
import { buildWikiMention } from "./tiptap/mention";

export interface UseWikiTiptapEditorArgs {
  /** Initial + controlled markdown from the parent controller. */
  content: string;
  /** Emit canonical markdown back to the controller. */
  onChange: (markdown: string) => void;
  /** Wikilink resolver (drives broken-link styling). */
  resolve: (slug: string) => boolean;
  /** Live mention catalog, projected to `MentionItem`s by the parent. */
  mentionItems: MentionItem[];
  /** Open the link popover for the current selection (Mod-e shortcut). */
  onSetLink: (editor: Editor) => void;
  /** Id of the visible label that names the editor. Wired onto the
   *  contenteditable via `aria-labelledby` so the ProseMirror surface has an
   *  accessible name and an explicit `textbox` role. */
  labelId?: string;
}

export interface WikiTiptapEditor {
  editor: Editor | null;
  /** Always-current editor handle for callbacks fired outside React render. */
  editorRef: React.RefObject<Editor | null>;
  /** Mark a markdown string as "emitted" and forward it to the controller.
   *  Used by the block-splice inserts so the loop guard stays consistent. */
  emit: (markdown: string) => void;
  /** Read the markdown the editor last emitted. `onUpdate` keeps this in
   *  lockstep with the live document, so an insert helper can reuse it
   *  instead of re-serialising `editor.getHTML()` a second time. */
  getLastEmitted: () => string;
}

export function useWikiTiptapEditor({
  content,
  onChange,
  resolve,
  mentionItems,
  onSetLink,
  labelId,
}: UseWikiTiptapEditorArgs): WikiTiptapEditor {
  // Loop guard: the last markdown we handed the controller. The external
  // content effect re-seeds the editor only when `content` diverges from it.
  const lastEmittedRef = useRef<string>(content);
  const onChangeRef = useRef(onChange);
  useEffect(() => {
    onChangeRef.current = onChange;
  }, [onChange]);

  const emit = useCallback((markdown: string) => {
    lastEmittedRef.current = markdown;
    onChangeRef.current(markdown);
  }, []);

  const getLastEmitted = useCallback(() => lastEmittedRef.current, []);

  const editorRef = useRef<Editor | null>(null);

  // Refs the build-once extensions read so live data is never stale.
  const resolveRef = useRef(resolve);
  useEffect(() => {
    resolveRef.current = resolve;
  }, [resolve]);
  const mentionItemsRef = useRef(mentionItems);
  useEffect(() => {
    mentionItemsRef.current = mentionItems;
  }, [mentionItems]);
  const onSetLinkRef = useRef(onSetLink);
  useEffect(() => {
    onSetLinkRef.current = onSetLink;
  }, [onSetLink]);

  // Lazy initializer => built once for the editor's lifetime.
  const [extensions] = useState<Extensions>(() => {
    const base = buildWikiEditorExtensions({
      resolver: (slug) => resolveRef.current(slug),
      onSetLink: () => {
        const ed = editorRef.current;
        if (ed) onSetLinkRef.current(ed);
      },
    });
    const mention = buildWikiMention({
      getItems: (query) =>
        searchMentionItems(mentionItemsRef.current, query, 50),
      render: buildMentionRender(),
    });
    return [...base, mention];
  });

  // Seed the document once. `content` here is the lazy initial value; later
  // external changes flow through the effect below, not this field.
  const [initialHtml] = useState(() => wikiMarkdownToHtml(content, resolve));

  // The ProseMirror contenteditable is the AT-exposed editing surface. Give it
  // an explicit textbox role + multiline hint, and point its accessible name at
  // the parent's visible label (rendered as `#${labelId}`) so it is not an
  // anonymous editable region. Built once via the lazy initializer — the label
  // id is stable for the editor's lifetime.
  const [editorAttributes] = useState<Record<string, string>>(() => {
    const attrs: Record<string, string> = {
      role: "textbox",
      "aria-multiline": "true",
    };
    if (labelId) attrs["aria-labelledby"] = labelId;
    return attrs;
  });

  const editor = useEditor({
    extensions,
    content: initialHtml,
    editorProps: { attributes: editorAttributes },
    onUpdate: ({ editor: ed }) => {
      const markdown = wikiHtmlToMarkdown(ed.getHTML());
      if (markdown === lastEmittedRef.current) return;
      emit(markdown);
    },
    immediatelyRender: false,
  });
  editorRef.current = editor;

  // External content (conflict reload, draft restore) -> editor. Skip when it
  // matches what we last emitted so ordinary typing never resets the cursor.
  useEffect(() => {
    if (!editor) return;
    if (content === lastEmittedRef.current) return;
    lastEmittedRef.current = content;
    editor.commands.setContent(wikiMarkdownToHtml(content, resolveRef.current));
  }, [editor, content]);

  return { editor, editorRef, emit, getLastEmitted };
}
