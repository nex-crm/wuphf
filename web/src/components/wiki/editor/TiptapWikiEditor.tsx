/**
 * Tiptap-backed rich editor for wiki articles.
 *
 * Replaces the former Milkdown editor behind the per-article "Rich" toggle in
 * `WikiEditor`. Lazy-loaded so the default textarea path stays zero-cost.
 *
 * Round-trip contract (mirrors the old Milkdown editor's loop-guard, now owned
 * by `useWikiTiptapEditor`):
 *   - On mount / external reload, markdown -> HTML via `wikiMarkdownToHtml`
 *     seeds the document; wikilinks become `wikiLink` marks.
 *   - On every edit, `editor.getHTML()` -> `wikiHtmlToMarkdown` -> `onChange`.
 *   - The hook tracks the last markdown emitted; the parent's `content` prop is
 *     only re-seeded when it differs, so normal typing never self-resets.
 *
 * Surfaces mounted here: the bubble menu (text selection), the slash menu
 * (`/`), the `@`-mention picker, the WUPHF insert dialogs (citation / fact /
 * decision / related / wiki-link / agent-mention pickers), and the link /
 * image popovers.
 */

import { useCallback, useMemo, useState } from "react";
import { type Editor, EditorContent } from "@tiptap/react";

import type { WikiCatalogEntry } from "../../../api/wiki";
import { EditorBubbleMenu } from "./EditorBubbleMenu";
import { EditorImagePopover } from "./EditorImagePopover";
import { EditorLinkPopover } from "./EditorLinkPopover";
import type { BasicBlock } from "./EditorSlashMenu";
import { EditorSlashMenu } from "./EditorSlashMenu";
import { findBrokenWikilinks } from "./inserts/brokenLinks";
import { CitationDialog } from "./inserts/CitationDialog";
import { DecisionDialog } from "./inserts/DecisionDialog";
import { FactDialog } from "./inserts/FactDialog";
import { MentionMenu } from "./inserts/MentionMenu";
import {
  type MentionCategory,
  type MentionItem,
  toMentionItem,
} from "./inserts/mentionCatalog";
import { RelatedDialog } from "./inserts/RelatedDialog";
import type { SlashAction } from "./inserts/types";
import { useEditorInserts } from "./useEditorInserts";
import { useSlashMenu } from "./useSlashMenu";
import { useWikiTiptapEditor } from "./useWikiTiptapEditor";

export interface TiptapWikiEditorProps {
  /** Current markdown source from the controller. */
  content: string;
  /** Called when the user edits — receives canonical markdown with
   *  `[[wikilink]]` syntax preserved. */
  onChange: (markdown: string) => void;
  /** Returns true when a wikilink slug points at an existing article. */
  resolver?: (slug: string) => boolean;
  /** Catalog used by the mention picker + broken-link detection. */
  catalog?: WikiCatalogEntry[];
  /** Id of the visible label that names this editor (rendered by the parent
   *  `WikiEditor`). Wired to the contenteditable via `aria-labelledby` so the
   *  ProseMirror surface has an accessible name + textbox role. */
  labelId?: string;
}

interface FloatingPosition {
  top: number;
  left: number;
}

interface LinkPopoverState {
  position: FloatingPosition;
  initialUrl: string;
}

/** Run a basic-block command. Split out to keep the component lean. */
function runBasicBlock(ed: Editor, block: BasicBlock): void {
  const chain = ed.chain().focus();
  switch (block) {
    case "text":
      chain.setParagraph().run();
      break;
    case "h1":
      chain.toggleHeading({ level: 1 }).run();
      break;
    case "h2":
      chain.toggleHeading({ level: 2 }).run();
      break;
    case "h3":
      chain.toggleHeading({ level: 3 }).run();
      break;
    case "bullet":
      chain.toggleBulletList().run();
      break;
    case "ordered":
      chain.toggleOrderedList().run();
      break;
    case "check":
      chain.toggleTaskList().run();
      break;
    case "code":
      chain.toggleCodeBlock().run();
      break;
    case "quote":
      chain.toggleBlockquote().run();
      break;
    case "divider":
      chain.setHorizontalRule().run();
      break;
    case "table":
      chain.insertTable({ rows: 3, cols: 3, withHeaderRow: true }).run();
      break;
    case "image":
      // Handled by the parent (opens the image popover); never reached here.
      break;
  }
}

/** Delete the `/query` trigger text so a command doesn't leave the slash in. */
function deleteSlashTrigger(ed: Editor, query: string): void {
  const { from } = ed.state.selection;
  const start = from - query.length - 1;
  if (start < 0) return;
  ed.chain().focus().deleteRange({ from: start, to: from }).run();
}

