/**
 * Slack-style message rendering components.
 *
 * Includes message grouping logic, date separators, unread markers,
 * thread indicators, system messages, and the scrollable message list.
 */

import React, { useMemo, useEffect, useRef } from "react";
import { Box, Text } from "ink";
import type { ReactNode } from "react";
import { getAgentColor } from "../../agent-colors.js";
import { Markdown } from "../markdown.js";

// ── Types ───────────────────────────────────────────────────────────

export interface ChatMessageInput {
  id: string;
  sender: string;
  senderType: "agent" | "human" | "system";
  content: string;
  timestamp: number;
  threadReplyCount?: number;
  threadParticipants?: string[];
  threadLastReply?: number;
  reactions?: ReactionData[];
  edited?: boolean;
  isError?: boolean;
}

export interface ReactionData {
  emoji: string;
  count: number;
  reacted: boolean;
}

export interface GroupedMessage {
  id: string;
  sender: string;
  senderType: "agent" | "human" | "system";
  initials: string;
  content: string;
  timestamp: number;
  isFirstInGroup: boolean;
  threadReplyCount?: number;
  threadParticipants?: string[];
  threadLastReply?: number;
  reactions?: ReactionData[];
  edited?: boolean;
  isSystem?: boolean;
  dateSeparator?: string;
  isUnreadMarker?: boolean;
  isError?: boolean;
}

// ── Time Formatting ─────────────────────────────────────────────────

function formatTime(timestamp: number): string {
  const d = new Date(timestamp);
  const hours = d.getHours();
  const minutes = d.getMinutes().toString().padStart(2, "0");
  const ampm = hours >= 12 ? "PM" : "AM";
  const h = hours % 12 || 12;
  return `${h}:${minutes} ${ampm}`;
}

function formatTimeWithDate(timestamp: number): string {
  const d = new Date(timestamp);
  const now = new Date();
  const isToday =
    d.getFullYear() === now.getFullYear() &&
    d.getMonth() === now.getMonth() &&
    d.getDate() === now.getDate();

  if (isToday) return formatTime(timestamp);

  const month = d.toLocaleDateString("en-US", { month: "short" });
  const day = d.getDate();
  return `${month} ${day}, ${formatTime(timestamp)}`;
}

function getDateLabel(timestamp: number): string {
  const d = new Date(timestamp);
  const now = new Date();

  const isToday =
    d.getFullYear() === now.getFullYear() &&
    d.getMonth() === now.getMonth() &&
    d.getDate() === now.getDate();

  if (isToday) return "Today";

  const yesterday = new Date(now);
  yesterday.setDate(yesterday.getDate() - 1);
  const isYesterday =
    d.getFullYear() === yesterday.getFullYear() &&
    d.getMonth() === yesterday.getMonth() &&
    d.getDate() === yesterday.getDate();

  if (isYesterday) return "Yesterday";

  return d.toLocaleDateString("en-US", {
    weekday: "long",
    month: "long",
    day: "numeric",
  });
}

// ── groupMessages ───────────────────────────────────────────────────

const GROUP_WINDOW_MS = 5 * 60 * 1000; // 5 minutes

