/**
 * @agent mention autocomplete overlay and hook.
 *
 * Shows a dropdown of matching agents when the user types "@" in the input.
 * Tab cycles through matches, Enter accepts, Escape dismisses.
 *
 * Mirrors the API of slash-autocomplete.tsx for consistency.
 */

import React, { useState, useRef } from "react";
import { Box, Text } from "ink";
import { getAgentColor } from "../agent-colors.js";
import type { AgentColor } from "../agent-colors.js";

// ── Types ───────────────────────────────────────────────────────────

export interface AgentEntry {
  slug: string;
  name: string;
}

export interface MentionState {
  /** Whether the mention overlay is visible */
  visible: boolean;
  /** Filtered list of matching agents */
  matches: AgentEntry[];
  /** Currently highlighted index */
  selectedIndex: number;
  /** The query text (everything after "@") */
  query: string;
  /** Position of the "@" in the input string */
  atIndex: number;
}

export interface MentionActions {
  /** Call when Tab is pressed — cycles to next match */
  onTab: () => MentionResult | null;
  /** Call on Shift+Tab — cycles to previous match */
  onShiftTab: () => MentionResult | null;
  /** Call when Enter is pressed while overlay is visible — accepts the match */
  onAccept: () => MentionResult | null;
  /** Call when Escape is pressed — dismisses the overlay */
  onDismiss: () => void;
  /** Move selection up/down without accepting (for arrow keys) */
  onNavigate: (direction: number) => void;
  /** Update the mention state when input changes */
  update: (input: string) => void;
}

export interface MentionResult {
  /** The full input text with the @mention replaced */
  text: string;
  /** The agent slug that was selected */
  agentSlug: string;
}

export interface MentionAutocompleteProps {
  state: MentionState;
  maxVisible?: number;
}

// ── Pure functions ──────────────────────────────────────────────────

/**
 * Filter agents that match a prefix query (case-insensitive).
 */
export function filterAgents(
  query: string,
  agents: AgentEntry[],
): AgentEntry[] {
  const q = query.toLowerCase();
  if (!q) return agents;
  return agents.filter(
    (a) =>
      a.slug.toLowerCase().startsWith(q) ||
      a.name.toLowerCase().startsWith(q),
  );
}

/**
 * Create initial mention state.
 */
export function createMentionState(): MentionState {
  return {
    visible: false,
    matches: [],
    selectedIndex: 0,
    query: "",
    atIndex: -1,
  };
}

/**
 * Find the last "@" trigger in the input and compute mention state.
 *
 * Rules:
 * - Looks for the last "@" that is either at position 0 or preceded by a space
 * - The text after "@" must be word chars only (no spaces yet)
 * - If a space follows the query, the mention is considered "closed"
 */
export function computeMentionState(
  input: string,
  agents: AgentEntry[],
): MentionState {
  // Find the last "@" that could be a trigger
  let atIndex = -1;
  for (let i = input.length - 1; i >= 0; i--) {
    if (input[i] === "@") {
      // Valid trigger: start of string or preceded by whitespace
      if (i === 0 || /\s/.test(input[i - 1])) {
        atIndex = i;
        break;
      }
    }
  }

  if (atIndex < 0) {
    return createMentionState();
  }

  const afterAt = input.slice(atIndex + 1);

  // If there's a space after the query, the user has moved on
  if (afterAt.includes(" ")) {
    return createMentionState();
  }

  // Only word characters allowed in the query
  if (afterAt.length > 0 && !/^[a-zA-Z0-9_-]+$/.test(afterAt)) {
    return createMentionState();
  }

  const query = afterAt;
  const matches = filterAgents(query, agents);

  // Don't show if exactly one match equals the query
  if (matches.length === 1 && matches[0].slug === query) {
    return createMentionState();
  }

  if (matches.length === 0) {
    return { visible: false, matches: [], selectedIndex: 0, query, atIndex };
  }

  return {
    visible: true,
    matches,
    selectedIndex: 0,
    query,
    atIndex,
  };
}

/**
 * Create actions that manage mention state.
 */
export function createMentionActions(
  state: MentionState,
  setState: (s: MentionState) => void,
  agents: AgentEntry[],
  currentInput: string,
): MentionActions {
  const accept = (index: number): MentionResult | null => {
    if (!state.visible || state.matches.length === 0) return null;
    const match = state.matches[index];
    if (!match) return null;
    setState(createMentionState());

    // Replace the @query with @slug in the input
    const before = currentInput.slice(0, state.atIndex);
    const after = currentInput.slice(state.atIndex + 1 + state.query.length);
    const text = `${before}@${match.slug} ${after}`;

    return { text: text.trimEnd() + " ", agentSlug: match.slug };
  };

  return {
    onTab: () => {
      if (!state.visible || state.matches.length === 0) return null;

      if (state.matches.length === 1) {
        return accept(0);
      }

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
      setState(createMentionState());
    },

    onNavigate: (direction: number) => {
      if (!state.visible || state.matches.length <= 1) return;
      const len = state.matches.length;
      const next = ((state.selectedIndex + direction) % len + len) % len;
      setState({ ...state, selectedIndex: next });
    },

    update: (input: string) => {
      const newState = computeMentionState(input, agents);
      setState(newState);
    },
  };
}

