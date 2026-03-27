/**
 * Unified home screen — channel chat + compact calendar strip.
 *
 * Layout (top → bottom):
 *   ┌─ ChannelBar ────────────────────────────────────────┐
 *   │  #general  ◀ Tab ▶  #leads  #seo                   │
 *   ├─ MessageArea ───────────────────────────────────────┤
 *   │  [SEO Analyst] Found 3 new keyword opportunities    │
 *   │  > you: Great, focus on the keyword opportunities   │
 *   ├─ CalendarStrip (collapsible) ───────────────────────┤
 *   │  Today          Tomorrow       Wed                  │
 *   │  ■ 09:00 SEO    ■ 10:00 Lead  ■ 09:00 Research    │
 *   ├─ InputBar ──────────────────────────────────────────┤
 *   │  > Type a message...                                │
 *   └────────────────────────────────────────────────────-┘
 */

import React, { useState, useMemo, useEffect, useCallback } from "react";
import { Box, Text, useStdout } from "ink";
import { TextInput } from "@inkjs/ui";
import { getAgentColor } from "../agent-colors.js";
import type { AgentColor } from "../agent-colors.js";
import { getChannelColor } from "../channel-colors.js";
import type { ChannelColor } from "../channel-colors.js";
import type { SelectOption } from "../components/inline-select.js";
import { InlineSelect } from "../components/inline-select.js";
import { InlineConfirm } from "../components/inline-confirm.js";
import {
  useSlashAutocomplete,
  SlashAutocomplete,
} from "../components/slash-autocomplete.js";
import type { SlashCommandEntry } from "../components/slash-autocomplete.js";
import {
  useMentionAutocomplete,
  MentionAutocomplete,
} from "../components/mention-autocomplete.js";
import type { AgentEntry } from "../components/mention-autocomplete.js";
import { Markdown } from "../components/markdown.js";
import { Spinner } from "../components/spinner.js";
import { ErrorBox, categorizeError } from "../components/error-box.js";
import { Banner } from "../components/banner.js";

// ── Types ────────────────────────────────────────────────────────────

export interface HomeChannel {
  id: string;
  name: string;
  unread: number;
}

export interface HomeMessage {
  id: string;
  sender: string;
  senderType: "agent" | "human" | "system";
  content: string;
  timestamp: number;
  channelName?: string;
  isError?: boolean;
}

export interface HomeCalendarEvent {
  agentName: string;
  agentColor: string;
  time: string;   // "09:00"
  day: string;    // "Today", "Tomorrow", "Wed"
}

export interface PickerState {
  title: string;
  options: SelectOption[];
  onSelect: (value: string) => void;
}

export interface ConfirmState {
  question: string;
  onConfirm: (confirmed: boolean) => void;
}

export interface HomeScreenProps {
  // Chat
  channels: HomeChannel[];
  activeChannelId: string;
  messages: HomeMessage[];
  onSend: (content: string) => void;
  onChannelChange: (channelId: string) => void;

  // Calendar
  calendarEvents: HomeCalendarEvent[];
  showCalendar: boolean;

  // Autocomplete
  slashCommands?: SlashCommandEntry[];
  agents?: AgentEntry[];

  // State
  isLoading?: boolean;
  loadingHint?: string;

  // tmux
  isTmux?: boolean;

  // Inline interactive widgets (replace text input when active)
  picker?: PickerState | null;
  confirm?: ConfirmState | null;
}

// ── Helpers ──────────────────────────────────────────────────────────

function formatTime(ts: number): string {
  const d = new Date(ts);
  const h = String(d.getHours()).padStart(2, "0");
  const m = String(d.getMinutes()).padStart(2, "0");
  return `${h}:${m}`;
}

// ── Sub-components ───────────────────────────────────────────────────

