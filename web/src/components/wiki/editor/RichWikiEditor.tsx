/**
 * Milkdown-backed rich editor for wiki articles.
 *
 * Lazy-loaded behind the per-article "Rich" toggle in `WikiEditor`. Keeps
 * the default textarea path zero-cost; only users who opt into rich mode
 * pay the ~100kB-gzipped Milkdown bundle.
 *
 * Round-trip contract:
 *   - On parse, we register `wikiLinkRemarkPlugin` so `[[slug]]` becomes a
 *     standard `link` mdast node with `url = "#/wiki/<slug>"`. ProseMirror
 *     stores that as a regular link mark and round-trips it cleanly.
 *   - On serialize, Milkdown's commonmark/gfm preset emits standard
 *     markdown. `postProcessWikilinks` rewrites `[Display](#/wiki/slug)`
 *     back to `[[slug]]` (or `[[slug|Display]]`) so the saved markdown
 *     matches what the textarea editor would have produced.
 *
 * State ownership: controller (in `useWikiEditorController`) holds the
 * source-of-truth markdown. This component is a controlled-ish view: it
 * receives `content` and pushes changes via `onChange`. External content
 * updates (conflict reload, draft restore) are pushed back into Milkdown
 * via `replaceAll`; we de-dup against the last value we emitted to avoid
 * an edit -> serialize -> reset feedback loop.
 *
 * Inserts (PR 6): The editor mounts a custom ProseMirror plugin that
 * watches for `/` and `@` triggers. When active, React renders a floating
 * SlashMenu / MentionMenu against the trigger position. Slash actions
 * route to dialog components (citation / fact / decision / related) or
 * the mention picker; on confirm, markdown is inserted at the caret.
 */

import { useCallback, useEffect, useMemo, useRef } from "react";
import {
  defaultValueCtx,
  Editor,
  prosePluginsCtx,
  remarkPluginsCtx,
  remarkStringifyOptionsCtx,
  rootCtx,
} from "@milkdown/core";
import { listener, listenerCtx } from "@milkdown/plugin-listener";
import { commonmark } from "@milkdown/preset-commonmark";
import { gfm } from "@milkdown/preset-gfm";
import type { EditorView } from "@milkdown/prose/view";
import { Milkdown, MilkdownProvider, useEditor } from "@milkdown/react";
import { replaceAll } from "@milkdown/utils";

import type { WikiCatalogEntry } from "../../../api/wiki";
import { wikiLinkRemarkPlugin } from "../../../lib/wikilink";
import { STRINGIFY_DEFAULTS } from "../../../lib/wikilinkStringify";
import { findBrokenWikilinks } from "./inserts/brokenLinks";
import { CitationDialog } from "./inserts/CitationDialog";
import { DecisionDialog } from "./inserts/DecisionDialog";
import { FactDialog } from "./inserts/FactDialog";
import { MentionMenu } from "./inserts/MentionMenu";
import { type MentionItem, toMentionItem } from "./inserts/mentionCatalog";
import { RelatedDialog } from "./inserts/RelatedDialog";
import { SlashMenu } from "./inserts/SlashMenu";
import { buildTriggerPlugin, type TriggerState } from "./inserts/triggerPlugin";
import { useInsertController } from "./inserts/useInsertController";
import { postProcessWikilinks } from "./wikilinkPostProcess";

export interface RichWikiEditorProps {
  /** Current markdown source from the controller. */
  content: string;
  /** Called when the user edits — receives canonical markdown with
   *  `[[wikilink]]` syntax restored. */
  onChange: (next: string) => void;
  /** Catalog used by the mention picker + broken-link detection. The
   *  same list `WikiEditor` already passes to the preview pane. */
  catalog?: WikiCatalogEntry[];
}

interface InnerProps {
  /** Current markdown source from the controller. */
  content: string;
  /** Called when the user edits — receives canonical markdown with
   *  `[[wikilink]]` syntax restored. */
  onChange: (next: string) => void;
  /** Latest TriggerState emitted by the ProseMirror plugin. */
  setTrigger: (next: TriggerState | null) => void;
  /** Stash the live ProseMirror view so the controller can dispatch
   *  insert transactions from outside the editor closure. */
  setView: (view: EditorView | null) => void;
}

