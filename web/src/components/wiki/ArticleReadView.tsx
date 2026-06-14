import { useMemo, useRef } from "react";
import ReactMarkdown from "react-markdown";
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

/**
 * The Wikipedia-parity article read view.
 *
 * Takes the raw markdown body and renders:
 * - a right-floating infobox lifted from B2's `## Summary` definition list
 * - `[^n]` citations as superscript `[1]` markers with a hover popover,
 *   and the footnote block relabeled "References" with backref carets
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
      buildMarkdownComponents({
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
      className="wk-article-body"
      data-testid="wk-article-body"
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
