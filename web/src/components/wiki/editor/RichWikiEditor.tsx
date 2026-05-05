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
 * an edit→serialize→reset feedback loop.
 */

import { useEffect, useRef } from "react";
import {
  defaultValueCtx,
  Editor,
  remarkPluginsCtx,
  remarkStringifyOptionsCtx,
  rootCtx,
} from "@milkdown/core";
import { listener, listenerCtx } from "@milkdown/plugin-listener";
import { commonmark } from "@milkdown/preset-commonmark";
import { gfm } from "@milkdown/preset-gfm";
import { Milkdown, MilkdownProvider, useEditor } from "@milkdown/react";
import { replaceAll } from "@milkdown/utils";

import { wikiLinkRemarkPlugin } from "../../../lib/wikilink";
import { STRINGIFY_DEFAULTS } from "../../../lib/wikilinkStringify";
import { postProcessWikilinks } from "./wikilinkPostProcess";

export interface RichWikiEditorProps {
  /** Current markdown source from the controller. */
  content: string;
  /** Called when the user edits — receives canonical markdown with
   *  `[[wikilink]]` syntax restored. */
  onChange: (next: string) => void;
}

function RichWikiEditorInner({ content, onChange }: RichWikiEditorProps) {
  const onChangeRef = useRef(onChange);
  useEffect(() => {
    onChangeRef.current = onChange;
  }, [onChange]);

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

export default function RichWikiEditor(props: RichWikiEditorProps) {
  return (
    <MilkdownProvider>
      <RichWikiEditorInner {...props} />
    </MilkdownProvider>
  );
}