export function groupMessages(
  messages: ChatMessageInput[],
  unreadAfterTimestamp?: number,
): GroupedMessage[] {
  const result: GroupedMessage[] = [];
  let lastSender = "";
  let lastTimestamp = 0;
  let lastDateKey = "";
  let unreadMarkerInserted = false;

  for (const msg of messages) {
    const msgDate = new Date(msg.timestamp);
    const dateKey = `${msgDate.getFullYear()}-${msgDate.getMonth()}-${msgDate.getDate()}`;

    // Date separator
    let dateSeparator: string | undefined;
    if (dateKey !== lastDateKey) {
      dateSeparator = getDateLabel(msg.timestamp);
      lastDateKey = dateKey;
    }

    // Unread marker
    let isUnreadMarker = false;
    if (
      !unreadMarkerInserted &&
      unreadAfterTimestamp !== undefined &&
      msg.timestamp > unreadAfterTimestamp
    ) {
      isUnreadMarker = true;
      unreadMarkerInserted = true;
    }

    // Grouping: same sender within 5 min = continuation
    const isFirstInGroup =
      msg.sender !== lastSender ||
      msg.timestamp - lastTimestamp > GROUP_WINDOW_MS ||
      dateSeparator !== undefined ||
      msg.senderType === "system";

    // Compute initials
    const initials = msg.sender
      .split(/[\s-]+/)
      .slice(0, 2)
      .map((w) => w[0]?.toUpperCase() ?? "")
      .join("");

    result.push({
      id: msg.id,
      sender: msg.sender,
      senderType: msg.senderType,
      initials,
      content: msg.content,
      timestamp: msg.timestamp,
      isFirstInGroup,
      isSystem: msg.senderType === "system",
      dateSeparator,
      isUnreadMarker,
      threadReplyCount: msg.threadReplyCount,
      threadParticipants: msg.threadParticipants,
      threadLastReply: msg.threadLastReply,
      reactions: msg.reactions,
      edited: msg.edited,
      isError: msg.isError,
    });

    lastSender = msg.sender;
    lastTimestamp = msg.timestamp;
  }

  return result;
}

// ── DateSeparator ───────────────────────────────────────────────────

export interface DateSeparatorProps {
  label: string;
  width?: number;
}

export function DateSeparator({
  label,
  width = 60,
}: DateSeparatorProps): React.JSX.Element {
  const labelWithPad = ` ${label} `;
  const remaining = Math.max(0, width - labelWithPad.length);
  const left = Math.floor(remaining / 2);
  const right = remaining - left;
  const line = "─".repeat(left) + labelWithPad + "─".repeat(right);

  return (
    <Box justifyContent="center" marginY={0}>
      <Text color="gray">{line}</Text>
    </Box>
  );
}

// ── UnreadSeparator ─────────────────────────────────────────────────

export interface UnreadSeparatorProps {
  width?: number;
}

export function UnreadSeparator({
  width = 60,
}: UnreadSeparatorProps): React.JSX.Element {
  const labelWithPad = " New ";
  const remaining = Math.max(0, width - labelWithPad.length);
  const left = Math.floor(remaining / 2);
  const right = remaining - left;
  const line = "─".repeat(left) + labelWithPad + "─".repeat(right);

  return (
    <Box justifyContent="center" marginY={0}>
      <Text color="red" bold>
        {line}
      </Text>
    </Box>
  );
}

// ── SystemMessage ───────────────────────────────────────────────────

export interface SystemMessageProps {
  content: string;
  timestamp: number;
}

export function SystemMessage({
  content,
  timestamp,
}: SystemMessageProps): React.JSX.Element {
  return (
    <Box justifyContent="center" paddingX={4}>
      <Text color="gray" dimColor italic>
        {"✦ "}
        {content}
      </Text>
    </Box>
  );
}

// ── ThreadIndicator ─────────────────────────────────────────────────

export interface ThreadIndicatorProps {
  replyCount: number;
  lastReplyTimestamp: number;
  onClick?: () => void;
}

export function ThreadIndicator({
  replyCount,
  lastReplyTimestamp,
  onClick,
}: ThreadIndicatorProps): React.JSX.Element {
  const replyText = replyCount === 1 ? "1 reply" : `${replyCount} replies`;
  const lastReply = `Last reply ${formatTimeWithDate(lastReplyTimestamp)}`;

  return (
    <Box paddingLeft={6}>
      <Text color="cyan">
        {"↳ "}
        {replyText}
        {"  "}
        {lastReply}
      </Text>
    </Box>
  );
}

// ── MessageGroupComponent ───────────────────────────────────────────

interface MessageRowProps {
  message: GroupedMessage;
  onThreadOpen?: (messageId: string) => void;
}

