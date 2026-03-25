/**
 * Slack-style thread panel component.
 *
 * Right panel showing a parent message, reply count divider,
 * thread replies, and a compose area for sending replies.
 * Escape closes the panel.
 */

import React, { useState } from "react";
import { Box, Text } from "ink";
import { ComposeArea } from "./compose.js";
import { getAgentColor } from "../../agent-colors.js";
import type { SlashCommandEntry } from "../slash-autocomplete.js";
import type { AgentEntry } from "../mention-autocomplete.js";

// ── Types ───────────────────────────────────────────────────────────

/** A message with grouping metadata (matches architecture spec). */
export interface ThreadMessage {
  id: string;
  sender: string;
  senderType: "agent" | "human" | "system";
  initials: string;
  content: string;
  timestamp: number;
  isFirstInGroup: boolean;
}

export interface ThreadPanelProps {
  width: number;
  focused: boolean;
  parentMessage: ThreadMessage;
  replies: ThreadMessage[];
  sourceChannelName: string;
  sourceChannelType: "channel" | "dm" | "group-dm";
  alsoSendToChannel: boolean;
  onSendReply: (content: string) => void;
  onToggleAlsoSend: () => void;
  onClose: () => void;
  slashCommands?: SlashCommandEntry[];
  agents?: AgentEntry[];
}

// ── Helpers ─────────────────────────────────────────────────────────

function formatTime(ts: number): string {
  const d = new Date(ts);
  const h = d.getHours();
  const m = d.getMinutes().toString().padStart(2, "0");
  const ampm = h >= 12 ? "PM" : "AM";
  const hour = h % 12 || 12;
  return `${hour}:${m} ${ampm}`;
}

// ── ThreadHeader ────────────────────────────────────────────────────

export function ThreadHeader({
  channelName,
  channelType,
  onClose,
}: {
  channelName: string;
  channelType: "channel" | "dm" | "group-dm";
  onClose: () => void;
}): React.JSX.Element {
  const channelLabel =
    channelType === "channel" ? `#${channelName}` : channelName;
  return (
    <Box justifyContent="space-between" paddingX={1}>
      <Box gap={1}>
        <Text bold color="white">
          Thread
        </Text>
        <Text color="cyan">{channelLabel}</Text>
      </Box>
      <Text color="gray">✕ Esc</Text>
    </Box>
  );
}

// ── ReplyDivider ────────────────────────────────────────────────────

export function ReplyDivider({
  count,
  width,
}: {
  count: number;
  width: number;
}): React.JSX.Element {
  const label = `${count} ${count === 1 ? "reply" : "replies"}`;
  const pad = Math.max(Math.floor((width - label.length - 4) / 2), 1);
  const line = "─".repeat(pad);
  return (
    <Box paddingX={1}>
      <Text color="gray">
        {line} {label} {line}
      </Text>
    </Box>
  );
}

// ── ThreadMessageItem ───────────────────────────────────────────────

function ThreadMessageItem({
  message,
}: {
  message: ThreadMessage;
}): React.JSX.Element {
  const color = getAgentColor(message.sender);
  const time = formatTime(message.timestamp);

  if (message.isFirstInGroup) {
    return (
      <Box flexDirection="column" paddingX={1}>
        <Box gap={1}>
          <Text color={color} bold>
            [{message.initials}]
          </Text>
          <Text bold color="white">
            {message.sender}
          </Text>
          <Text color="gray">{time}</Text>
        </Box>
        <Box paddingLeft={5}>
          <Text>{message.content}</Text>
        </Box>
      </Box>
    );
  }

  return (
    <Box paddingX={1} paddingLeft={6}>
      <Text>{message.content}</Text>
    </Box>
  );
}

// ── AlsoSendCheckbox ────────────────────────────────────────────────

function AlsoSendCheckbox({
  checked,
  channelName,
  channelType,
  onToggle,
}: {
  checked: boolean;
  channelName: string;
  channelType: "channel" | "dm" | "group-dm";
  onToggle: () => void;
}): React.JSX.Element {
  const icon = checked ? "☑" : "☐";
  const iconColor = checked ? "green" : "gray";
  const channelLabel =
    channelType === "channel" ? `#${channelName}` : channelName;

  return (
    <Box paddingX={1} gap={1}>
      <Text color={iconColor}>{icon}</Text>
      <Text color="gray">Also send to {channelLabel}</Text>
    </Box>
  );
}

// ── ThreadPanel ─────────────────────────────────────────────────────

export function ThreadPanel({
  width,
  focused,
  parentMessage,
  replies,
  sourceChannelName,
  sourceChannelType,
  alsoSendToChannel,
  onSendReply,
  onToggleAlsoSend,
  onClose,
  slashCommands = [],
  agents = [],
}: ThreadPanelProps): React.JSX.Element {
  return (
    <Box
      flexDirection="column"
      width={width}
      borderStyle="single"
      borderColor={focused ? "cyan" : "gray"}
    >
      {/* Header */}
      <ThreadHeader
        channelName={sourceChannelName}
        channelType={sourceChannelType}
        onClose={onClose}
      />

      {/* Divider */}
      <Box paddingX={1}>
        <Text color="gray">{"─".repeat(Math.max(width - 4, 1))}</Text>
      </Box>

      {/* Parent message */}
      <ThreadMessageItem message={parentMessage} />

      {/* Reply count divider */}
      <Box height={1} />
      <ReplyDivider count={replies.length} width={width} />
      <Box height={1} />

      {/* Replies */}
      <Box flexDirection="column" flexGrow={1}>
        {replies.map((reply) => (
          <ThreadMessageItem key={reply.id} message={reply} />
        ))}
      </Box>

      {/* Divider above compose */}
      <Box paddingX={1}>
        <Text color="gray">{"─".repeat(Math.max(width - 4, 1))}</Text>
      </Box>

      {/* Thread compose area */}
      <ComposeArea
        channelName={sourceChannelName}
        channelType={sourceChannelType}
        focused={focused}
        isThread={true}
        onSubmit={onSendReply}
        slashCommands={slashCommands}
        agents={agents}
      />

      {/* Also send checkbox */}
      <AlsoSendCheckbox
        checked={alsoSendToChannel}
        channelName={sourceChannelName}
        channelType={sourceChannelType}
        onToggle={onToggleAlsoSend}
      />
    </Box>
  );
}

export default ThreadPanel;
