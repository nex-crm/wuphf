/**
 * Helpers for grouping the wiki catalog into mention-picker categories.
 *
 * The wiki catalog already includes wiki pages, agents, tasks, people,
 * companies, projects, etc. — they share a single `WikiCatalogEntry`
 * shape and resolve through the same `[[slug]]` wikilink path. To make
 * the slash and `@` mention pickers feel structured, we project entries
 * into stable category buckets keyed off the entry's `group` field
 * (which the broker populates from the path prefix `team/<group>/...`).
 *
 * Grouping rules:
 *   - `agents`: entries whose path starts with `agents/` (the legacy
 *     v1.2 per-agent notebook namespace) OR whose `group` is `agents`
 *     in newer blueprints. Agents are tracked separately so a "@"
 *     mention can default to them.
 *   - `tasks`: `group === "tasks"`.
 *   - `people` / `companies` / `projects`: matched on `group` directly.
 *   - `pages`: everything else — playbooks, decisions, inbox notes,
 *     anything blueprint-specific.
 *
 * Inputs that never resolve into a valid wikilink (because `parseWikiLinkInner`
 * rejects the path) are silently dropped so the picker never offers a slug
 * the editor would later refuse.
 */

import type { WikiCatalogEntry } from "../../../../api/wiki";
import { parseWikiLinkInner } from "../../../../lib/wikilink";

export type MentionCategory =
  | "pages"
  | "agents"
  | "tasks"
  | "people"
  | "companies"
  | "projects";

export interface MentionItem {
  /** Canonical wiki path used by `[[…]]` resolution. */
  slug: string;
  /** Title from the catalog. */
  title: string;
  /** Pre-bucketed category for grouped rendering. */
  category: MentionCategory;
}

const CATEGORY_LABELS: Record<MentionCategory, string> = {
  pages: "Pages",
  agents: "Agents",
  tasks: "Tasks",
  people: "People",
  companies: "Companies",
  projects: "Projects",
};

const CATEGORY_ORDER: MentionCategory[] = [
  "pages",
  "people",
  "companies",
  "projects",
  "agents",
  "tasks",
];

export function categoryLabel(c: MentionCategory): string {
  return CATEGORY_LABELS[c];
}

export function categoryOrder(): readonly MentionCategory[] {
  return CATEGORY_ORDER;
}

/**
 * Project a catalog entry into a `MentionItem`. Returns null when the
 * entry's path cannot be reduced to a valid wikilink slug.
 *
 * The conversion strips the trailing `.md` extension because wikilinks
 * are written without it. `parseWikiLinkInner` enforces the slug grammar
 * so a path containing `..` or other illegal sequences is rejected.
 */
export function toMentionItem(entry: WikiCatalogEntry): MentionItem | null {
  const slug = entry.path.replace(/\.md$/, "");
  if (!parseWikiLinkInner(slug)) return null;
  return {
    slug,
    title: entry.title || slug,
    category: classify(entry),
  };
}

function classify(entry: WikiCatalogEntry): MentionCategory {
  if (entry.path.startsWith("agents/")) return "agents";
  switch (entry.group) {
    case "people":
      return "people";
    case "companies":
      return "companies";
    case "projects":
      return "projects";
    case "agents":
      return "agents";
    case "tasks":
      return "tasks";
    default:
      return "pages";
  }
}

/**
 * Filter and rank mention items against a query. Empty queries return all
 * items (bucketed). Non-empty queries match on title + slug substring, then
 * sort by:
 *   1. Title prefix match (highest)
 *   2. Slug prefix match
 *   3. Substring match anywhere
 * Ties broken by title alpha.
 *
 * The picker UI renders the result top-N (caller-controlled) so very large
 * catalogs (1000+ entries) still render in a single frame.
 */
export function searchMentionItems(
  items: MentionItem[],
  query: string,
  limit = 50,
): MentionItem[] {
  const q = query.trim().toLowerCase();
  if (!q) {
    // No query: return items in the configured category order, alpha
    // within each bucket. Truncate so the picker stays responsive.
    return [...items]
      .sort((a, b) => {
        const ai = CATEGORY_ORDER.indexOf(a.category);
        const bi = CATEGORY_ORDER.indexOf(b.category);
        if (ai !== bi) return ai - bi;
        return a.title.localeCompare(b.title);
      })
      .slice(0, limit);
  }
  const scored: { item: MentionItem; score: number }[] = [];
  for (const item of items) {
    const titleLc = item.title.toLowerCase();
    const slugLc = item.slug.toLowerCase();
    let score = 0;
    if (titleLc.startsWith(q)) score = 3;
    else if (slugLc.startsWith(q)) score = 2;
    else if (titleLc.includes(q) || slugLc.includes(q)) score = 1;
    if (score > 0) scored.push({ item, score });
  }
  scored.sort((a, b) => {
    if (a.score !== b.score) return b.score - a.score;
    return a.item.title.localeCompare(b.item.title);
  });
  return scored.slice(0, limit).map((s) => s.item);
}

/**
 * Group mention items by category for menu rendering. Categories are
 * returned in `CATEGORY_ORDER`, omitting empty buckets.
 */
export function groupMentionItems(
  items: MentionItem[],
): { category: MentionCategory; items: MentionItem[] }[] {
  const buckets = new Map<MentionCategory, MentionItem[]>();
  for (const item of items) {
    const bucket = buckets.get(item.category) ?? [];
    bucket.push(item);
    buckets.set(item.category, bucket);
  }
  const out: { category: MentionCategory; items: MentionItem[] }[] = [];
  for (const cat of CATEGORY_ORDER) {
    const bucket = buckets.get(cat);
    if (bucket && bucket.length > 0) out.push({ category: cat, items: bucket });
  }
  return out;
}
