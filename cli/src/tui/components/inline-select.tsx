/**
 * Inline Select picker for the conversation view.
 *
 * Wraps @inkjs/ui's Select component to provide an arrow-key
 * navigable single-select within the chat REPL.
 * Styled to match Bubbletea picker: highlighted row with arrow
 * marker, descriptions, brand colors.
 */

import React from "react";
import { Box, Text } from "ink";
import { Select } from "@inkjs/ui";

// Colors from bubbletea-ux-spec.md
const COLORS = {
  brand: "#2980fb",
  info: "#4d97ff",
  muted: "#838485",
  label: "#999a9b",
} as const;

export interface SelectOption {
  label: string;
  value: string;
  description?: string;
}

export interface InlineSelectProps {
  title: string;
  options: SelectOption[];
  onSelect: (value: string) => void;
}

export function InlineSelect({
  title,
  options,
  onSelect,
}: InlineSelectProps): React.JSX.Element {
  // Build label with description on same line (dimmed)
  const selectOptions = options.map((opt) => ({
    label: opt.description ? `${opt.label}  ${opt.description}` : opt.label,
    value: opt.value,
  }));

  return (
    <Box flexDirection="column" paddingX={2}>
      <Text bold color={COLORS.brand}>
        {title}
      </Text>
      <Box marginTop={1} flexDirection="column">
        <Select
          options={selectOptions}
          onChange={(value) => onSelect(value)}
        />
      </Box>
      <Box marginTop={0}>
        <Text color={COLORS.muted}>
          {"↑/↓ navigate · enter select · esc back"}
        </Text>
      </Box>
    </Box>
  );
}

export default InlineSelect;