/** Viewport coords just below the caret, for caret-anchored floating UI. */
function caretPosition(ed: Editor): FloatingPosition {
  const { from } = ed.state.selection;
  const coords = ed.view.coordsAtPos(from);
  return { top: coords.bottom + 8, left: coords.left };
}

export default function TiptapWikiEditor({
  content,
  onChange,
  resolver,
  catalog,
  labelId,
}: TiptapWikiEditorProps): React.ReactElement {
  const resolve = useCallback(
    (slug: string) => (resolver ? resolver(slug) : true),
    [resolver],
  );

  const mentionItems = useMemo<MentionItem[]>(() => {
    if (!catalog) return [];
    const out: MentionItem[] = [];
    for (const entry of catalog) {
      const item = toMentionItem(entry);
      if (item) out.push(item);
    }
    return out;
  }, [catalog]);

  const [linkPopover, setLinkPopover] = useState<LinkPopoverState | null>(null);
  const [imagePopover, setImagePopover] = useState<FloatingPosition | null>(
    null,
  );
  // Polite live-region copy announced after a successful insert. Cleared
  // immediately is unnecessary — overwriting on the next insert is enough for
  // AT, and a stale string is silent until then.
  const [announcement, setAnnouncement] = useState("");

  const openLinkPopover = useCallback((ed: Editor) => {
    const { from, to } = ed.state.selection;
    if (from === to) return;
    const existing = ed.getAttributes("link").href;
    setLinkPopover({
      position: caretPosition(ed),
      initialUrl: typeof existing === "string" ? existing : "",
    });
  }, []);

  const { editor, editorRef, emit, getLastEmitted } = useWikiTiptapEditor({
    content,
    onChange,
    resolve,
    mentionItems,
    onSetLink: openLinkPopover,
    labelId,
  });

  const inserts = useEditorInserts({
    getEditor: () => editorRef.current,
    resolver: resolve,
    onChange: emit,
    getLastEmitted,
  });

  // The slash watcher is suspended while a dialog is open so its keydown
  // handler doesn't fight the dialog's own inputs.
  const slash = useSlashMenu(inserts.dialog === null ? editor : null);

  // Wiki-link / agent-mention / task-ref slash actions open the mention
  // picker; stash which bucket to surface so it defaults correctly.
  const [pickerCategory, setPickerCategory] = useState<MentionCategory | null>(
    null,
  );

  const handleSlashBasic = useCallback(
    (block: BasicBlock) => {
      if (!editor) return;
      deleteSlashTrigger(editor, slash.query);
      slash.reset();
      if (block === "image") {
        setImagePopover(caretPosition(editor));
        return;
      }
      runBasicBlock(editor, block);
    },
    [editor, slash],
  );

  const handleSlashAction = useCallback(
    (action: SlashAction) => {
      if (!editor) return;
      deleteSlashTrigger(editor, slash.query);
      slash.reset();
      dispatchSlashAction(action, {
        openDialog: inserts.openDialog,
        openMentionPicker: (category) => {
          setPickerCategory(category);
          inserts.openDialog("mention-picker");
        },
      });
    },
    [editor, slash, inserts],
  );

  const onMentionPick = useCallback(
    (item: MentionItem) => {
      editorRef.current
        ?.chain()
        .focus()
        .insertContent([
          {
            type: "text",
            text: item.title,
            marks: [{ type: "wikiLink", attrs: { slug: item.slug } }],
          },
          { type: "text", text: " " },
        ])
        .run();
      inserts.closeDialog();
      setPickerCategory(null);
      setAnnouncement(`Linked ${item.title}`);
    },
    [inserts, editorRef],
  );

  // Wrap the insert-dialog confirm handlers so a successful insert is announced
  // to the polite live region. Each closes its dialog via the inserts hook.
  const handleCitationConfirm = useCallback(
    (built: Parameters<typeof inserts.insertCitation>[0]) => {
      inserts.insertCitation(built);
      setAnnouncement("Citation added");
    },
    [inserts],
  );
  const appendAndAnnounce = useCallback(
    (message: string) => (block: string) => {
      inserts.appendBlock(block);
      setAnnouncement(message);
    },
    [inserts],
  );

  const brokenLinks = useMemo(() => {
    if (!catalog) return [];
    return findBrokenWikilinks(content, resolve);
  }, [catalog, content, resolve]);

  const applyLink = useCallback(
    (url: string) => {
      editorRef.current
        ?.chain()
        .focus()
        .extendMarkRange("link")
        .setLink({ href: url })
        .run();
      setLinkPopover(null);
    },
    [editorRef],
  );

  const removeLink = useCallback(() => {
    editorRef.current
      ?.chain()
      .focus()
      .extendMarkRange("link")
      .unsetLink()
      .run();
    setLinkPopover(null);
  }, [editorRef]);

  const insertImage = useCallback(
    (payload: { src: string; alt: string }) => {
      editorRef.current
        ?.chain()
        .focus()
        .setImage({ src: payload.src, alt: payload.alt })
        .run();
      setImagePopover(null);
    },
    [editorRef],
  );

  return (
    <div className="wk-tiptap-editor" data-testid="wk-tiptap-editor">
      {brokenLinks.length > 0 ? (
        <div
          className="wk-editor-banner wk-editor-banner--warn"
          role="alert"
          data-testid="wk-tiptap-broken-links"
        >
          <strong>Unresolved wikilinks:</strong>{" "}
          {brokenLinks.map((b) => `[[${b.slug}]]`).join(", ")}. Save still
          succeeds, but these render broken until the target exists.
        </div>
      ) : null}

      <div
        className="sr-only"
        role="status"
        aria-live="polite"
        data-testid="wk-tiptap-announcer"
      >
        {announcement}
      </div>

      <EditorContent
        editor={editor}
        className="wk-tiptap-editor__content"
        data-testid="wk-tiptap-content"
      />

      <EditorBubbleMenu
        editor={editor}
        onRequestLink={() => {
          if (editor) openLinkPopover(editor);
        }}
      />

      {slash.open && editor ? (
        <EditorSlashMenu
          query={slash.query}
          position={slash.position}
          activeIdx={slash.activeIdx}
          setActiveIdx={slash.setActiveIdx}
          onSelectBasic={handleSlashBasic}
          onSelectAction={handleSlashAction}
          onClose={slash.reset}
          editorDom={editor.view.dom as HTMLElement}
        />
      ) : null}

      {inserts.dialog === "mention-picker" ? (
        <MentionMenu
          items={mentionItems}
          query=""
          position={{ top: 120, left: 120 }}
          categoryFilter={pickerCategory}
          heading="Link a page"
          activeDescendantTarget={
            editor ? (editor.view.dom as HTMLElement) : null
          }
          onSelect={onMentionPick}
          onClose={() => {
            inserts.closeDialog();
            setPickerCategory(null);
          }}
        />
      ) : null}

      {inserts.dialog === "citation" ? (
        <CitationDialog
          currentMarkdown={content}
          onConfirm={handleCitationConfirm}
          onCancel={inserts.closeDialog}
        />
      ) : null}
      {inserts.dialog === "fact" ? (
        <FactDialog
          onConfirm={appendAndAnnounce("Fact block inserted")}
          onCancel={inserts.closeDialog}
        />
      ) : null}
      {inserts.dialog === "decision" ? (
        <DecisionDialog
          onConfirm={appendAndAnnounce("Decision block inserted")}
          onCancel={inserts.closeDialog}
        />
      ) : null}
      {inserts.dialog === "related" ? (
        <RelatedDialog
          items={mentionItems}
          onConfirm={appendAndAnnounce("Related pages inserted")}
          onCancel={inserts.closeDialog}
        />
      ) : null}

      {linkPopover ? (
        <EditorLinkPopover
          position={linkPopover.position}
          initialUrl={linkPopover.initialUrl}
          onApply={applyLink}
          onRemove={linkPopover.initialUrl ? removeLink : undefined}
          onCancel={() => setLinkPopover(null)}
        />
      ) : null}
      {imagePopover ? (
        <EditorImagePopover
          position={imagePopover}
          onInsert={insertImage}
          onCancel={() => setImagePopover(null)}
        />
      ) : null}
    </div>
  );
}

/** Route a WUPHF slash action to its dialog or the mention picker. */
function dispatchSlashAction(
  action: SlashAction,
  handlers: {
    openDialog: (next: "citation" | "fact" | "decision" | "related") => void;
    openMentionPicker: (category: MentionCategory) => void;
  },
): void {
  switch (action) {
    case "citation":
      handlers.openDialog("citation");
      break;
    case "fact":
      handlers.openDialog("fact");
      break;
    case "decision":
      handlers.openDialog("decision");
      break;
    case "related":
      handlers.openDialog("related");
      break;
    case "wiki-link":
      handlers.openMentionPicker("pages");
      break;
    case "agent-mention":
      handlers.openMentionPicker("agents");
      break;
    case "task-ref":
      handlers.openMentionPicker("tasks");
      break;
  }
}