// ── React hook ──────────────────────────────────────────────────────

export function useMentionAutocomplete(
  agents: AgentEntry[],
  currentInput: string,
): {
  state: MentionState;
  actions: MentionActions;
} {
  const [mState, setMState] = useState<MentionState>(createMentionState());
  const agentsRef = useRef(agents);
  agentsRef.current = agents;
  const inputRef = useRef(currentInput);
  inputRef.current = currentInput;
  const stateRef = useRef(mState);
  stateRef.current = mState;

  // Memoize actions to avoid re-creating on every render
  const actions = React.useMemo<MentionActions>(() => ({
    onTab: () => {
      const s = stateRef.current;
      if (!s.visible || s.matches.length === 0) return null;
      if (s.matches.length === 1) {
        const match = s.matches[0];
        setMState(createMentionState());
        const before = inputRef.current.slice(0, s.atIndex);
        const after = inputRef.current.slice(s.atIndex + 1 + s.query.length);
        return { text: `${before}@${match.slug} ${after}`.trimEnd() + " ", agentSlug: match.slug };
      }
      const next = (s.selectedIndex + 1) % s.matches.length;
      setMState({ ...s, selectedIndex: next });
      return null;
    },
    onShiftTab: () => {
      const s = stateRef.current;
      if (!s.visible || s.matches.length === 0) return null;
      if (s.matches.length === 1) {
        const match = s.matches[0];
        setMState(createMentionState());
        const before = inputRef.current.slice(0, s.atIndex);
        const after = inputRef.current.slice(s.atIndex + 1 + s.query.length);
        return { text: `${before}@${match.slug} ${after}`.trimEnd() + " ", agentSlug: match.slug };
      }
      const next = (s.selectedIndex - 1 + s.matches.length) % s.matches.length;
      setMState({ ...s, selectedIndex: next });
      return null;
    },
    onAccept: () => {
      const s = stateRef.current;
      if (!s.visible || s.matches.length === 0) return null;
      const match = s.matches[s.selectedIndex];
      if (!match) return null;
      setMState(createMentionState());
      const before = inputRef.current.slice(0, s.atIndex);
      const after = inputRef.current.slice(s.atIndex + 1 + s.query.length);
      return { text: `${before}@${match.slug} ${after}`.trimEnd() + " ", agentSlug: match.slug };
    },
    onDismiss: () => setMState(createMentionState()),
    onNavigate: (direction: number) => {
      const s = stateRef.current;
      if (!s.visible || s.matches.length <= 1) return;
      const len = s.matches.length;
      const next = ((s.selectedIndex + direction) % len + len) % len;
      setMState({ ...s, selectedIndex: next });
    },
    update: (input: string) => {
      // Skip computation if no agents registered (common case — avoids flicker)
      if (agentsRef.current.length === 0) {
        if (stateRef.current.visible) setMState(createMentionState());
        return;
      }
      const newState = computeMentionState(input, agentsRef.current);
      // Only update if state actually changed to avoid unnecessary re-renders
      const cur = stateRef.current;
      if (cur.visible !== newState.visible ||
          cur.selectedIndex !== newState.selectedIndex ||
          cur.query !== newState.query ||
          cur.matches.length !== newState.matches.length) {
        setMState(newState);
      }
    },
  }), []); // stable reference — uses refs internally

  return { state: mState, actions };
}

// ── Component ───────────────────────────────────────────────────────

/**
 * Overlay rendering the list of matching agents.
 */
export function MentionAutocomplete({
  state,
  maxVisible = 8,
}: MentionAutocompleteProps): React.JSX.Element | null {
  if (!state.visible || state.matches.length === 0) {
    return null;
  }

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
      {visibleItems.map((agent, i) => {
        const actualIndex = startIdx + i;
        const isSelected = actualIndex === state.selectedIndex;
        const color = getAgentColor(agent.slug);

        return (
          <Box key={agent.slug} gap={1}>
            <Text
              color={isSelected ? "cyan" : undefined}
              bold={isSelected}
              dimColor={!isSelected}
            >
              {isSelected ? ">" : " "}
            </Text>
            <Text color={isSelected ? "cyan" : (color as AgentColor)} bold={isSelected}>
              {`@${agent.slug}`}
            </Text>
            <Text dimColor>{agent.name}</Text>
          </Box>
        );
      })}

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

export default MentionAutocomplete;
