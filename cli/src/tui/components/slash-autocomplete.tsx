/**
 * Slash command autocomplete overlay and hook.
 *
 * Shows a small overlay Box above the input line listing matching slash
 * commands when the input starts with "/".  Tab cycles through matches,
 * Enter accepts the current match, Escape dismisses.
 *
 * This is a standalone component — wire it into the home/conversation
 * view by:
 *   1. Calling useSlashAutocomplete(inputValue, slashCommands)
 *   2. Rendering <SlashAutocomplete {...autocomplete} /> above the input
 *   3. Intercepting Tab/Enter/Escape in useInput and calling the
 *      returned handlers (onTab, onAccept, onDismiss)
 */

import React from "react";
import { Box, Text } from "ink";

// ── Types ───────────────────────────────────────────────────────────

export interface SlashCommandEntry {
  name: string;
  description: string;
  usage?: string;
}

export interface AutocompleteState {
  /** Whether the autocomplete overlay is visible */
  visible: boolean;
  /** Filtered list of matching commands */
  matches: SlashCommandEntry[];
  /** Currently highlighted index */
  selectedIndex: number;
  /** The query text (everything after "/") */
  query: string;
}

export interface AutocompleteActions {
  /** Call when Tab is pressed — cycles to next match */
  onTab: () => AutocompleteResult | null;
  /** Call on Shift+Tab — cycles to previous match */
  onShiftTab: () => AutocompleteResult | null;
  /** Call when Enter is pressed while overlay is visible — accepts the match */
  onAccept: () => AutocompleteResult | null;
  /** Call when Escape is pressed — dismisses the overlay */
  onDismiss: () => void;
  /** Move selection up/down without accepting (for arrow keys) */
  onNavigate: (direction: number) => void;
  /** Update the autocomplete state when input changes */
  update: (input: string) => void;
}

export interface AutocompleteResult {
  /** The completed text to replace the input with (e.g. "/search ") */
  text: string;
  /** The command name that was selected */
  command: string;
}

export interface SlashAutocompleteProps {
  /** Current autocomplete state */
  state: AutocompleteState;
  /** Max number of items to show in the overlay */
  maxVisible?: number;
}

// ── Pure functions ──────────────────────────────────────────────────

/**
 * Filter and rank commands that match a prefix query.
 */
export function filterCommands(
  query: string,
  commands: SlashCommandEntry[],
): SlashCommandEntry[] {
  const q = query.toLowerCase();
  if (!q) return commands;
  return commands.filter((cmd) => cmd.name.toLowerCase().startsWith(q));
}

/**
 * Create initial autocomplete state.
 */
export function createAutocompleteState(): AutocompleteState {
  return {
    visible: false,
    matches: [],
    selectedIndex: 0,
    query: "",
  };
}

/**
 * Compute new autocomplete state from current input value.
 */
export function computeAutocompleteState(
  input: string,
  commands: SlashCommandEntry[],
): AutocompleteState {
  const trimmed = input.trimStart();

  // Only activate when input starts with "/" and has no space yet
  // (space means the user has moved on to arguments)
  if (!trimmed.startsWith("/") || trimmed.includes(" ")) {
    return createAutocompleteState();
  }

  const query = trimmed.slice(1); // everything after "/"
  const matches = filterCommands(query, commands);

  // Don't show if there's exactly one match that equals the query
  if (matches.length === 1 && matches[0].name === query) {
    return createAutocompleteState();
  }

  // Don't show if no matches
  if (matches.length === 0) {
    return { visible: false, matches: [], selectedIndex: 0, query };
  }

  return {
    visible: true,
    matches,
    selectedIndex: 0,
    query,
  };
}

/**
 * Create actions that manage autocomplete state.
 * This is a factory for use in React state management.
 */
export function createAutocompleteActions(
  state: AutocompleteState,
  setState: (s: AutocompleteState) => void,
  commands: SlashCommandEntry[],
): AutocompleteActions {
  const accept = (index: number): AutocompleteResult | null => {
    if (!state.visible || state.matches.length === 0) return null;
    const match = state.matches[index];
    if (!match) return null;
    setState(createAutocompleteState());
    return { text: `/${match.name} `, command: match.name };
  };

  return {
    onTab: () => {
      if (!state.visible || state.matches.length === 0) return null;

      // If only one match, accept it immediately
      if (state.matches.length === 1) {
        return accept(0);
      }

      // Cycle to next
      const nextIndex = (state.selectedIndex + 1) % state.matches.length;
      setState({ ...state, selectedIndex: nextIndex });
      return null;
    },

    onShiftTab: () => {
      if (!state.visible || state.matches.length === 0) return null;

      if (state.matches.length === 1) {
        return accept(0);
      }

      const nextIndex =
        (state.selectedIndex - 1 + state.matches.length) % state.matches.length;
      setState({ ...state, selectedIndex: nextIndex });
      return null;
    },

    onAccept: () => {
      return accept(state.selectedIndex);
    },

    onDismiss: () => {
      setState(createAutocompleteState());
    },

    onNavigate: (direction: number) => {
      if (!state.visible || state.matches.length <= 1) return;
      const len = state.matches.length;
      const next = ((state.selectedIndex + direction) % len + len) % len;
      setState({ ...state, selectedIndex: next });
    },

    update: (input: string) => {
      const newState = computeAutocompleteState(input, commands);
      setState(newState);
    },
  };
}

