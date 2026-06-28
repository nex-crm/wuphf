import {
  isValidElement,
  type ReactElement,
  type ReactNode,
  useMemo,
  useRef,
} from "react";
import ReactMarkdown, { type Components } from "react-markdown";
import type { PluggableList } from "unified";

import type { RichArtifactDetail } from "../../api/richArtifacts";
import { keyedByOccurrence } from "../../lib/reactKeys";
import {
  extractRichArtifactIds,
  stripStandaloneRichArtifactReferenceLines,
} from "../../lib/richArtifactReferences";
import {
  buildMarkdownComponents,
  buildRehypePlugins,
  buildRemarkPlugins,
} from "../../lib/wikiMarkdownConfig";
import RichArtifactEmbed from "../rich-artifacts/RichArtifactEmbed";
import ArticleHoverPreviews, {
  type HoverPreviewContent,
} from "./ArticleHoverPreviews";
import ArticleInfobox from "./ArticleInfobox";
import { prepareArticleMarkdown } from "./articleContent";
import Hatnote from "./Hatnote";
import MermaidBlock from "./MermaidBlock";
import "../../styles/wiki-reader.css";

/**
 * The Wikipedia-parity article read view.
 *
 * Takes the raw markdown body and renders:
 * - a right-floating infobox lifted from B2's `## Summary` definition list
 * - standard GFM `[^n]` footnotes (rendered natively via remark-gfm) with the
 *   footnote block relabeled "References" and backref carets
 * - blue/red wikilinks with hover page-preview cards
 * - a hatnote (italic, muted) when the body carries a generated-article
 *   marker comment — instead of the raw HTML comment leaking or hiding
 * - H2 sections with the `[ edit ]` affordance when `onEditSection` is set
 */

interface ArticleReadViewProps {
  /** Raw markdown body (may include marker comments, Summary, footnotes). */
  content: string;
  /** Article title — used as the infobox header band. */
  title: string;
  /** Canonical article path for relative link/image resolution. */
  articlePath: string;
  /** Wikilink existence resolver (blue vs redlink). */
  resolver: (slug: string) => boolean;
  onNavigate?: (slug: string) => void;
  /** Opens the editor; wires Wikipedia's per-section [ edit ] link. */
  onEditSection?: () => void;
  /** Promoted visual artifact (fetched by the article shell). */
  visualArtifact?: RichArtifactDetail | null;
  /** Inline `visual-artifact:<id>` embeds (fetched by the article shell). */
  inlineArtifacts?: RichArtifactDetail[];
  /** Injectable hover-preview fetcher for tests/Storybook. */
  fetchPreview?: (slug: string) => Promise<HoverPreviewContent | null>;
  /** Hover-intent delay override (tests pass 0). */
  previewDelayMs?: number;
}