function RichWikiEditorInner({
  content,
  onChange,
  setTrigger,
  setView,
}: InnerProps) {
  const onChangeRef = useRef(onChange);
  useEffect(() => {
    onChangeRef.current = onChange;
  }, [onChange]);

  const setTriggerRef = useRef(setTrigger);
  useEffect(() => {
    setTriggerRef.current = setTrigger;
  }, [setTrigger]);

  const setViewRef = useRef(setView);
  useEffect(() => {
    setViewRef.current = setView;
  }, [setView]);

  const lastEmittedRef = useRef<string>(content);

  const { get } = useEditor((root) =>
    Editor.make()
      .config((ctx) => {
        ctx.set(rootCtx, root);
        ctx.set(defaultValueCtx, content);
        ctx.update(remarkPluginsCtx, (prev) => [
          ...prev,
          {
            // The resolver always returns true here because Milkdown only
            // uses this plugin for parsing — it strips the `data-broken`
            // class once content reaches ProseMirror anyway. Broken-link
            // surfacing happens in the read-only view.
            plugin: wikiLinkRemarkPlugin(() => true),
            options: {},
          },
        ]);
        ctx.update(remarkStringifyOptionsCtx, (prev) => ({
          ...prev,
          ...STRINGIFY_DEFAULTS,
        }));
        // Mount the trigger-watching ProseMirror plugin. Stable refs so
        // the closure can reach the latest React setters without
        // rebuilding the editor when props change. The plugin's `view`
        // hook calls `onViewReady` on mount/destroy so the parent can
        // dispatch insert transactions without polling.
        ctx.update(prosePluginsCtx, (prev) => [
          ...prev,
          buildTriggerPlugin({
            triggers: ["/", "@"],
            onChange: (next) => setTriggerRef.current(next),
            onViewReady: (view) => setViewRef.current(view),
          }),
        ]);
        ctx.get(listenerCtx).markdownUpdated((_, md, prev) => {
          if (md === prev) return;
          const canonical = postProcessWikilinks(md);
          // The external-content effect sets `lastEmittedRef.current = content`
          // *before* dispatching `replaceAll`, so the listener fires with the
          // value we just pushed. Skip emitting onChange in that case — the
          // canonical form is identical to what the controller already holds,
          // and a redundant onChange would race with autosave/draft state.
          if (canonical === lastEmittedRef.current) return;
          lastEmittedRef.current = canonical;
          onChangeRef.current(canonical);
        });
      })
      .use(commonmark)
      .use(gfm)
      .use(listener),
  );

  // Push external content changes (conflict reload, draft restore) back
  // into Milkdown. Skip when `content` matches what we just emitted —
  // otherwise every keystroke would trigger a self-replaceAll loop.
  //
  // Mark the value as "seen" *before* dispatching the transaction. The
  // markdownUpdated listener fires synchronously inside `editor.action`
  // and writes the canonical (post-processed) markdown into the ref; if
  // we wrote `content` afterward, we'd overwrite that canonical value
  // with the raw input and the next render would re-trigger replaceAll,
  // resetting the ProseMirror cursor on every external update.
  useEffect(() => {
    if (content === lastEmittedRef.current) return;
    const editor = get();
    if (!editor) return;
    lastEmittedRef.current = content;
    editor.action(replaceAll(content));
  }, [content, get]);

  return <Milkdown />;
}

