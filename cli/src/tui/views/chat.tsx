import React, { useState } from "react";
import { Box, Text } from "ink";
import { MessageList } from "../components/message-list.js";
import type { Message } from "../components/message-list.js";
import { ChatInput } from "../components/chat-input.js";
import type { Mode } from "../components/status-bar.js";
import { getChannelColor } from "../channel-colors.js";

// --- Types ---

export interface Channel {
  id: string;
  name: string;
  unread: number;
}

export interface ChatViewProps {
  channels: Channel[];
  messages: Message[];
  activeChannel?: string;
  mode?: Mode;
  onSend?: (message: string, channel: string) => void;
  onChannelSelect?: (channelId: string) => void;
}

// --- Component ---

export function ChatView({
  channels,
  messages,
  activeChannel,
  mode = "normal",
  onSend,
  onChannelSelect: _onChannelSelect,
}: ChatViewProps): React.JSX.Element {
  const [inputValue, setInputValue] = useState("");

  const selectedChannel = activeChannel ?? channels[0]?.id;
  const selectedChannelName = channels.find((c) => c.id === selectedChannel)?.name ?? "";
  const channelMessages = messages.filter(
    (m) => !m.channel || m.channel === selectedChannel,
  );

  const handleSubmit = (value: string) => {
    if (!value.trim() || !selectedChannel) return;
    onSend?.(value, selectedChannel);
    setInputValue("");
  };

  // Resolve channel color for message list
  const chColor = selectedChannelName ? getChannelColor(selectedChannelName) : undefined;

  return (
    <Box flexDirection="row" width="100%">
      {/* Channel sidebar */}
      <Box
        flexDirection="column"
        width={20}
        borderStyle="single"
        borderRight
        borderTop={false}
        borderBottom={false}
        borderLeft={false}
        paddingX={1}
      >
        <Box marginBottom={1}>
          <Text bold color="cyan">
            {"Channels"}
          </Text>
        </Box>
        {channels.map((ch) => {
          const isActive = ch.id === selectedChannel;
          const color = getChannelColor(ch.name);
          return (
            <Box key={ch.id}>
              <Text
                bold={isActive}
                color={color}
                dimColor={!isActive}
              >
                {isActive ? "> " : "  "}
                {"#"}
                {ch.name}
              </Text>
              {ch.unread > 0 && (
                <Text color="red" bold>
                  {` (${ch.unread})`}
                </Text>
              )}
            </Box>
          );
        })}

        {channels.length === 0 && (
          <Text dimColor>{"No channels"}</Text>
        )}
      </Box>

      {/* Main chat area */}
      <Box flexDirection="column" flexGrow={1}>
        {/* Channel header */}
        {selectedChannelName && (
          <Box paddingX={1} marginBottom={1}>
            <Text bold color={chColor}>{"#"}{selectedChannelName}</Text>
          </Box>
        )}

        {/* Messages */}
        <Box flexGrow={1} flexDirection="column">
          <MessageList
            messages={channelMessages}
            channelColor={chColor}
            channelName={selectedChannelName}
          />
        </Box>

        {/* Input */}
        <Box paddingX={1} marginTop={1}>
          <ChatInput
            value={inputValue}
            onChange={setInputValue}
            onSubmit={handleSubmit}
            prefix={`#${selectedChannelName}> `}
            placeholder="Type a message..."
            isActive={true}
          />
        </Box>
      </Box>
    </Box>
  );
}

export default ChatView;
