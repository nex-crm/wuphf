import React, { useState, useCallback } from "react";
import { Box, Text } from "ink";
import { TextInput } from "@inkjs/ui";

// --- Types ---

export interface ChatInputProps {
  value: string;
  onChange: (value: string) => void;
  onSubmit: (value: string) => void;
  placeholder?: string;
  prefix?: string;
  isActive: boolean;
}

// --- Component ---

export function ChatInput({
  value,
  onChange,
  onSubmit,
  placeholder = "Type a command...",
  prefix = "wuphf> ",
  isActive,
}: ChatInputProps): React.JSX.Element {
  // Counter to force TextInput remount after submit.
  // @inkjs/ui TextInput manages its own internal state via useReducer;
  // defaultValue is only read on initial mount. Incrementing the key
  // forces React to unmount + remount with a fresh empty state.
  const [submitKey, setSubmitKey] = useState(0);

  const handleSubmit = useCallback(
    (val: string) => {
      onSubmit(val);
      setSubmitKey((k) => k + 1);
    },
    [onSubmit],
  );

  return (
    <Box>
      <Text bold color={isActive ? "cyan" : undefined} dimColor={!isActive}>
        {prefix}
      </Text>
      {isActive ? (
        <TextInput
          key={submitKey}
          placeholder={placeholder}
          onChange={onChange}
          onSubmit={handleSubmit}
        />
      ) : (
        <Text dimColor>{value || placeholder}</Text>
      )}
    </Box>
  );
}

export default ChatInput;
