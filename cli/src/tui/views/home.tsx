/**
 * Conversation-first home view.
 *
 * Chat REPL: full-width input always active at bottom,
 * message history scrolling up, slash commands for navigation.
 *
 * When a picker or confirm prompt is active, the TextInput is hidden
 * and the interactive widget is shown instead.
 */

import React, { useState } from "react";
import { Box, Text, useStdout } from "ink";
import { TextInput } from "@inkjs/ui";
import type { ConversationMessage } from "../slash-commands.js";
import type { SelectOption } from "../components/inline-select.js";
import { InlineSelect } from "../components/inline-select.js";
import { InlineConfirm } from "../components/inline-confirm.js";

// ── Types ────────────────────────────────────────────────────────────

export interface PickerState {
  title: string;
  options: SelectOption[];
  onSelect: (value: string) => void;
}

export interface ConfirmState {
  question: string;
  onConfirm: (confirmed: boolean) => void;
}

export interface ConversationViewProps {
  messages: ConversationMessage[];
  onSubmit: (input: string) => void | Promise<void>;
  isLoading?: boolean;
  loadingHint?: string;
  /** When set, shows an arrow-key navigable picker instead of the text input. */
  picker?: PickerState | null;
  /** When set, shows a y/n confirm prompt instead of the text input. */
  confirm?: ConfirmState | null;
}

// ── Message renderer ────────────────────────────────────────────────

function MessageBubble({ msg }: { msg: ConversationMessage }) {
  const streamingCursor = msg.isStreaming ? "▌" : "";

  switch (msg.role) {
    case "user":
      return (
        <Box paddingX={2}>
          <Text bold color="white">
            {"> "}
          </Text>
          <Text>{msg.content}</Text>
        </Box>
      );

    case "assistant":
      return (
        <Box paddingX={2} flexDirection="column">
          <Text color={msg.isError ? "red" : "cyan"}>
            {msg.content}{streamingCursor}
          </Text>
        </Box>
      );

    case "system":
      return (
        <Box paddingX={2} justifyContent="center">
          <Text color="yellow" dimColor>
            {msg.content}
          </Text>
        </Box>
      );

    case "tool":
      return (
        <Box paddingX={4}>
          {msg.toolName && (
            <Text dimColor bold>
              {`[${msg.toolName}] `}
            </Text>
          )}
          <Text dimColor>{msg.content}</Text>
        </Box>
      );

    default:
      return (
        <Box paddingX={2}>
          <Text>{msg.content}</Text>
        </Box>
      );
  }
}

// ── Conversation view ───────────────────────────────────────────────

export function ConversationView({
  messages,
  onSubmit,
  isLoading = false,
  loadingHint = "thinking...",
  picker = null,
  confirm = null,
}: ConversationViewProps): React.JSX.Element {
  const [inputValue, setInputValue] = useState("");
  // Counter to force TextInput remount after submit.
  // @inkjs/ui TextInput only reads defaultValue on initial mount (useReducer
  // initial state), so setting defaultValue="" after submit has no effect.
  // Changing the key forces React to unmount + remount, giving a fresh input.
  const [submitKey, setSubmitKey] = useState(0);
  const { stdout } = useStdout();
  const rows = stdout?.rows ?? 24;

  // Calculate visible area: total rows - 4 (status bar, input, divider, padding)
  const visibleRows = Math.max(rows - 5, 6);

  // Determine which messages to show (tail of the list)
  // Rough estimate: each message ~1-3 lines
  const displayMessages = messages.slice(-visibleRows);

  const handleSubmit = (value: string) => {
    if (!value.trim()) return;
    // onSubmit may be async — catch unhandled rejections
    Promise.resolve(onSubmit(value)).catch(() => {});
    setInputValue("");
    setSubmitKey((k) => k + 1);
  };

  const hasWidget = picker != null || confirm != null;

  return (
    <Box flexDirection="column" width="100%" minHeight={visibleRows + 3}>
      {/* Message history */}
      <Box flexDirection="column" flexGrow={1}>
        {displayMessages.map((msg) => (
          <Box key={msg.id} marginBottom={0}>
            <MessageBubble msg={msg} />
          </Box>
        ))}

        {/* Loading indicator */}
        {isLoading && (
          <Box paddingX={2}>
            <Text color="cyan" dimColor>
              {`  ${loadingHint}`}
            </Text>
          </Box>
        )}
      </Box>

      {/* Divider */}
      <Box paddingX={1}>
        <Text dimColor>
          {"─".repeat(Math.min(stdout?.columns ?? 60, 120) - 2)}
        </Text>
      </Box>

      {/* Interactive widget area: picker, confirm, or text input */}
      {picker != null ? (
        <InlineSelect
          title={picker.title}
          options={picker.options}
          onSelect={picker.onSelect}
        />
      ) : confirm != null ? (
        <InlineConfirm
          question={confirm.question}
          onConfirm={confirm.onConfirm}
        />
      ) : (
        <Box paddingX={1}>
          <Text bold color="cyan">
            {"> "}
          </Text>
          <TextInput
            key={submitKey}
            placeholder="Type a message or /help..."
            onChange={setInputValue}
            onSubmit={handleSubmit}
          />
        </Box>
      )}
    </Box>
  );
}

// Keep the default export for backward compat
export default ConversationView;