export default function ArticleReadView({
  content,
  title,
  articlePath,
  resolver,
  onNavigate,
  onEditSection,
  visualArtifact = null,
  inlineArtifacts = [],
  fetchPreview,
  previewDelayMs,
}: ArticleReadViewProps) {
  const bodyRef = useRef<HTMLDivElement | null>(null);

  const prepared = useMemo(() => prepareArticleMarkdown(content), [content]);

  const remarkPlugins: PluggableList = useMemo(
    () => buildRemarkPlugins(resolver),
    [resolver],
  );
  const rehypePlugins: PluggableList = useMemo(() => buildRehypePlugins(), []);
  const markdownComponents = useMemo(
    () =>
      buildReadViewComponents({
        resolver,
        onNavigate,
        articlePath,
        onEditSection,
      }),
    [resolver, onNavigate, articlePath, onEditSection],
  );
  // The GFM footnote section becomes the article's References section —
  // visible heading (mdast-to-hast hides it with sr-only by default).
  const remarkRehypeOptions = useMemo(
    () => ({
      footnoteLabel: "References",
      footnoteLabelTagName: "h2",
      footnoteLabelProperties: {},
    }),
    [],
  );

  // The body may contain standalone `visual-artifact:<id>` marker lines the
  // agent hand-wrote. Strip them so they never render as raw text, then
  // embed each referenced artifact inline. Dedupe the promoted artifact
  // against the body's inline markers (see WikiArticle for the rationale —
  // the two fetches settle independently, so we key off the synchronous
  // content markers to avoid a transient double-render).
  const renderedContent = stripStandaloneRichArtifactReferenceLines(
    prepared.markdown,
  );
  const referencedIds = new Set(extractRichArtifactIds(content));
  const showPromoted =
    visualArtifact !== null && !referencedIds.has(visualArtifact.artifact.id);

  return (
    <div
      className={
        prepared.compiled ? "wk-article-body wiki-reader" : "wk-article-body"
      }
      data-testid="wk-article-body"
      data-compiled={prepared.compiled ? "true" : undefined}
      ref={bodyRef}
    >
      {prepared.generated ? (
        <Hatnote>
          This article is auto-generated from team activity. See the commit
          history for the full trail.
        </Hatnote>
      ) : null}
      {prepared.infobox ? (
        <ArticleInfobox
          title={title}
          rows={prepared.infobox}
          resolver={resolver}
          onNavigate={onNavigate}
          articlePath={articlePath}
        />
      ) : null}
      {showPromoted && visualArtifact ? (
        <RichArtifactEmbed
          title={visualArtifact.artifact.title}
          html={visualArtifact.html}
        />
      ) : null}
      {keyedByOccurrence(inlineArtifacts, (detail) => detail.artifact.id).map(
        ({ key, value: detail }) => (
          <RichArtifactEmbed
            key={key}
            title={detail.artifact.title}
            html={detail.html}
          />
        ),
      )}
      <ReactMarkdown
        remarkPlugins={remarkPlugins}
        rehypePlugins={rehypePlugins}
        remarkRehypeOptions={remarkRehypeOptions}
        components={markdownComponents}
      >
        {renderedContent}
      </ReactMarkdown>
      <ArticleHoverPreviews
        containerRef={bodyRef}
        fetchPreview={fetchPreview}
        delayMs={previewDelayMs}
      />
    </div>
  );
}

interface ReadViewComponentOptions {
  resolver: (slug: string) => boolean;
  onNavigate?: (slug: string) => void;
  articlePath: string;
  onEditSection?: () => void;
}

/**
 * Compose the shared wiki markdown components, then layer one read-view-only
 * override on top:
 *  - `pre`: intercept fenced ```mermaid blocks and render them as diagrams.
 *
 * GFM footnotes (`[^n]`) render natively through remark-gfm in the shared
 * pipeline, so the read view no longer wires any custom citation handling.
 */
function buildReadViewComponents(
  options: ReadViewComponentOptions,
): Partial<Components> {
  const base = buildMarkdownComponents(options);
  return {
    ...base,
    pre: (props): ReactElement => {
      const mermaid = mermaidSourceFromPre(props.children);
      if (mermaid !== null) return <MermaidBlock code={mermaid} />;
      return <pre {...props} />;
    },
  };
}

/**
 * Pull the raw source out of a ```mermaid fenced block. react-markdown renders
 * a fenced block as `<pre><code class="language-mermaid">…</code></pre>`, so we
 * inspect the single code child. Returns null for any other `<pre>` content
 * (plain code blocks fall through to the default renderer).
 */
function mermaidSourceFromPre(children: ReactNode): string | null {
  if (!isValidElement(children)) return null;
  const codeProps = children.props as {
    className?: string;
    children?: ReactNode;
  };
  const className = codeProps.className ?? "";
  if (!/(^|\s)language-mermaid(\s|$)/.test(className)) return null;
  const raw = codeProps.children;
  const text = typeof raw === "string" ? raw : extractText(raw);
  return text.replace(/\n$/, "");
}

/** Flatten a code element's children down to its text content. */
function extractText(node: ReactNode): string {
  if (typeof node === "string") return node;
  if (Array.isArray(node)) return node.map(extractText).join("");
  if (isValidElement(node)) {
    return extractText((node.props as { children?: ReactNode }).children);
  }
  return "";
}