function FirstMessage({
  message,
  onThreadOpen,
}: MessageRowProps): React.JSX.Element {
  const isHuman = message.senderType === "human";
  const avatarChar = isHuman ? ">" : message.initials.charAt(0) || "?";
  const nameColor = isHuman ? "white" : getAgentColor(message.sender);
  const time = formatTimeWithDate(message.timestamp);

  return (
    <Box flexDirection="column">
      <Box>
        <Text color={nameColor} bold>
          {"["}
          {avatarChar}
          {"] "}
        </Text>
        <Text color={nameColor} bold>
          {message.sender}
        </Text>
        <Text color="gray" dimColor>
          {"  "}
          {time}
        </Text>
      </Box>
      <Box paddingLeft={4}>
        <Markdown content={message.content} />
      </Box>
      {message.edited && (
        <Box paddingLeft={4}>
          <Text color="gray" dimColor>
            (edited)
          </Text>
        </Box>
      )}
      {message.threadReplyCount != null &&
        message.threadReplyCount > 0 &&
        message.threadLastReply != null && (
          <ThreadIndicator
            replyCount={message.threadReplyCount}
            lastReplyTimestamp={message.threadLastReply}
            onClick={onThreadOpen ? () => onThreadOpen(message.id) : undefined}
          />
        )}
    </Box>
  );
}

function ContinuationMessage({
  message,
  onThreadOpen,
}: MessageRowProps): React.JSX.Element {
  return (
    <Box flexDirection="column">
      <Box paddingLeft={4}>
        <Markdown content={message.content} />
      </Box>
      {message.edited && (
        <Box paddingLeft={4}>
          <Text color="gray" dimColor>
            (edited)
          </Text>
        </Box>
      )}
      {message.threadReplyCount != null &&
        message.threadReplyCount > 0 &&
        message.threadLastReply != null && (
          <ThreadIndicator
            replyCount={message.threadReplyCount}
            lastReplyTimestamp={message.threadLastReply}
            onClick={onThreadOpen ? () => onThreadOpen(message.id) : undefined}
          />
        )}
    </Box>
  );
}

// ── SlackMessageList ────────────────────────────────────────────────

export interface SlackMessageListProps {
  messages: ChatMessageInput[];
  unreadAfterTimestamp?: number;
  onThreadOpen?: (messageId: string) => void;
  width?: number;
}

export function SlackMessageList({
  messages,
  unreadAfterTimestamp,
  onThreadOpen,
  width = 60,
}: SlackMessageListProps): React.JSX.Element {
  const grouped = useMemo(
    () => groupMessages(messages, unreadAfterTimestamp),
    [messages, unreadAfterTimestamp],
  );

  return (
    <Box flexDirection="column" flexGrow={1}>
      {grouped.map((msg) => {
        const elements: ReactNode[] = [];

        // Date separator before this message
        if (msg.dateSeparator) {
          elements.push(
            <DateSeparator
              key={`date-${msg.id}`}
              label={msg.dateSeparator}
              width={width}
            />,
          );
        }

        // Unread marker before this message
        if (msg.isUnreadMarker) {
          elements.push(
            <UnreadSeparator key={`unread-${msg.id}`} width={width} />,
          );
        }

        // The message itself
        if (msg.isSystem) {
          elements.push(
            <SystemMessage
              key={msg.id}
              content={msg.content}
              timestamp={msg.timestamp}
            />,
          );
        } else if (msg.isFirstInGroup) {
          elements.push(
            <FirstMessage
              key={msg.id}
              message={msg}
              onThreadOpen={onThreadOpen}
            />,
          );
        } else {
          elements.push(
            <ContinuationMessage
              key={msg.id}
              message={msg}
              onThreadOpen={onThreadOpen}
            />,
          );
        }

        return <Box key={`row-${msg.id}`} flexDirection="column">{elements}</Box>;
      })}
    </Box>
  );
}

export default SlackMessageList;
