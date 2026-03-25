/**
 * Slack-style compose area component.
 *
 * Message input at the bottom of the main panel with slash command
 * and @mention autocomplete integration. Uses TextInput with a
 * submitKey remount trick for clearing after send.
 */

import React, { useState, useEffect, useCallback, useRef } from "react";
import { Box, Text } from "ink";
import { TextInput } from "@inkjs/ui";
import {
  SlashAutocomplete,
  useSlashAutocomplete,
} from "../slash-autocomplete.js";
import type { SlashCommandEntry } from "../slash-autocomplete.js";
import {
  MentionAutocomplete,
  useMentionAutocomplete,
} from "../mention-autocomplete.js";
import type { AgentEntry } from "../mention-autocomplete.js";

// ── Types ───────────────────────────────────────────────────────────

export interface ComposeAreaProps {
  channelName: string;
  channelType: "channel" | "dm" | "group-dm";
  /** DM recipient name for placeholder */
  recipientName?: string;
  focused: boolean;
  isThread?: boolean;
  onSubmit: (value: string) => void;
  onSlashCommand?: (command: string, args: string) => void;
  slashCommands: SlashCommandEntry[];
  agents: AgentEntry[];
}

// ── Helpers ─────────────────────────────────────────────────────────

function getPlaceholder(
  channelName: string,
  channelType: "channel" | "dm" | "group-dm",
  recipientName?: string,
  isThread?: boolean,
): string {
  if (isThread) return "Reply in thread...";
  if (channelType === "dm" && recipientName) {
    return `Message ${recipientName}`;
  }
  if (channelType === "group-dm") {
    return `Message ${channelName}`;
  }
  return `Message #${channelName}`;
}

// ── HintBar ─────────────────────────────────────────────────────────

export function HintBar({
  visible,
}: {
  visible: boolean;
}): React.JSX.Element | null {
  if (!visible) return null;
  return (
    <Box paddingX={1}>
      <Text color="gray">@ mention · / command · Enter send</Text>
    </Box>
  );
}

// ── ComposeArea ─────────────────────────────────────────────────────

export function ComposeArea({
  channelName,
  channelType,
  recipientName,
  focused,
  isThread,
  onSubmit,
  onSlashCommand,
  slashCommands,
  agents,
}: ComposeAreaProps): React.JSX.Element {
  const [value, setValue] = useState("");
  const [submitKey, setSubmitKey] = useState(0);

  // When accepting an autocomplete match, we remount TextInput with this as defaultValue.
  // On normal submit (send message), this is "" so TextInput starts empty.
  const nextDefaultRef = useRef("");

  // Autocomplete hooks
  const slash = useSlashAutocomplete(slashCommands);
  const mention = useMentionAutocomplete(agents, value);

  const placeholder = getPlaceholder(
    channelName,
    channelType,
    recipientName,
    isThread,
  );

  // Refs so all callbacks can read current state/actions without being recreated
  const slashRef = useRef(slash);
  slashRef.current = slash;
  const mentionRef = useRef(mention);
  mentionRef.current = mention;

  /** Remount TextInput with a new defaultValue and sync React state. */
  const remountInput = useCallback((text: string) => {
    nextDefaultRef.current = text;
    setValue(text);
    setSubmitKey((k) => k + 1);
  }, []);

  // Stable ([] deps) — reads actions through refs so TextInput never gets a new onChange
  const handleChange = useCallback(
    (newValue: string) => {
      setValue(newValue);
      slashRef.current.actions.update(newValue);
      mentionRef.current.actions.update(newValue);
    },
    [], // eslint-disable-line react-hooks/exhaustive-deps
  );

  // Handle submit — Enter accepts first autocomplete item if overlay is visible
  const handleSubmit = useCallback(
    (submitted: string) => {
      // If slash autocomplete is visible, Enter accepts the selected (or first) match
      if (slashRef.current.state.visible) {
        const result = slashRef.current.actions.onAccept();
        if (result) {
          remountInput(result.text);
          return;
        }
      }
      // If mention autocomplete is visible, Enter accepts the selected (or first) match
      if (mentionRef.current.state.visible) {
        const result = mentionRef.current.actions.onAccept();
        if (result) {
          remountInput(result.text);
          return;
        }
      }

      const trimmed = submitted.trim();
      if (!trimmed) return;

      // Check for slash commands
      if (trimmed.startsWith("/")) {
        const spaceIdx = trimmed.indexOf(" ");
        const command =
          spaceIdx > 0 ? trimmed.slice(1, spaceIdx) : trimmed.slice(1);
        const args = spaceIdx > 0 ? trimmed.slice(spaceIdx + 1).trim() : "";
        if (onSlashCommand) {
          onSlashCommand(command, args);
        } else {
          onSubmit(trimmed);
        }
      } else {
        onSubmit(trimmed);
      }

      // Clear input via remount trick
      remountInput("");
    },
    [onSubmit, onSlashCommand, remountInput],
  );

  useEffect(() => {
    const g = globalThis as Record<string, unknown>;

    // Tab/Shift+Tab: cycle autocomplete or accept single match
    g.__nexHomeTabComplete = (direction: number): boolean => {
      const s = slashRef.current;
      const m = mentionRef.current;
      if (s.state.visible) {
        const result = direction > 0 ? s.actions.onTab() : s.actions.onShiftTab();
        if (result) remountInput(result.text);
        return true;
      }
      if (m.state.visible) {
        const result = direction > 0 ? m.actions.onTab() : m.actions.onShiftTab();
        if (result) remountInput(result.text);
        return true;
      }
      return false;
    };

    // Up/Down arrow: navigate autocomplete selection
    g.__nexHomeAutocompleteNav = (direction: number): boolean => {
      const s = slashRef.current;
      const m = mentionRef.current;
      if (s.state.visible) {
        s.actions.onNavigate(direction);
        return true;
      }
      if (m.state.visible) {
        m.actions.onNavigate(direction);
        return true;
      }
      return false;
    };

    return () => {
      delete g.__nexHomeTabComplete;
      delete g.__nexHomeAutocompleteNav;
    };
  }, [remountInput]); // register once, refs keep it current

  return (
    <Box flexDirection="column">
      {/* Autocomplete overlays (render above input) */}
      <SlashAutocomplete state={slash.state} maxVisible={6} />
      <MentionAutocomplete state={mention.state} maxVisible={6} />

      {/* Hint bar */}
      <HintBar visible={focused} />

      {/* Input box */}
      <Box
        borderStyle="single"
        borderColor={focused ? "cyan" : "gray"}
        flexDirection="column"
        paddingX={1}
      >
        <Box justifyContent="space-between">
          {focused
            ? <Text color="black" backgroundColor="cyan" bold>{" COMPOSE "}</Text>
            : <Text color="gray">
                {isThread
                  ? "Reply"
                  : channelType === "channel"
                    ? `Message #${channelName}`
                    : `Message ${channelName}`}
              </Text>
          }
          {focused && <Text color="gray">↑↓ · Tab · Enter</Text>}
        </Box>
        <Box>
          {focused ? (
            <TextInput
              key={submitKey}
              defaultValue={nextDefaultRef.current}
              placeholder={placeholder}
              onChange={handleChange}
              onSubmit={handleSubmit}
            />
          ) : (
            <Text dimColor>{placeholder}</Text>
          )}
        </Box>
        {!focused && (
          <Box justifyContent="flex-end">
            <Text dimColor>Tab=focus</Text>
          </Box>
        )}
      </Box>
    </Box>
  );
}

export default ComposeArea;