export default function RichWikiEditor({
  content,
  onChange,
  catalog,
}: RichWikiEditorProps) {
  const viewRef = useRef<EditorView | null>(null);
  const setView = useCallback((view: EditorView | null) => {
    viewRef.current = view;
  }, []);

  // Latest content snapshot for the controller — kept in sync with the
  // `content` prop so the citation appender always works against the
  // canonical bytes.
  const contentRef = useRef(content);
  useEffect(() => {
    contentRef.current = content;
  }, [content]);

  // Wrap the raw onChange so editor-originated edits also sync contentRef.
  // Without this, plain typing bypasses the ref until the controlled
  // `content` prop round-trips through the parent, leaving a window where
  // a concurrent confirm reads stale markdown.
  const handleEditorChange = useCallback(
    (next: string) => {
      contentRef.current = next;
      onChange(next);
    },
    [onChange],
  );

  const insertController = useInsertController({
    getView: () => viewRef.current,
    pushContent: (next) => {
      contentRef.current = next;
      onChange(next);
    },
    getCurrentContent: () => contentRef.current,
  });
  // The controller owns the trigger state so a single source feeds both
  // the floating menus and the action handlers.
  const { trigger, setTrigger } = insertController;

  const mentionItems = useMemo<MentionItem[]>(() => {
    if (!catalog) return [];
    const out: MentionItem[] = [];
    for (const entry of catalog) {
      const item = toMentionItem(entry);
      if (item) out.push(item);
    }
    return out;
  }, [catalog]);

  // Precompute the set of catalog paths once per catalog change so the
  // broken-link resolver is O(1) per lookup. Each entry contributes both
  // its raw path and the `${path}.md` variant so a slug typed without
  // the `.md` extension (the common case) still resolves.
  const catalogPathSet = useMemo<Set<string> | null>(() => {
    if (!catalog) return null;
    const set = new Set<string>();
    for (const e of catalog) {
      set.add(e.path);
      set.add(`${e.path}.md`);
    }
    return set;
  }, [catalog]);

  const resolver = useCallback(
    (slug: string) => {
      if (!catalogPathSet) return true; // No catalog -> no detection signal.
      return catalogPathSet.has(slug) || catalogPathSet.has(`${slug}.md`);
    },
    [catalogPathSet],
  );

  const brokenLinks = useMemo(
    () => (catalogPathSet ? findBrokenWikilinks(content, resolver) : []),
    [catalogPathSet, content, resolver],
  );

  const showSlashMenu =
    trigger?.trigger === "/" && insertController.dialog === null;
  const showMentionMenu =
    trigger?.trigger === "@" && insertController.dialog === null;

  return (
    <MilkdownProvider>
      <div className="wk-rich-editor-host">
        <RichWikiEditorInner
          content={content}
          onChange={handleEditorChange}
          setTrigger={setTrigger}
          setView={setView}
        />
        {brokenLinks.length > 0 ? (
          <div
            className="wk-editor-banner wk-editor-banner--warn"
            role="alert"
            data-testid="wk-rich-broken-links"
          >
            <strong>Unresolved wikilinks:</strong>{" "}
            {brokenLinks.map((b) => `[[${b.slug}]]`).join(", ")}. Save will
            still succeed, but these links will render as broken until the
            target page exists.
          </div>
        ) : null}
        {showSlashMenu ? (
          <SlashMenu
            query={trigger.query}
            position={{ top: trigger.rect.bottom + 4, left: trigger.rect.left }}
            onSelect={insertController.onSlashSelect}
            onClose={insertController.closeTrigger}
          />
        ) : null}
        {showMentionMenu ? (
          <MentionMenu
            items={mentionItems}
            query={trigger.query}
            position={{ top: trigger.rect.bottom + 4, left: trigger.rect.left }}
            onSelect={insertController.onMentionSelect}
            onClose={insertController.closeTrigger}
          />
        ) : null}
        {insertController.dialog === "mention-picker" &&
        insertController.mentionPickerState ? (
          <MentionMenu
            items={mentionItems}
            query=""
            position={insertController.mentionPickerState.position}
            categoryFilter={insertController.mentionPickerState.categoryFilter}
            heading={insertController.mentionPickerState.heading}
            onSelect={insertController.onMentionSelect}
            onClose={insertController.closeDialog}
          />
        ) : null}
        {insertController.dialog === "citation" ? (
          <CitationDialog
            currentMarkdown={content}
            onConfirm={insertController.onCitationConfirm}
            onCancel={insertController.closeDialog}
          />
        ) : null}
        {insertController.dialog === "fact" ? (
          <FactDialog
            onConfirm={insertController.onFactConfirm}
            onCancel={insertController.closeDialog}
          />
        ) : null}
        {insertController.dialog === "decision" ? (
          <DecisionDialog
            onConfirm={insertController.onDecisionConfirm}
            onCancel={insertController.closeDialog}
          />
        ) : null}
        {insertController.dialog === "related" ? (
          <RelatedDialog
            items={mentionItems}
            onConfirm={insertController.onRelatedConfirm}
            onCancel={insertController.closeDialog}
          />
        ) : null}
      </div>
    </MilkdownProvider>
  );
}
