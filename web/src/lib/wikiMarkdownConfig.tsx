// biome-ignore-all lint/a11y/noStaticElementInteractions: Intentional wrapper/backdrop or SVG hover target; interactive child controls and keyboard paths are handled nearby.
// biome-ignore-all lint/a11y/useValidAnchor: Anchor is intercepted by the app router or markdown renderer while preserving href fallback behavior.
/**
 * Shared markdown pipeline for the wiki surface.
 *
 * Extracted so the editor's live preview renders through the exact same
 * remark/rehype plugins and component overrides as `WikiArticle`. Keep
 * this file small — it's pure config, not logic.
 */

import type { ComponentProps, ReactElement } from "react";
import type { Components } from "react-markdown";
import rehypeAutolinkHeadings from "rehype-autolink-headings";
import rehypeSlug from "rehype-slug";
import remarkGfm from "remark-gfm";
import type { PluggableList } from "unified";

import { CalloutBlockquote } from "../components/wiki/Callout";
import ImageEmbed from "../components/wiki/ImageEmbed";
import { calloutRemarkPlugin } from "../components/wiki/parseCallout";
import { wikiLinkRemarkPlugin } from "./wikilink";

export interface WikiMarkdownOptions {
  /**
   * Returns true when a wikilink slug resolves to an existing article.
   * Used to mark broken links in red per DESIGN-WIKI.md.
   */
  resolver: (slug: string) => boolean;
  /**
   * Optional navigation callback for intercepting internal wikilink clicks
   * so they route through the hash router instead of a full page load.
   * When omitted, links render as ordinary anchors.
   */
  onNavigate?: (slug: string) => void;
  /**
   * Wiki path of the article being rendered (e.g. `team/about/README.md`).
   * When provided, plain markdown links to `.md` files (e.g.
   * `[Company](company.md)`) are resolved relative to the article's
   * directory and rewritten to point at the hash-router wiki route
   * instead of leaking to the document base URL.
   */
  articlePath?: string;
}

/**
 * Resolve a markdown link href against the article's path and return the
 * destination wiki slug when the href is a relative `.md` link that should
 * route through the wiki SPA. Returns null for absolute URLs, anchors,
 * hash routes, non-`.md` targets, or paths that escape the wiki root.
 */
const NON_WIKI_HREF_RE = /^([a-z][a-z0-9+.-]*:|[#/])/i;

function joinWikiSegments(
  baseSegments: string[],
  relative: string,
): string[] | null {
  const out = [...baseSegments];
  for (const raw of relative.split("/")) {
    if (raw === "" || raw === ".") continue;
    if (raw === "..") {
      if (out.length === 0) return null;
      out.pop();
      continue;
    }
    out.push(raw);
  }
  return out;
}

export function resolveRelativeWikiPath(
  href: string,
  articlePath: string,
): string | null {
  if (!(href && articlePath)) return null;
  if (NON_WIKI_HREF_RE.test(href)) return null;

  // Fragments and query strings are dropped: the hash-router cannot nest a
  // second `#section` inside the wiki route, and articles do not expose a
  // query surface today. If section-anchors inside the wiki become a real
  // demand, route them through onNavigate instead of the href.
  const [pathOnly] = href.split(/[?#]/);
  if (!pathOnly.endsWith(".md")) return null;

  const lastSlash = articlePath.lastIndexOf("/");
  const baseDir = lastSlash >= 0 ? articlePath.slice(0, lastSlash) : "";
  const segments = joinWikiSegments(
    baseDir ? baseDir.split("/") : [],
    pathOnly,
  );
  if (!segments || segments.length === 0) return null;
  return segments.join("/");
}

/** Remark plugins — remark-gfm + wikilinks + Obsidian callouts. */
export function buildRemarkPlugins(
  resolver: (slug: string) => boolean,
): PluggableList {
  return [remarkGfm, wikiLinkRemarkPlugin(resolver), calloutRemarkPlugin()];
}

/** Rehype plugins — slug + autolink headings for TOC anchors. */
export function buildRehypePlugins(): PluggableList {
  return [rehypeSlug, [rehypeAutolinkHeadings, { behavior: "wrap" }]];
}

type AnchorProps = ComponentProps<"a">;
type ImageProps = ComponentProps<"img">;

/**
 * React-markdown component overrides:
 *  - anchors route wikilinks through onNavigate when provided
 *  - images render through the editorial ImageEmbed (lazy, no-referrer, lightbox)
 */
export function buildMarkdownComponents(
  options: WikiMarkdownOptions,
): Partial<Components> {
  const { onNavigate, articlePath } = options;
  return {
    blockquote: CalloutBlockquote,
    a: (props: AnchorProps): ReactElement => {
      const record = props as Record<string, unknown>;
      const isWikilink = record["data-wikilink"] === "true";
      if (isWikilink && onNavigate) {
        const slug = record["data-slug"] as string | undefined;
        return (
          <a
            {...props}
            onClick={(e) => {
              if (slug) {
                e.preventDefault();
                onNavigate(slug);
              }
            }}
          />
        );
      }
      if (!isWikilink && articlePath && typeof props.href === "string") {
        const resolved = resolveRelativeWikiPath(props.href, articlePath);
        if (resolved) {
          const encoded = resolved.split("/").map(encodeURIComponent).join("/");
          const nextProps = { ...props, href: `#/wiki/${encoded}` };
          return (
            <a
              {...nextProps}
              onClick={(e) => {
                if (onNavigate) {
                  e.preventDefault();
                  onNavigate(resolved);
                }
              }}
            />
          );
        }
      }
      return <a {...props} />;
    },
    img: ({ src, alt, width, height }: ImageProps): ReactElement | null => {
      if (!src) return null;
      const w =
        typeof width === "string" ? parseInt(width, 10) || undefined : width;
      const h =
        typeof height === "string" ? parseInt(height, 10) || undefined : height;
      return (
        <ImageEmbed src={String(src)} alt={alt ?? ""} width={w} height={h} />
      );
    },
  };
}
