import React, { useState, useCallback } from "react";
import { Box, Text } from "ink";
import { MessageList } from "../components/message-list.js";
import type { Message } from "../components/message-list.js";
import { ChatInput } from "../components/chat-input.js";
import type { Mode } from "../components/status-bar.js";

// --- Types ---

export interface AskChatViewProps {
  sessionId?: string;
  mode?: Mode;
  onAsk?: (question: string) => Promise<string> | string;
}

// --- Component ---

export function AskChatView({
  sessionId,
  mode = "insert",
  onAsk,
}: AskChatViewProps): React.JSX.Element {
  const [messages, setMessages] = useState<Message[]>([]);
  const [inputValue, setInputValue] = useState("");
  const [isLoading, setIsLoading] = useState(false);

  const handleSubmit = useCallback(
    async (value: string) => {
      if (!value.trim()) return;

      const humanMsg: Message = {
        id: `h-${Date.now()}`,
        sender: "human",
        content: value,
        timestamp: Date.now(),
      };
      setMessages((prev) => [...prev, humanMsg]);
      setInputValue("");
      setIsLoading(true);

      try {
        const answer = onAsk ? await onAsk(value) : "No handler configured.";
        const agentMsg: Message = {
          id: `a-${Date.now()}`,
          sender: "wuphf",
          content: answer,
          timestamp: Date.now(),
        };
        setMessages((prev) => [...prev, agentMsg]);
      } finally {
        setIsLoading(false);
      }
    },
    [onAsk],
  );

  return (
    <Box flexDirection="column" width="100%">
      {/* Header */}
      <Box paddingX={2} marginBottom={1}>
        <Text bold color="cyan">
          {"Ask WUPHF"}
        </Text>
        {sessionId && (
          <>
            <Text dimColor>{" \u2502 session: "}</Text>
            <Text dimColor>{sessionId}</Text>
          </>
        )}
      </Box>

      {/* Messages */}
      <Box flexGrow={1} flexDirection="column">
        <MessageList messages={messages} />
      </Box>

      {/* Loading indicator */}
      {isLoading && (
        <Box paddingX={2}>
          <Text color="cyan" dimColor>
            {"thinking..."}
          </Text>
        </Box>
      )}

      {/* Input */}
      <Box marginTop={1} paddingX={1}>
        <ChatInput
          value={inputValue}
          onChange={setInputValue}
          onSubmit={handleSubmit}
          prefix="ask> "
          placeholder="Ask a question..."
          isActive={!isLoading}
        />
      </Box>
    </Box>
  );
}

export default AskChatView;
