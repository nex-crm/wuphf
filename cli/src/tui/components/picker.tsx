import React, { useMemo } from "react";
import { Box, Text } from "ink";

// --- Types ---

export interface PickerItem {
  command: string;
  label: string;
  detail: string;
}

export interface PickerProps {
  items: PickerItem[];
  cursor: number;
  onSelect: (item: PickerItem) => void;
  onCursorChange: (cursor: number) => void;
  quickSelect?: boolean;
  maxVisible?: number;
}

// --- Component ---

export function Picker({
  items,
  cursor,
  onSelect: _onSelect,
  onCursorChange: _onCursorChange,
  quickSelect = false,
  maxVisible = 15,
}: PickerProps): React.JSX.Element {
  const total = items.length;

  // Compute visible window boundaries
  const { windowStart, windowEnd, showUpArrow, showDownArrow } = useMemo(() => {
    const effective = Math.min(maxVisible, total);
    let start = 0;
    if (cursor >= effective) {
      start = cursor - effective + 1;
    }
    const end = Math.min(start + effective, total);
    return {
      windowStart: start,
      windowEnd: end,
      showUpArrow: start > 0,
      showDownArrow: end < total,
    };
  }, [cursor, maxVisible, total]);

  const visibleItems = items.slice(windowStart, windowEnd);

  return (
    <Box flexDirection="column">
      {showUpArrow && (
        <Box paddingLeft={2}>
          <Text dimColor>{"  \u25B2 more"}</Text>
        </Box>
      )}
      {visibleItems.map((item, i) => {
        const realIndex = windowStart + i;
        const isSelected = realIndex === cursor;
        const prefix = isSelected ? "> " : "  ";
        const digitLabel =
          quickSelect && realIndex < 9 ? `${realIndex + 1}. ` : "";

        return (
          <Box key={item.command + realIndex} paddingLeft={1}>
            <Text
              color={isSelected ? "cyan" : undefined}
              bold={isSelected}
            >
              {prefix}
              {digitLabel}
              {item.label}
            </Text>
            <Text dimColor>{`  ${item.detail}`}</Text>
          </Box>
        );
      })}
      {showDownArrow && (
        <Box paddingLeft={2}>
          <Text dimColor>{"  \u25BC more"}</Text>
        </Box>
      )}
    </Box>
  );
}

export default Picker;
