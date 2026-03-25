/**
 * Inline Confirm input for the conversation view.
 *
 * Wraps @inkjs/ui's ConfirmInput component to provide an inline
 * y/n confirmation within the chat REPL.
 * Styled to match Bubbletea UX spec colors.
 */

import React from "react";
import { Box, Text } from "ink";
import { ConfirmInput } from "@inkjs/ui";

// Colors from bubbletea-ux-spec.md
const COLORS = {
  brand: "#2980fb",
  muted: "#838485",
} as const;

export interface InlineConfirmProps {
  question: string;
  onConfirm: (confirmed: boolean) => void;
}

export function InlineConfirm({
  question,
  onConfirm,
}: InlineConfirmProps): React.JSX.Element {
  return (
    <Box flexDirection="column" paddingX={2}>
      <Text bold color={COLORS.brand}>
        {question}
      </Text>
      <Box marginTop={1}>
        <ConfirmInput
          onConfirm={() => onConfirm(true)}
          onCancel={() => onConfirm(false)}
        />
      </Box>
      <Box marginTop={0}>
        <Text color={COLORS.muted}>
          {"y/n · enter to confirm"}
        </Text>
      </Box>
    </Box>
  );
}

export default InlineConfirm;
