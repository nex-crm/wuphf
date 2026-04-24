import { useEffect, useMemo, useRef } from "react";

import {
  FALLBACK_SLASH_COMMANDS,
  type SlashCommand,
} from "../../hooks/useCommands";
import { useOfficeMembers } from "../../hooks/useMembers";

export interface AutocompleteItem {
  /** Token to insert (e.g. "/clear" or "@ceo"). */
  insert: string;
  /** Primary label shown in the panel. */
  label: string;
  /** Secondary description. */
  desc?: string;
  /** Leading glyph. */
  icon?: string;
}

export type { SlashCommand };

/**
 * Legacy export preserved for callers that import SLASH_COMMANDS directly
 * (tests, external tooling). The live autocomplete now reads from the
 * broker via useCommands; this is the offline fallback list.
 */
export const SLASH_COMMANDS: SlashCommand[] = FALLBACK_SLASH_COMMANDS;

interface AutocompleteProps {
  /** Current composer text. */
  value: string;
  /** Caret position in the textarea (0-based). */
  caret: number;
  /** Currently highlighted item index, set by parent. */
  selectedIdx: number;
  /** Notify parent of total visible items so it can clamp selectedIdx. */
  onItems: (items: AutocompleteItem[]) => void;
  /** Pick an item: parent rewrites the text. */
  onPick: (item: AutocompleteItem) => void;
  /**
   * Slash-command set to offer. Parent supplies this (usually from
   * useCommands) so the broker registry stays the single source of truth.
   * Defaults to the offline fallback when omitted.
   */
  commands?: SlashCommand[];
}

/**
 * Renders the autocomplete panel above the composer. The parent owns
 * keyboard handling (up/down/enter/tab/escape) — this component only
 * paints, calculates the items list, and reports it.
 */
export function Autocomplete({
  value,
  caret,
  selectedIdx,
  onItems,
  onPick,
  commands = SLASH_COMMANDS,
}: AutocompleteProps) {
  const { data: members = [] } = useOfficeMembers();
  const listRef = useRef<HTMLDivElement>(null);

  const items = useMemo<AutocompleteItem[]>(() => {
    const trigger = currentTrigger(value, caret);
    if (!trigger) return [];
    if (trigger.kind === "slash") {
      const q = trigger.query.toLowerCase();
      return commands
        .filter((c) => c.name.slice(1).toLowerCase().startsWith(q))
        .slice(0, 8)
        .map((c) => ({
          insert: c.name,
          label: c.name,
          desc: c.desc,
          icon: c.icon,
        }));
    }
    const q = trigger.query.toLowerCase();
    return members
      .filter((m) => m.slug && m.slug !== "human" && m.slug !== "you")
      .filter((m) => {
        if (!q) return true;
        return (
          (m.slug || "").toLowerCase().includes(q) ||
          (m.name || "").toLowerCase().includes(q)
        );
      })
      .slice(0, 8)
      .map((m) => ({
        insert: `@${m.slug}`,
        label: `@${m.slug}`,
        desc: m.name,
        icon: m.emoji || "🤖",
      }));
  }, [value, caret, members, commands]);

  useEffect(() => {
    onItems(items);
  }, [items, onItems]);

  // Scroll selected into view
  useEffect(() => {
    const el = listRef.current?.children[selectedIdx] as
      | HTMLElement
      | undefined;
    el?.scrollIntoView({ block: "nearest" });
  }, [selectedIdx]);

  if (items.length === 0) return null;

  return (
    <div ref={listRef} className="autocomplete open">
      {items.map((item, idx) => (
        <button
          key={item.insert}
          type="button"
          className={`autocomplete-item${idx === selectedIdx ? " selected" : ""}`}
          onMouseDown={(e) => {
            // Prevent textarea blur before the click registers
            e.preventDefault();
            onPick(item);
          }}
        >
          <span className="autocomplete-item-icon">{item.icon}</span>
          <span className="autocomplete-item-label">{item.label}</span>
          {item.desc && (
            <span className="autocomplete-item-desc">{item.desc}</span>
          )}
        </button>
      ))}
    </div>
  );
}

/**
 * Inspect the text up to the caret and return the active trigger token.
 *
 * Returns:
 *  - {kind: 'slash', query, start} when the input starts with `/` and no space yet
 *  - {kind: 'mention', query, start} when the caret is inside an `@token` (no whitespace)
 *  - null otherwise
 */
export function currentTrigger(
  value: string,
  caret: number,
): { kind: "slash" | "mention"; query: string; start: number } | null {
  const before = value.slice(0, caret);
  // Slash must be at the very start of input (legacy behavior)
  if (/^\/\S*$/.test(value) && caret > 0) {
    return { kind: "slash", query: value.slice(1, caret), start: 0 };
  }
  // @-mention: find the last @ that is preceded by start-of-string or whitespace,
  // and has no whitespace between it and the caret.
  const atIdx = before.lastIndexOf("@");
  if (atIdx === -1) return null;
  const prevChar = atIdx === 0 ? "" : before[atIdx - 1];
  if (prevChar !== "" && !/\s/.test(prevChar)) return null;
  const tail = before.slice(atIdx + 1);
  if (/\s/.test(tail)) return null;
  return { kind: "mention", query: tail, start: atIdx };
}

/**
 * Replace the active trigger token in `value` with `insert + ' '`.
 * Returns the new text and the new caret position.
 */
export function applyAutocomplete(
  value: string,
  caret: number,
  item: AutocompleteItem,
): { text: string; caret: number } {
  const trigger = currentTrigger(value, caret);
  if (!trigger) return { text: value, caret };
  const before = value.slice(0, trigger.start);
  const after = value.slice(caret);
  const insert = `${item.insert} `;
  return {
    text: before + insert + after,
    caret: before.length + insert.length,
  };
}
