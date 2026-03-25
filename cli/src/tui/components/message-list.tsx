import React, { useMemo } from "react";
import { Box, Text } from "ink";
import { getAgentColor } from "../agent-colors.js";

// --- Types ---

export interface Message {
  id: string;
  sender: string;
  content: string;
  timestamp: number;
  channel?: string;
}

export interface MessageListProps {
  messages: Message[];
  maxVisible?: number;
  /** Channel color for left border. When set, messages show a colored left border. */
  channelColor?: string;
  /** Channel name shown as a tag on each message. */
  channelName?: string;
}

// --- Helpers ---

function formatTime(ts: number): string {
  const d = new Date(ts);
  const h = String(d.getHours()).padStart(2, "0");
  const m = String(d.getMinutes()).padStart(2, "0");
  return `${h}:${m}`;
}

// --- Component ---

export function MessageList({
  messages,
  maxVisible = 20,
  channelColor,
  channelName,
}: MessageListProps): React.JSX.Element {
  const visible = useMemo(() => {
    if (messages.length <= maxVisible) return messages;
    return messages.slice(messages.length - maxVisible);
  }, [messages, maxVisible]);

  if (visible.length === 0) {
    return (
      <Box paddingX={1}>
        <Text dimColor>{"No messages yet."}</Text>
      </Box>
    );
  }

  return (
    <Box flexDirection="column">
      {visible.map((msg) => {
        const isHuman = msg.sender === "human";
        const senderColor = isHuman ? "green" : getAgentColor(msg.sender);
        return (
          <Box key={msg.id} paddingX={1} marginBottom={0}>
            {channelColor && (
              <Text color={channelColor}>{"\u2502 "}</Text>
            )}
            {channelColor && channelName && (
              <Text color={channelColor} dimColor>{"[#"}{channelName}{"] "}</Text>
            )}
            <Text dimColor>{formatTime(msg.timestamp)}</Text>
            <Text>{" "}</Text>
            <Text bold color={senderColor}>
              {msg.sender}
            </Text>
            <Text dimColor>{": "}</Text>
            <Text>{msg.content}</Text>
          </Box>
        );
      })}
    </Box>
  );
}

export default MessageList;