/** Horizontal tab strip showing channel names. */
function ChannelBar({
  channels,
  activeChannelId,
}: {
  channels: HomeChannel[];
  activeChannelId: string;
}) {
  return (
    <Box paddingX={1}>
      {channels.map((ch, idx) => {
        const isActive = ch.id === activeChannelId;
        const chColor = getChannelColor(ch.name);
        return (
          <React.Fragment key={ch.id}>
            {idx > 0 && <Text dimColor>{"  "}</Text>}
            <Text bold={isActive} color={chColor} dimColor={!isActive}>
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

/** A single message row with channel-colored left border and agent-coloured prefix. */
function AgentMessage({ msg, channelColor }: { msg: HomeMessage; channelColor?: ChannelColor }) {
  const borderColor = channelColor ?? "gray";

  if (msg.senderType === "system") {
    return (
      <Box paddingX={1}>
        <Text color={borderColor}>{"\u2502 "}</Text>
        <Text color="yellow" dimColor>
          {msg.content}
        </Text>
      </Box>
    );
  }

  if (msg.senderType === "human") {
    // User messages: white text, subtle gray background (like Claude Code)
    return (
      <Box paddingX={1}>
        <Text color={borderColor}>{"\u2502 "}</Text>
        <Text dimColor>{formatTime(msg.timestamp)}</Text>
        <Text>{" "}</Text>
        <Text bold color="white" backgroundColor="gray">
          {" > "}
        </Text>
        <Text color="white">{" "}{msg.content}</Text>
      </Box>
    );
  }

  // Agent/wuphf response — render via Markdown or ErrorBox
  const isError = msg.isError || msg.content.startsWith("Error:");
  const agentColor = getAgentColor(msg.sender);
  return (
    <Box paddingX={1} flexDirection="column">
      <Box>
        <Text color={borderColor}>{"\u2502 "}</Text>
        <Text dimColor>{formatTime(msg.timestamp)}</Text>
        <Text>{" "}</Text>
        <Text bold color={agentColor}>
          {"["}{msg.sender}{"]"}
        </Text>
      </Box>
      <Box paddingLeft={4}>
        {isError ? (
          <ErrorBox message={msg.content} category={categorizeError(new Error(msg.content))} />
        ) : (
          <Markdown content={msg.content} />
        )}
      </Box>
    </Box>
  );
}

/** Compact 3-day calendar strip. */
function CalendarStrip({ events }: { events: HomeCalendarEvent[] }) {
  // Group events by day column
  const dayOrder = [...new Set(events.map((e) => e.day))].slice(0, 3);

  if (dayOrder.length === 0) {
    return (
      <Box paddingX={2}>
        <Text dimColor>{"No upcoming events."}</Text>
      </Box>
    );
  }

  // Collect events per day
  const byDay = new Map<string, HomeCalendarEvent[]>();
  for (const day of dayOrder) {
    byDay.set(day, events.filter((e) => e.day === day));
  }

  // Find max row count
  const maxRows = Math.max(...Array.from(byDay.values()).map((v) => v.length), 0);
  const colWidth = 18;

  return (
    <Box flexDirection="column" paddingX={2}>
      {/* Day headers */}
      <Box>
        {dayOrder.map((day) => (
          <Box key={day} width={colWidth}>
            <Text bold dimColor>{day}</Text>
          </Box>
        ))}
      </Box>

      {/* Event rows */}
      {Array.from({ length: Math.min(maxRows, 4) }, (_, rowIdx) => (
        <Box key={rowIdx}>
          {dayOrder.map((day) => {
            const dayEvents = byDay.get(day) ?? [];
            const ev = dayEvents[rowIdx];
            if (!ev) {
              return (
                <Box key={day} width={colWidth}>
                  <Text>{" "}</Text>
                </Box>
              );
            }
            return (
              <Box key={day} width={colWidth}>
                <Text color={ev.agentColor as AgentColor}>{"\u25A0 "}</Text>
                <Text dimColor>{ev.time}</Text>
                <Text>{" "}</Text>
                <Text color={ev.agentColor as AgentColor}>
                  {ev.agentName.slice(0, 8)}
                </Text>
              </Box>
            );
          })}
        </Box>
      ))}
    </Box>
  );
}

// ── Main component ───────────────────────────────────────────────────

export function HomeScreen({
  channels,
  activeChannelId,
  messages,
  onSend,
  onChannelChange: _onChannelChange,
  calendarEvents,
  showCalendar,
  slashCommands = [],
  agents = [],
  isLoading = false,
  loadingHint = "thinking...",
  isTmux = false,
  picker = null,
  confirm = null,
}: HomeScreenProps): React.JSX.Element {
  const [submitKey, setSubmitKey] = useState(0);
  const [inputValue, setInputValue] = useState("");
  const [showBanner, setShowBanner] = useState(true);
  const { stdout } = useStdout();
  const rows = stdout?.rows ?? 24;
  const cols = stdout?.columns ?? 80;

  // Auto-dismiss banner after 5 seconds
  useEffect(() => {
    if (!showBanner) return;
    const id = setTimeout(() => setShowBanner(false), 5000);
    return () => clearTimeout(id);
  }, [showBanner]);

  // Reserve rows for UI chrome: channel bar (1) + dividers (2) + input (1) + calendar (~5) + padding
  const calendarRows = showCalendar ? 6 : 0;
  const chromeRows = 3 + calendarRows;
  const visibleMessageRows = Math.max(rows - chromeRows - 5, 4);

  // Tail the message list
  const displayMessages = useMemo(
    () => messages.slice(-visibleMessageRows),
    [messages, visibleMessageRows],
  );

  // ── Slash autocomplete ──
  const { state: slashState, actions: slashActions } =
    useSlashAutocomplete(slashCommands);

  // ── @mention autocomplete ──
  const { state: mentionState, actions: mentionActions } =
    useMentionAutocomplete(agents, inputValue);

  const handleInputChange = useCallback(
    (value: string) => {
      setInputValue(value);
      // Only update slash autocomplete if "/" prefix
      if (value.startsWith("/")) {
        slashActions.update(value);
      } else if (slashState.visible) {
        slashActions.onDismiss();
      }
      // Only update mention autocomplete if "@" is present AND we have agents
      if (value.includes("@") && agents.length > 0) {
        mentionActions.update(value);
      } else if (mentionState.visible) {
        mentionActions.onDismiss();
      }
    },
    [slashActions, mentionActions, slashState.visible, mentionState.visible, agents.length],
  );

  const handleSubmit = useCallback((value: string) => {
    if (!value.trim()) return;
    if (showBanner) setShowBanner(false);

    // If slash autocomplete is visible, accept the selection instead
    if (slashState.visible) {
      const result = slashActions.onAccept();
      if (result) {
        setInputValue(result.text);
        return;
      }
    }

    // If mention autocomplete is visible, accept the selection instead
    if (mentionState.visible) {
      const result = mentionActions.onAccept();
      if (result) {
        setInputValue(result.text);
        return;
      }
    }

    onSend(value);
    setInputValue("");
    setSubmitKey((k) => k + 1);
  }, [showBanner, slashState.visible, mentionState.visible, slashActions, mentionActions, onSend]);

  // Expose Tab handler via globalThis so app.tsx can invoke it.
  // app.tsx's useInput fires BEFORE TextInput, so Tab is intercepted there.
  useEffect(() => {
    const tabComplete = (direction: number): boolean => {
      if (slashState.visible) {
        const result =
          direction < 0 ? slashActions.onShiftTab() : slashActions.onTab();
        if (result) setInputValue(result.text);
        return true;
      }
      if (mentionState.visible) {
        const result =
          direction < 0 ? mentionActions.onShiftTab() : mentionActions.onTab();
        if (result) setInputValue(result.text);
        return true;
      }
      return false;
    };

    (globalThis as Record<string, unknown>).__nexHomeTabComplete = tabComplete;
    return () => {
      delete (globalThis as Record<string, unknown>).__nexHomeTabComplete;
    };
  }, [slashState, mentionState, slashActions, mentionActions]);

  const divider = "\u2500".repeat(Math.min(cols, 120) - 2);

  // Resolve active channel color for message borders
  const activeChannelName = channels.find((c) => c.id === activeChannelId)?.name;
  const activeChColor = activeChannelName ? getChannelColor(activeChannelName) : ("gray" as ChannelColor);

  return (
    <Box flexDirection="column" width="100%" minHeight={visibleMessageRows + chromeRows}>
      {/* Animated banner (shown on first render, dismissed after input or 5s) */}
      {showBanner && <Banner />}

      {/* Channel bar */}
      <ChannelBar channels={channels} activeChannelId={activeChannelId} />

      {/* Divider */}
      <Box paddingX={1}>
        <Text dimColor>{divider}</Text>
      </Box>

      {/* Message area */}
      <Box flexDirection="column" flexGrow={1}>
        {displayMessages.map((msg) => {
          // Use per-message channel color if available, else active channel
          const msgChColor = msg.channelName
            ? getChannelColor(msg.channelName)
            : activeChColor;
          return (
            <Box key={msg.id} marginBottom={0}>
              <AgentMessage msg={msg} channelColor={msgChColor} />
            </Box>
          );
        })}

        {/* Loading indicator */}
        {isLoading && (
          <Box paddingX={2}>
            <Spinner label={loadingHint || "thinking..."} />
          </Box>
        )}

        {displayMessages.length === 0 && !isLoading && (
          <Box paddingX={2}>
            <Text dimColor>{"No messages yet. Type something to get started."}</Text>
          </Box>
        )}
      </Box>

      {/* Calendar strip (collapsible) */}
      {showCalendar && (
        <>
          <Box paddingX={1}>
            <Text dimColor>{divider}</Text>
          </Box>
          <CalendarStrip events={calendarEvents} />
        </>
      )}

      {/* Autocomplete overlays (rendered above the divider) */}
      <SlashAutocomplete state={slashState} />
      <MentionAutocomplete state={mentionState} />

      {/* Divider */}
      <Box paddingX={1}>
        <Text dimColor>{divider}</Text>
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
            onChange={handleInputChange}
            onSubmit={handleSubmit}
          />
        </Box>
      )}

      {/* tmux hint */}
      {isTmux && (
        <Box paddingX={1}>
          <Text dimColor>{"tmux detected: Ctrl+B % for split pane"}</Text>
        </Box>
      )}
    </Box>
  );
}

// TODO: Wire DataTable component for tabular output (object lists, search results).
// Currently dispatch returns flat text — needs structured data format from dispatch layer
// before DataTable can be rendered in AgentMessage. See src/tui/components/data-table.tsx.

export default HomeScreen;
