/**
 * Channel-colored message components.
 *
 * Renders chat messages in colored bordered boxes tagged with channel name,
 * matching Claude Code's TeamCreate multi-agent UI style.
 *
 * Each channel gets a distinct color. Agent messages within a channel show
 * the agent name in the agent's own color inside the channel's colored box.
 */

import React from "react";
import { Box, Text } from "ink";
import { getChannelColor } from "./channel-colors.js";
import type { ChannelColor } from "./channel-colors.js";

// ── Types ───────────────────────────────────────────────────────────

export interface ChannelMessageData {
  id: string;
  channel: string;
  sender: string;
  senderType: "agent" | "human" | "system";
  content: string;
  timestamp: number;
  /** Optional agent-specific color (overrides default for agent sender name) */
  agentColor?: string;
}

export interface ChannelMessageProps {
  message: ChannelMessageData;
}

export interface ChannelMessageListProps {
  messages: ChannelMessageData[];
  maxVisible?: number;
}

export interface ColoredChannelBarProps {
  channels: Array<{
    id: string;
    name: string;
    unread: number;
  }>;
  activeChannelId: string;
}

// ── Helpers ─────────────────────────────────────────────────────────

function formatTime(ts: number): string {
  const d = new Date(ts);
  const h = String(d.getHours()).padStart(2, "0");
  const m = String(d.getMinutes()).padStart(2, "0");
  return `${h}:${m}`;
}

// ── Single message component ────────────────────────────────────────

/**
 * A single message with colored left border and channel tag.
 *
 * Layout:
 *   ▌ [#channel] sender: content
 *   ^--- channel color border
 */
export function ChannelMessage({
  message,
}: ChannelMessageProps): React.JSX.Element {
  const channelColor = getChannelColor(message.channel);
  const time = formatTime(message.timestamp);

  if (message.senderType === "system") {
    return (
      <Box>
        <Text color={channelColor}>{"▌ "}</Text>
        <Text dimColor>{time}</Text>
        <Text>{" "}</Text>
        <Text color={channelColor} dimColor>
          {`[#${message.channel}]`}
        </Text>
        <Text>{" "}</Text>
        <Text color="yellow" dimColor>
          {message.content}
        </Text>
      </Box>
    );
  }

  if (message.senderType === "human") {
    return (
      <Box>
        <Text color={channelColor}>{"▌ "}</Text>
        <Text dimColor>{time}</Text>
        <Text>{" "}</Text>
        <Text color={channelColor} dimColor>
          {`[#${message.channel}]`}
        </Text>
        <Text>{" "}</Text>
        <Text bold color="white">
          {"> "}
        </Text>
        <Text>{message.content}</Text>
      </Box>
    );
  }

  // Agent message — agent name in agent's own color
  const senderColor = message.agentColor ?? channelColor;

  return (
    <Box>
      <Text color={channelColor}>{"▌ "}</Text>
      <Text dimColor>{time}</Text>
      <Text>{" "}</Text>
      <Text color={channelColor} dimColor>
        {`[#${message.channel}]`}
      </Text>
      <Text>{" "}</Text>
      <Text bold color={senderColor as ChannelColor}>
        {`[${message.sender}]`}
      </Text>
      <Text>{" "}</Text>
      <Text>{message.content}</Text>
    </Box>
  );
}

// ── Message list ────────────────────────────────────────────────────

/**
 * List of channel-colored messages with auto-tail.
 */
export function ChannelMessageList({
  messages,
  maxVisible = 20,
}: ChannelMessageListProps): React.JSX.Element {
  const displayMessages = messages.slice(-maxVisible);

  if (displayMessages.length === 0) {
    return (
      <Box paddingX={2}>
        <Text dimColor>{"No messages yet."}</Text>
      </Box>
    );
  }

  return (
    <Box flexDirection="column">
      {displayMessages.map((msg) => (
        <ChannelMessage key={msg.id} message={msg} />
      ))}
    </Box>
  );
}

// ── Colored channel bar ─────────────────────────────────────────────

/**
 * Horizontal channel tab bar with per-channel colors.
 * Active channel is bold + full color, others are dimmed.
 */
export function ColoredChannelBar({
  channels,
  activeChannelId,
}: ColoredChannelBarProps): React.JSX.Element {
  return (
    <Box paddingX={1}>
      {channels.map((ch, idx) => {
        const isActive = ch.id === activeChannelId;
        const color = getChannelColor(ch.name);
        return (
          <React.Fragment key={ch.id}>
            {idx > 0 && <Text dimColor>{"  "}</Text>}
            <Text
              bold={isActive}
              color={color}
              dimColor={!isActive}
            >
              {"#"}{ch.name}
            </Text>
            {ch.unread > 0 && (
              <Text color="red" bold>
                {` (${ch.unread})`}
              </Text>
            )}
          </React.Fragment>
        );
      })}
      {channels.length > 1 && (
        <Text dimColor>{"  [Tab=switch]"}</Text>
      )}
    </Box>
  );
}

export default ChannelMessage;
