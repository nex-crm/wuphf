/**
 * Tiptap extension set for the WUPHF wiki editor.
 *
 * A pure config module — no JSX, no React. `buildWikiEditorExtensions` returns
 * the static core set (StarterKit + nodes/marks the editor always needs). The
 * `@`-mention extension is intentionally NOT included here: it needs runtime
 * config (a catalog `getItems` plus a React popup `render`), so the editor
 * component appends `buildWikiMention(...)` from `./mention` to this array.
 *
 * The language set, math config, and StarterKit overrides are tuned for the
 * wiki editor, while the WikiLink mark and the design-token styling are
 * WUPHF's own.
 */

import { MathExtension } from "@aarkue/tiptap-math-extension";
import type { Extensions } from "@tiptap/core";
import { CodeBlockLowlight } from "@tiptap/extension-code-block-lowlight";
import { Color } from "@tiptap/extension-color";
import { Highlight } from "@tiptap/extension-highlight";
import { Image } from "@tiptap/extension-image";
import { Link } from "@tiptap/extension-link";
import { Placeholder } from "@tiptap/extension-placeholder";
import { Subscript } from "@tiptap/extension-subscript";
import { Superscript } from "@tiptap/extension-superscript";
import { Table } from "@tiptap/extension-table";
import { TableCell } from "@tiptap/extension-table-cell";
import { TableHeader } from "@tiptap/extension-table-header";
import { TableRow } from "@tiptap/extension-table-row";
import { TaskItem } from "@tiptap/extension-task-item";
import { TaskList } from "@tiptap/extension-task-list";
import { TextAlign } from "@tiptap/extension-text-align";
import { TextStyle } from "@tiptap/extension-text-style";
import { Underline } from "@tiptap/extension-underline";
import StarterKit from "@tiptap/starter-kit";
import bash from "highlight.js/lib/languages/bash";
import css from "highlight.js/lib/languages/css";
import go from "highlight.js/lib/languages/go";
import javascript from "highlight.js/lib/languages/javascript";
import json from "highlight.js/lib/languages/json";
import markdown from "highlight.js/lib/languages/markdown";
import python from "highlight.js/lib/languages/python";
import rust from "highlight.js/lib/languages/rust";
import shell from "highlight.js/lib/languages/shell";
import sql from "highlight.js/lib/languages/sql";
import typescript from "highlight.js/lib/languages/typescript";
import xml from "highlight.js/lib/languages/xml";
import yaml from "highlight.js/lib/languages/yaml";
import { createLowlight } from "lowlight";

import { WikiLink, type WikiLinkResolver } from "./wikiLinkMark";

const DEFAULT_PLACEHOLDER = "Start writing, or press '/' for commands…";

/**
 * Curated language set. `typescript` covers TSX (highlight.js registers `tsx`
 * as an alias) and `xml` covers HTML. The fenced `fact` / `decision` blocks
 * the WUPHF inserts emit are unknown languages — lowlight renders them as
 * plain text, which is the intended fallback.
 */
function buildLowlight() {
  const lowlight = createLowlight();
  lowlight.register({
    bash,
    css,
    go,
    javascript,
    json,
    markdown,
    python,
    rust,
    shell,
    sql,
    typescript,
    xml,
    yaml,
  });
  return lowlight;
}

export interface WikiExtensionOptions {
  /** Empty-document placeholder. */
  placeholder?: string;
  /**
   * Returns true when a wikilink slug points at an existing article. Drives
   * the `wk-broken` styling. Defaults to treating every slug as resolvable.
   */
  resolver?: (slug: string) => boolean;
  /**
   * Invoked when the user presses the link shortcut (Mod-e) over a non-empty
   * selection. The editor component supplies its own link-URL popover here so
   * this config module owns no blocking browser UI. When omitted, the
   * shortcut is a no-op (the chrome surface, not the schema, owns link entry).
   */
  onSetLink?: () => void;
}

/**
 * Build the core wiki editor extension set.
 *
 * StarterKit (v3) bundles `codeBlock`, `link`, and `underline`; all three are
 * disabled here because we register standalone, customized versions
 * (CodeBlockLowlight, Link, Underline) — leaving the bundled copies on would
 * trigger Tiptap's duplicate-extension warning.
 */
export function buildWikiEditorExtensions(
  opts?: WikiExtensionOptions,
): Extensions {
  const placeholder = opts?.placeholder ?? DEFAULT_PLACEHOLDER;
  const resolver: WikiLinkResolver = opts?.resolver ?? (() => true);
  const onSetLink = opts?.onSetLink;

  return [
    StarterKit.configure({
      heading: { levels: [1, 2, 3, 4] },
      codeBlock: false,
      link: false,
      underline: false,
    }),
    CodeBlockLowlight.configure({
      lowlight: buildLowlight(),
      HTMLAttributes: { class: "wk-editor-code-block" },
    }),
    Placeholder.configure({ placeholder }),
    Image.configure({
      allowBase64: false,
      HTMLAttributes: { class: "wk-editor-image" },
    }),
    Table.configure({
      resizable: true,
      lastColumnResizable: false,
      HTMLAttributes: { class: "wk-editor-table" },
    }),
    TableRow,
    TableHeader,
    TableCell,
    TaskList.configure({ HTMLAttributes: { class: "wk-editor-task-list" } }),
    TaskItem.configure({ nested: true }),
    Link.configure({
      openOnClick: false,
      HTMLAttributes: { class: "wk-editor-link" },
    }).extend({
      // Wikilinks have their own mark — never let the generic Link mark claim
      // a `data-wikilink` anchor.
      parseHTML() {
        return [{ tag: 'a[href]:not([data-wikilink="true"])' }];
      },
      // Mod-k is owned by the Cmd+K search palette app-wide. Bind the link
      // shortcut to Mod-e instead so it never collides. The actual URL-entry
      // UI lives in the editor component (via `onSetLink`); this module only
      // owns the keybinding, never a blocking browser prompt.
      addKeyboardShortcuts() {
        return {
          "Mod-e": () => {
            const { from, to } = this.editor.state.selection;
            if (from === to) return false;
            if (!onSetLink) return false;
            onSetLink();
            return true;
          },
        };
      },
    }),
    TextStyle,
    Color,
    Highlight.configure({ multicolor: true }).extend({
      // The bubble menu offers highlight swatches by mouse; give keyboard
      // users an equivalent. Mod-Shift-h toggles a default (uncoloured)
      // highlight over the selection. Mod-h is avoided because some browsers
      // reserve it for History; Shift keeps the chord unambiguous.
      addKeyboardShortcuts() {
        return {
          "Mod-Shift-h": () => this.editor.commands.toggleHighlight(),
        };
      },
    }),
    Underline,
    Subscript,
    Superscript,
    TextAlign.configure({
      types: ["heading", "paragraph"],
      alignments: ["left", "center", "right", "justify"],
    }),
    MathExtension.configure({
      evaluation: false,
      addInlineMath: true,
      delimiters: "dollar",
      renderTextMode: "raw-latex",
    }),
    WikiLink.configure({ resolver }),
  ];
}
