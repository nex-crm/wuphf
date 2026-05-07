/**
 * Shared types for the WUPHF-specific editor inserts.
 *
 * Central definitions live here so the slash menu, mention picker, the
 * dialog components, and the controller hook all reference the same
 * action identifiers without circular imports.
 */

import type { MentionItem } from "./mentionCatalog";

export type SlashAction =
  | "wiki-link"
  | "citation"
  | "fact"
  | "task-ref"
  | "agent-mention"
  | "decision"
  | "related";

export interface SlashActionDef {
  /** Stable identifier referenced by the controller. */
  id: SlashAction;
  /** Title shown in the slash menu. */
  title: string;
  /** One-line description shown beside the title. */
  description: string;
  /** Lowercase keywords used for filtering. */
  keywords: string[];
}

export const SLASH_ACTIONS: SlashActionDef[] = [
  {
    id: "wiki-link",
    title: "Link wiki page",
    description: "Pick an existing page and insert a [[wikilink]].",
    keywords: ["link", "wiki", "page", "ref"],
  },
  {
    id: "citation",
    title: "Cite source",
    description: "Insert a footnote pointing to an external URL.",
    keywords: ["cite", "source", "footnote", "url"],
  },
  {
    id: "fact",
    title: "Add fact / triple",
    description:
      "Capture a subject + predicate + object claim (review required).",
    keywords: ["fact", "triple", "claim"],
  },
  {
    id: "task-ref",
    title: "Insert task reference",
    description: "Link to an open task wiki page.",
    keywords: ["task", "ref", "todo"],
  },
  {
    id: "agent-mention",
    title: "Insert agent mention",
    description: "Reference an agent by their wiki page.",
    keywords: ["agent", "mention", "@"],
  },
  {
    id: "decision",
    title: "Insert decision block",
    description: "Capture a decision with rationale and alternatives.",
    keywords: ["decision", "adr", "rationale"],
  },
  {
    id: "related",
    title: "Insert related pages",
    description: "Append a Related section linking to picked pages.",
    keywords: ["related", "see also", "links"],
  },
];

/**
 * Filter the slash actions against a query. Empty query returns all
 * actions in the canonical order.
 */
export function filterSlashActions(query: string): SlashActionDef[] {
  const q = query.trim().toLowerCase();
  if (!q) return SLASH_ACTIONS;
  return SLASH_ACTIONS.filter((a) => {
    if (a.title.toLowerCase().includes(q)) return true;
    if (a.description.toLowerCase().includes(q)) return true;
    return a.keywords.some((k) => k.toLowerCase().includes(q));
  });
}

export type ResolverFn = (slug: string) => boolean;

export interface InsertMenuPosition {
  /** Viewport-relative pixel coordinates for the menu's top-left corner. */
  top: number;
  left: number;
}

export type SelectMentionFn = (item: MentionItem) => void;
