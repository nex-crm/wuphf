/**
 * Wikilink parser and remark plugin factory.
 *
 * Supports:
 *   [[slug]]           -> { slug: "slug", display: "slug" }
 *   [[slug|Display]]   -> { slug: "slug", display: "Display" }
 *   [[people/nazz]]    -> { slug: "people/nazz", display: "people/nazz" }
 *
 * Rejects (returns null):
 *   empty/whitespace-only slug
 *   multiple pipes
 *   path traversal (..)
 *   absolute paths (leading /)
 */

export interface WikiLink {
  slug: string
  display: string
}

const WIKILINK_RE = /^\[\[(.+?)\]\]$/s

export function parseWikiLink(input: string): WikiLink | null {
  if (typeof input !== 'string') return null
  const match = input.match(WIKILINK_RE)
  if (!match) return null
  return parseWikiLinkInner(match[1])
}

/**
 * Parse the inner text of a `[[...]]` link (without the surrounding brackets).
 */
export function parseWikiLinkInner(raw: string): WikiLink | null {
  if (typeof raw !== 'string') return null

  // Multiple pipes disallowed.
  const pipeCount = (raw.match(/\|/g) || []).length
  if (pipeCount > 1) return null

  let slug: string
  let display: string
  if (pipeCount === 1) {
    const idx = raw.indexOf('|')
    slug = raw.slice(0, idx).trim()
    display = raw.slice(idx + 1).trim()
  } else {
    slug = raw.trim()
    display = slug
  }

  if (!slug) return null
  if (!display) display = slug

  // Path traversal + absolute paths slip users out of the wiki root.
  if (slug.includes('..')) return null
  if (slug.startsWith('/')) return null
  // Control chars / NUL.
  if (/[\x00-\x1f]/.test(slug)) return null

  return { slug, display }
}

// ── AST types (minimal mdast surface for the remark plugin) ──

interface MdTextNode {
  type: 'text'
  value: string
}

interface MdLinkNode {
  type: 'link'
  url: string
  children: MdAnyNode[]
  data?: { hProperties?: Record<string, string> }
}

type MdAnyNode =
  | MdTextNode
  | MdLinkNode
  | { type: string; children?: MdAnyNode[]; value?: string }

interface MdParent {
  children: MdAnyNode[]
}

/**
 * Build a remark plugin that rewrites `[[slug|Display]]` tokens inside text
 * nodes into link AST nodes. A custom `react-markdown` renderer intercepts
 * via the `data-wikilink` attribute and mounts the `WikiLink` component.
 *
 * `resolver(slug)` returns true when the target exists (i.e. not broken).
 */
export function wikiLinkRemarkPlugin(resolver: (slug: string) => boolean) {
  return function plugin() {
    return function transformer(tree: unknown) {
      walk(tree as MdAnyNode, (parent) => {
        const children = parent.children
        for (let i = 0; i < children.length; i++) {
          const child = children[i]
          if (child.type !== 'text' || typeof (child as MdTextNode).value !== 'string') continue
          const value = (child as MdTextNode).value
          if (!value.includes('[[')) continue

          const replacements = buildReplacements(value, resolver)
          if (replacements.length === 0) continue
          children.splice(i, 1, ...replacements)
          i += replacements.length - 1
        }
      })
    }
  }
}

function buildReplacements(value: string, resolver: (slug: string) => boolean): MdAnyNode[] {
  const re = /\[\[([^\]\n]+)\]\]/g
  const out: MdAnyNode[] = []
  let lastIndex = 0
  let match: RegExpExecArray | null
  let changed = false
  while ((match = re.exec(value)) !== null) {
    const link = parseWikiLinkInner(match[1])
    if (!link) continue
    changed = true
    if (match.index > lastIndex) {
      out.push({ type: 'text', value: value.slice(lastIndex, match.index) })
    }
    const broken = !resolver(link.slug)
    out.push({
      type: 'link',
      url: `#/wiki/${encodeURI(link.slug)}`,
      children: [{ type: 'text', value: link.display }],
      data: {
        hProperties: {
          'data-wikilink': 'true',
          'data-broken': broken ? 'true' : 'false',
          'data-slug': link.slug,
          className: broken ? 'wk-wikilink wk-broken' : 'wk-wikilink',
        },
      },
    })
    lastIndex = re.lastIndex
  }
  if (!changed) return []
  if (lastIndex < value.length) {
    out.push({ type: 'text', value: value.slice(lastIndex) })
  }
  return out
}

function walk(node: MdAnyNode, onParent: (parent: MdParent) => void) {
  const maybeParent = node as { children?: MdAnyNode[] }
  const children = maybeParent.children
  if (!Array.isArray(children)) return
  onParent(node as MdParent)
  // Walk a snapshot because onParent may have mutated children.
  const snapshot = [...(maybeParent.children || [])]
  for (const child of snapshot) {
    if (child && typeof child === 'object' && 'children' in child) {
      walk(child as MdAnyNode, onParent)
    }
  }
}
