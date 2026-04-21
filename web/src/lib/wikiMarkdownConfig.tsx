/**
 * Shared markdown pipeline for the wiki surface.
 *
 * Extracted so the editor's live preview renders through the exact same
 * remark/rehype plugins and component overrides as `WikiArticle`. Keep
 * this file small — it's pure config, not logic.
 */

import type { ComponentProps, ReactElement } from 'react'
import type { PluggableList } from 'unified'
import remarkGfm from 'remark-gfm'
import rehypeSlug from 'rehype-slug'
import rehypeAutolinkHeadings from 'rehype-autolink-headings'
import type { Components } from 'react-markdown'
import ImageEmbed from '../components/wiki/ImageEmbed'
import { wikiLinkRemarkPlugin } from './wikilink'

export interface WikiMarkdownOptions {
  /**
   * Returns true when a wikilink slug resolves to an existing article.
   * Used to mark broken links in red per DESIGN-WIKI.md.
   */
  resolver: (slug: string) => boolean
  /**
   * Optional navigation callback for intercepting internal wikilink clicks
   * so they route through the hash router instead of a full page load.
   * When omitted, links render as ordinary anchors.
   */
  onNavigate?: (slug: string) => void
}

/** Remark plugins — remark-gfm + wikilinks. */
export function buildRemarkPlugins(
  resolver: (slug: string) => boolean,
): PluggableList {
  return [remarkGfm, wikiLinkRemarkPlugin(resolver)]
}

/** Rehype plugins — slug + autolink headings for TOC anchors. */
export function buildRehypePlugins(): PluggableList {
  return [rehypeSlug, [rehypeAutolinkHeadings, { behavior: 'wrap' }]]
}

type AnchorProps = ComponentProps<'a'>
type ImageProps = ComponentProps<'img'>

/**
 * React-markdown component overrides:
 *  - anchors route wikilinks through onNavigate when provided
 *  - images render through the editorial ImageEmbed (lazy, no-referrer, lightbox)
 */
export function buildMarkdownComponents(
  options: WikiMarkdownOptions,
): Partial<Components> {
  const { onNavigate } = options
  return {
    a: (props: AnchorProps): ReactElement => {
      const record = props as Record<string, unknown>
      const isWikilink = record['data-wikilink'] === 'true'
      if (isWikilink && onNavigate) {
        const slug = record['data-slug'] as string | undefined
        return (
          <a
            {...props}
            onClick={(e) => {
              if (slug) {
                e.preventDefault()
                onNavigate(slug)
              }
            }}
          />
        )
      }
      return <a {...props} />
    },
    img: ({ src, alt, width, height }: ImageProps): ReactElement | null => {
      if (!src) return null
      const w =
        typeof width === 'string' ? parseInt(width, 10) || undefined : width
      const h =
        typeof height === 'string' ? parseInt(height, 10) || undefined : height
      return (
        <ImageEmbed src={String(src)} alt={alt ?? ''} width={w} height={h} />
      )
    },
  }
}
