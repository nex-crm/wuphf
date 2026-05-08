/**
 * Command palette types.
 *
 * Groups map to the sections rendered inside the palette. The union is
 * intentionally ordered: static action groups appear first, dynamic object
 * groups appear after, and search-hit groups appear last.
 */
export type CommandGroup =
  | "Actions"
  | "Agents"
  | "Channels"
  | "Tasks"
  | "Wiki"
  | "Notebooks"
  | "Messages";

/**
 * One row in the command palette.
 *
 * `aliases` is an optional list of extra strings to match against during
 * filtering — useful for commands that have multiple natural-language names
 * (e.g. "doctor" also matches "health").
 */
export interface CommandItem {
  /** Unique key for React reconciliation and selection tracking. */
  id: string;
  group: CommandGroup;
  /** Emoji or short text icon rendered before the label. */
  icon: string;
  /** Primary display text. */
  label: string;
  /** Secondary description shown below the label. */
  desc?: string;
  /** Monospace badge shown at the trailing edge (e.g. "@slug", "L42"). */
  meta?: string;
  /** Extra strings included in the filter match (in addition to label + desc). */
  aliases?: string[];
  /** Called when the user activates this item via Enter or click. */
  run: () => void;
}
