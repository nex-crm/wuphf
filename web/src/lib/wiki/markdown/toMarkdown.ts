/**
 * Tiptap HTML -> wiki markdown.
 *
 * Built on turndown + turndown-plugin-gfm (tables, strikethrough, task lists),
 * with WUPHF-specific rules layered on so the editor's HTML serialises back to
 * the same markdown `wikiMarkdownToHtml` consumes:
 *
 *   - wikiLink: `<a data-wikilink="true" data-slug="S">D</a>` -> `[[S]]` or
 *     `[[S|D]]`, routed through `buildWikilink` so the emitted slug passes the
 *     same grammar validation the inserts use.
 *   - codeBlock: keep the language from `<pre><code class="language-X">` as a
 *     fenced ```X block (covers fact / decision / js identically).
 *   - taskListItem: `<li>` whose first input is a checkbox -> `- [ ]`/`- [x]`.
 *   - formatting: keep `<u>`, `<sub>`, `<sup>`, `<mark>`, and inline
 *     `style="color:…"` spans so colour / highlight / script formatting that
 *     Tiptap emits survives the trip.
 *
 * Turndown is configured atx headings, fenced code, `-` bullets so the output
 * matches the markdown the rest of the wiki surface produces.
 */

import TurndownService from "turndown";
// turndown-plugin-gfm ships no types; the import shape is a named `gfm`.
import { gfm } from "turndown-plugin-gfm";

import { buildWikilink } from "@/components/wiki/editor/inserts/markdownShapes";

/**
 * Reverse turndown's `[`/`]` escaping for the two bracket shapes we keep
 * literal: footnote citations (`\[^id\]`, `\[^id\]:`) and callout markers
 * (`\[!type\]`). Everything else stays escaped so a stray `[text]` cannot be
 * misparsed as a link on the next load.
 */
function unescapeWikiBrackets(text: string): string {
  return (
    text
      // Footnote reference / definition: \[^id\] (optionally followed by `:`).
      .replace(/\\\[\^([A-Za-z0-9_-]+)\\\]/g, "[^$1]")
      // Callout marker: \[!type\] (note, warning, tip, …).
      .replace(/\\\[!([A-Za-z]+)\\\]/g, "[!$1]")
  );
}

function createTurndown(): TurndownService {
  const service = new TurndownService({
    headingStyle: "atx",
    hr: "---",
    bulletListMarker: "-",
    codeBlockStyle: "fenced",
    fence: "```",
    emDelimiter: "*",
    strongDelimiter: "**",
  });

  // Tables, strikethrough, and the default task-list handling.
  service.use(gfm);

  // Turndown escapes every `[`/`]` to `\[`/`\]` so stray brackets can't be
  // misread as links. That would mangle two WUPHF shapes we deliberately keep
  // as literal text: GFM-style footnote citations (`[^1]`, `[^1]: …`) and
  // Obsidian callout markers (`[!note]`). Unescape just those token shapes
  // after the default escape so genuine bracket ambiguity is still handled.
  const defaultEscape = service.escape.bind(service);
  service.escape = (text: string): string =>
    unescapeWikiBrackets(defaultEscape(text));

  // Strikethrough: turndown-plugin-gfm emits a single `~`, but remark-gfm
  // renders `~~struck~~`. Override to the double-tilde form so a strike-through
  // round-trips to the exact markdown the rest of the wiki produces.
  service.addRule("strikethrough", {
    filter: (node) =>
      node.nodeName === "DEL" ||
      node.nodeName === "S" ||
      node.nodeName === "STRIKE",
    replacement: (content) => `~~${content}~~`,
  });

  // Wikilinks: emit `[[slug]]` when the display equals the slug, otherwise
  // `[[slug|Display]]`. Routed through `buildWikilink` so a malformed slug is
  // dropped to its text content rather than producing an invalid link.
  service.addRule("wikiLink", {
    filter: (node) =>
      node.nodeName === "A" &&
      (node as HTMLElement).getAttribute("data-wikilink") === "true",
    replacement: (content, node) => {
      const el = node as HTMLElement;
      const slug = el.getAttribute("data-slug") ?? "";
      const display = content.trim();
      const link = buildWikilink(slug, display);
      return link ?? display;
    },
  });

  // Fenced code blocks: keep the language info string from the `language-X`
  // class. Overrides turndown's default so ```fact / ```decision survive.
  service.addRule("fencedCodeBlock", {
    filter: (node) =>
      node.nodeName === "PRE" &&
      node.firstChild !== null &&
      node.firstChild.nodeName === "CODE",
    replacement: (_content, node) => {
      const code = (node as HTMLElement).firstChild as HTMLElement;
      const lang =
        code.getAttribute("class")?.replace(/^.*\blanguage-(\S+).*$/, "$1") ??
        "";
      const language = lang.startsWith("language-") ? "" : lang;
      // Strip a single trailing newline that the HTML carries inside <code>.
      const text = (code.textContent ?? "").replace(/\n$/, "");
      return `\n\n\`\`\`${language}\n${text}\n\`\`\`\n\n`;
    },
  });

  // Task list items: serialize the checkbox state as `- [ ]` / `- [x]`. This
  // overrides turndown-plugin-gfm's own task rule so we control the output and
  // tolerate the `disabled` attribute remark-gfm emits.
  service.addRule("taskListItem", {
    filter: (node) => {
      if (node.nodeName !== "LI") return false;
      const input = (node as HTMLElement).querySelector(
        'input[type="checkbox"]',
      );
      return input !== null;
    },
    replacement: (content, node) => {
      const input = (node as HTMLElement).querySelector(
        'input[type="checkbox"]',
      ) as HTMLInputElement | null;
      const checked = input?.hasAttribute("checked") || input?.checked === true;
      // turndown-plugin-gfm's own task rule has already turned the `<input>`
      // into a leading `[ ]` / `[x]` token inside `content`. Strip it so we do
      // not emit a doubled checkbox, then re-emit from the actual checked state.
      const text = content
        .replace(/^\s*\[[ xX]?\]\s*/, "")
        .replace(/^\s*\n+/, "")
        .replace(/\n+\s*$/, "")
        .trim();
      return `- [${checked ? "x" : " "}] ${text}\n`;
    },
  });

  // Preserve inline styled spans (text/background colour) so colour survives.
  service.addRule("styledSpan", {
    filter: (node) =>
      node.nodeName === "SPAN" && !!(node as HTMLElement).getAttribute("style"),
    replacement: (content, node) => {
      const style = (node as HTMLElement).getAttribute("style") ?? "";
      return `<span style="${style}">${content}</span>`;
    },
  });

  // Preserve <mark> highlights with any attributes Tiptap writes.
  service.addRule("mark", {
    filter: (node) => node.nodeName === "MARK",
    replacement: (content, node) => {
      const el = node as HTMLElement;
      const attrs = Array.from(el.attributes)
        .map((a) => `${a.name}="${a.value.replace(/"/g, "&quot;")}"`)
        .join(" ");
      return `<mark${attrs ? ` ${attrs}` : ""}>${content}</mark>`;
    },
  });

  // Preserve <u>, <sub>, <sup> (underline, subscript, superscript).
  for (const tag of ["u", "sub", "sup"] as const) {
    service.addRule(tag, {
      filter: (node) => node.nodeName === tag.toUpperCase(),
      replacement: (content) => `<${tag}>${content}</${tag}>`,
    });
  }

  return service;
}

const turndown = createTurndown();

/** Convert Tiptap-emitted HTML back into wiki markdown. */
export function wikiHtmlToMarkdown(html: string): string {
  return turndown.turndown(html);
}