// ── React hook ──────────────────────────────────────────────────────

/**
 * Hook that manages slash command autocomplete state.
 *
 * Usage:
 * ```tsx
 * const { state, actions } = useSlashAutocomplete(commands);
 *
 * // In your onChange handler:
 * actions.update(newValue);
 *
 * // In your useInput handler:
 * if (key.name === "tab" && acState.visible) {
 *   const result = actions.onTab();
 *   if (result) setInputValue(result.text);
 *   return;
 * }
 * ```
 */
import { useState, useRef } from "react";

export function useSlashAutocomplete(commands: SlashCommandEntry[]): {
  state: AutocompleteState;
  actions: AutocompleteActions;
} {
  const [acState, setAcState] = useState<AutocompleteState>(createAutocompleteState());
  const commandsRef = useRef(commands);
  commandsRef.current = commands;
  const stateRef = useRef(acState);
  stateRef.current = acState;

  // Stable reference — reads state/commands through refs so the object never changes
  const actions = React.useMemo<AutocompleteActions>(() => {
    const accept = (index: number): AutocompleteResult | null => {
      const s = stateRef.current;
      if (!s.visible || s.matches.length === 0) return null;
      const match = s.matches[index];
      if (!match) return null;
      setAcState(createAutocompleteState());
      return { text: `/${match.name} `, command: match.name };
    };

    return {
      onTab: () => {
        const s = stateRef.current;
        if (!s.visible || s.matches.length === 0) return null;
        if (s.matches.length === 1) return accept(0);
        const next = (s.selectedIndex + 1) % s.matches.length;
        setAcState({ ...s, selectedIndex: next });
        return null;
      },
      onShiftTab: () => {
        const s = stateRef.current;
        if (!s.visible || s.matches.length === 0) return null;
        if (s.matches.length === 1) return accept(0);
        const next = (s.selectedIndex - 1 + s.matches.length) % s.matches.length;
        setAcState({ ...s, selectedIndex: next });
        return null;
      },
      onAccept: () => accept(stateRef.current.selectedIndex),
      onDismiss: () => setAcState(createAutocompleteState()),
      onNavigate: (direction: number) => {
        const s = stateRef.current;
        if (!s.visible || s.matches.length <= 1) return;
        const len = s.matches.length;
        const next = ((s.selectedIndex + direction) % len + len) % len;
        setAcState({ ...s, selectedIndex: next });
      },
      update: (input: string) => {
        const newState = computeAutocompleteState(input, commandsRef.current);
        const cur = stateRef.current;
        // Skip setState when nothing meaningful changed to avoid extra renders
        if (
          cur.visible === newState.visible &&
          cur.query === newState.query &&
          cur.selectedIndex === newState.selectedIndex &&
          cur.matches.length === newState.matches.length
        ) return;
        setAcState(newState);
      },
    };
  }, []); // stable — reads through refs

  return { state: acState, actions };
}

// ── Component ───────────────────────────────────────────────────────

/**
 * Overlay rendering the list of matching slash commands.
 * Position this above the input line.
 */
export function SlashAutocomplete({
  state,
  maxVisible = 8,
}: SlashAutocompleteProps): React.JSX.Element | null {
  if (!state.visible || state.matches.length === 0) {
    return null;
  }

  // Determine visible window
  const total = state.matches.length;
  const visible = Math.min(total, maxVisible);
  let startIdx = 0;

  if (state.selectedIndex >= startIdx + visible) {
    startIdx = state.selectedIndex - visible + 1;
  }
  if (state.selectedIndex < startIdx) {
    startIdx = state.selectedIndex;
  }

  const visibleItems = state.matches.slice(startIdx, startIdx + visible);

  return (
    <Box
      flexDirection="column"
      paddingX={1}
      borderStyle="single"
      borderColor="cyan"
    >
      {visibleItems.map((cmd, i) => {
        const actualIndex = startIdx + i;
        const isSelected = actualIndex === state.selectedIndex;

        return (
          <Box key={cmd.name} gap={1}>
            <Text
              color={isSelected ? "cyan" : undefined}
              bold={isSelected}
              dimColor={!isSelected}
            >
              {isSelected ? ">" : " "}
            </Text>
            <Text color={isSelected ? "cyan" : "white"} bold={isSelected}>
              {`/${cmd.name}`}
            </Text>
            <Text dimColor>{cmd.description}</Text>
          </Box>
        );
      })}

      {/* Scroll indicators */}
      {total > visible && (
        <Box justifyContent="flex-end">
          <Text dimColor>
            {`${state.selectedIndex + 1}/${total}`}
          </Text>
        </Box>
      )}
    </Box>
  );
}

export default SlashAutocomplete;
