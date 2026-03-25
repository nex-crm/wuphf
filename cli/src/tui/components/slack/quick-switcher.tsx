/**
 * Quick Switcher (Ctrl+K) modal overlay.
 *
 * Provides fuzzy search across channels and DMs with frecency-based
 * ranking. Renders as a centered overlay capturing all keyboard input.
 */

import React, { useState, useMemo, useCallback, useEffect } from "react";
import { Box, Text } from "ink";
import { TextInput } from "@inkjs/ui";

// ── Types ───────────────────────────────────────────────────────────

export interface QuickSwitcherItem {
  id: string;
  name: string;
  type: "channel" | "dm" | "group-dm";
  online?: boolean;
  unread: number;
  /** Frecency score for ranking (higher = more recent/frequent). */
  score: number;
}

export interface QuickSwitcherProps {
  open: boolean;
  items: QuickSwitcherItem[];
  onSelect: (id: string) => void;
  onClose: () => void;
}

// ── Fuzzy match ─────────────────────────────────────────────────────

/**
 * Case-insensitive substring match on name.
 * Matches partial words and hyphen/underscore-separated tokens.
 */
export function fuzzyMatch(query: string, name: string): boolean {
  if (!query) return true;
  const q = query.toLowerCase();
  const n = name.toLowerCase();

  // Direct substring
  if (n.includes(q)) return true;

  // Token initials match (e.g. "fa" matches "Founding Agent")
  const tokens = n.split(/[\s\-_]+/);
  const initials = tokens.map((t) => t[0] || "").join("");
  if (initials.includes(q)) return true;

  return false;
}

/**
 * Filter and sort items by fuzzy match + frecency score.
 */
export function filterItems(
  query: string,
  items: QuickSwitcherItem[],
): QuickSwitcherItem[] {
  const matched = items.filter((item) => fuzzyMatch(query, item.name));
  // Sort by score descending (frecency)
  return matched.sort((a, b) => b.score - a.score);
}

// ── Item row ────────────────────────────────────────────────────────

function SwitcherItem({
  item,
  isSelected,
}: {
  item: QuickSwitcherItem;
  isSelected: boolean;
}): React.JSX.Element {
  let prefix: React.JSX.Element;
  if (item.type === "channel") {
    prefix = <Text color="gray"># </Text>;
  } else {
    const dotColor = item.online ? "green" : "gray";
    const dot = item.online ? "●" : "○";
    prefix = <Text color={dotColor}>{dot} </Text>;
  }

  return (
    <Box>
      <Text color={isSelected ? "cyan" : undefined} bold={isSelected}>
        {isSelected ? "> " : "  "}
      </Text>
      {prefix}
      <Text color={isSelected ? "cyan" : "white"} bold={isSelected}>
        {item.name}
      </Text>
      {item.unread > 0 && <Text color="gray"> ({item.unread})</Text>}
    </Box>
  );
}

// ── QuickSwitcher ───────────────────────────────────────────────────

export function QuickSwitcher({
  open,
  items,
  onSelect,
  onClose,
}: QuickSwitcherProps): React.JSX.Element | null {
  // All hooks must be called unconditionally before any early return
  const [query, setQuery] = useState("");
  const [selectedIndex, setSelectedIndex] = useState(0);

  const maxVisible = 10;
  const filtered = useMemo(() => filterItems(query, items), [query, items]);
  const visibleItems = filtered.slice(0, maxVisible);

  // Reset state when switcher opens
  useEffect(() => {
    if (open) {
      setQuery("");
      setSelectedIndex(0);
    }
  }, [open]);

  const handleChange = useCallback((newQuery: string) => {
    setQuery(newQuery);
    setSelectedIndex(0);
  }, []);

  const handleSubmit = useCallback(() => {
    if (filtered.length > 0 && selectedIndex < filtered.length) {
      onSelect(filtered[selectedIndex].id);
    }
  }, [filtered, selectedIndex, onSelect]);

  // Register globalThis bridge for arrow key navigation from app.tsx
  const filteredLengthRef = React.useRef(filtered.length);
  filteredLengthRef.current = filtered.length;
  useEffect(() => {
    if (!open) return;
    const g = globalThis as Record<string, unknown>;
    g.__nexQuickSwitcherNav = (direction: number) => {
      setSelectedIndex((prev) => {
        const len = Math.min(filteredLengthRef.current, maxVisible);
        if (len === 0) return 0;
        return ((prev + direction) % len + len) % len;
      });
    };
    return () => { delete g.__nexQuickSwitcherNav; };
  }, [open]);

  if (!open) return null;

  return (
    <Box
      flexDirection="column"
      borderStyle="double"
      borderColor="cyan"
      paddingX={1}
    >
      {/* Header */}
      <Box>
        <Text bold color="cyan">Switch to...</Text>
      </Box>

      {/* Search input */}
      <Box>
        <Text color="gray">🔍 </Text>
        <TextInput
          placeholder="Search channels and DMs"
          onChange={handleChange}
          onSubmit={handleSubmit}
        />
      </Box>

      {/* Divider */}
      <Box>
        <Text color="gray">{"─".repeat(40)}</Text>
      </Box>

      {/* Results */}
      <Box flexDirection="column">
        {visibleItems.length === 0 && (
          <Box paddingX={1}>
            <Text color="gray">No matches found</Text>
          </Box>
        )}
        {visibleItems.map((item, idx) => (
          <SwitcherItem key={item.id} item={item} isSelected={idx === selectedIndex} />
        ))}
      </Box>

      {/* Footer hint */}
      <Box>
        <Text color="gray">↑↓ navigate · Enter select · Esc close</Text>
      </Box>
    </Box>
  );
}

export default QuickSwitcher;
