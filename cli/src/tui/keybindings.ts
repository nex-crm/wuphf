/**
 * Key dispatcher for the TUI.
 *
 * In conversation mode (home view), the input is always active and handled
 * by React's TextInput — so this module is only used in sub-views.
 *
 * Sub-views still support vim-style navigation and the original mode model.
 */

import type { TuiState, Dispatch } from "./store.js";

// ── Key parsing ────────────────────────────────────────────────────

export interface ParsedKey {
  name: string;
  ctrl: boolean;
  shift: boolean;
}

/**
 * Parse a raw input string into a structured key descriptor.
 * Handles common ANSI escape sequences and control characters.
 */
export function parseKey(input: string): ParsedKey {
  if (input.length === 1) {
    const code = input.charCodeAt(0);

    // Tab (0x09) -- must check before general Ctrl range
    if (code === 9) {
      return { name: "tab", ctrl: false, shift: false };
    }

    // Enter / Return (0x0D) -- must check before general Ctrl range
    if (code === 13) {
      return { name: "return", ctrl: false, shift: false };
    }

    // Escape (0x1B)
    if (code === 27) {
      return { name: "escape", ctrl: false, shift: false };
    }

    // Backspace (0x7F)
    if (code === 127) {
      return { name: "backspace", ctrl: false, shift: false };
    }

    // Ctrl+a through Ctrl+z (remaining control chars 1-26)
    if (code >= 1 && code <= 26) {
      const letter = String.fromCharCode(code + 96); // a=1, b=2, ...
      return { name: letter, ctrl: true, shift: false };
    }

    // Printable with shift detection for A-Z
    if (code >= 65 && code <= 90) {
      return { name: input, ctrl: false, shift: true };
    }

    return { name: input, ctrl: false, shift: false };
  }

  // ANSI escape sequences
  if (input.startsWith("\x1b[")) {
    const seq = input.slice(2);
    switch (seq) {
      case "A":
        return { name: "up", ctrl: false, shift: false };
      case "B":
        return { name: "down", ctrl: false, shift: false };
      case "C":
        return { name: "right", ctrl: false, shift: false };
      case "D":
        return { name: "left", ctrl: false, shift: false };
      case "H":
        return { name: "home", ctrl: false, shift: false };
      case "F":
        return { name: "end", ctrl: false, shift: false };
      case "3~":
        return { name: "delete", ctrl: false, shift: false };
      default:
        return { name: `[${seq}`, ctrl: false, shift: false };
    }
  }

  // Escape alone (multi-byte check when Ink sends it)
  if (input === "\x1b") {
    return { name: "escape", ctrl: false, shift: false };
  }

  return { name: input, ctrl: false, shift: false };
}

// ── Double-tap timing ──────────────────────────────────────────────

const DOUBLE_TAP_MS = 500;

// ── Available commands for autocomplete ────────────────────────────

const COMMANDS = [
  "record list",
  "record get",
  "record create",
  "record update",
  "record delete",
  "object list",
  "object get",
  "search",
  "insight",
  "context",
  "task list",
  "task create",
  "note create",
  "help",
  "quit",
];

// ── Normal mode handler (sub-views only) ─────────────────────────

function handleNormalMode(
  key: ParsedKey,
  rawInput: string,
  state: TuiState,
  dispatch: Dispatch,
): void {
  // Ctrl combinations
  if (key.ctrl) {
    switch (key.name) {
      case "d": // half-page down
        dispatch({ type: "SCROLL", offset: state.scrollOffset + 15 });
        return;
      case "u": // half-page up
        dispatch({
          type: "SCROLL",
          offset: Math.max(0, state.scrollOffset - 15),
        });
        return;
      case "c": // exit
        process.exit(0);
        return;
    }
    return;
  }

  switch (key.name) {
    case "i":
      dispatch({ type: "SET_MODE", mode: "insert" });
      return;

    case "/":
      dispatch({ type: "SET_MODE", mode: "insert" });
      dispatch({ type: "SET_INPUT", value: "/" });
      return;

    case "?":
      dispatch({ type: "PUSH_VIEW", view: { name: "help" } });
      return;

    case "a":
      dispatch({ type: "PUSH_VIEW", view: { name: "agent-list" } });
      return;

    case "c":
      dispatch({ type: "PUSH_VIEW", view: { name: "chat" } });
      return;

    case "C":
      dispatch({ type: "PUSH_VIEW", view: { name: "calendar" } });
      return;

    case "o":
      dispatch({ type: "PUSH_VIEW", view: { name: "orchestration" } });
      return;

    case "j":
    case "down":
      if (state.pickerItems && state.pickerItems.length > 0) {
        const next = Math.min(
          state.pickerCursor + 1,
          state.pickerItems.length - 1,
        );
        dispatch({ type: "SET_PICKER_CURSOR", cursor: next });
      } else {
        dispatch({ type: "SCROLL", offset: state.scrollOffset + 1 });
      }
      return;

    case "k":
    case "up":
      if (state.pickerItems && state.pickerItems.length > 0) {
        const prev = Math.max(state.pickerCursor - 1, 0);
        dispatch({ type: "SET_PICKER_CURSOR", cursor: prev });
      } else {
        dispatch({
          type: "SCROLL",
          offset: Math.max(0, state.scrollOffset - 1),
        });
      }
      return;

    case "g": {
      const now = Date.now();
      if (
        state.lastKey === "g" &&
        now - state.lastKeyTime < DOUBLE_TAP_MS
      ) {
        dispatch({ type: "SCROLL", offset: 0 });
        dispatch({ type: "SET_LAST_KEY", key: "", time: 0 });
        return;
      }
      dispatch({ type: "SET_LAST_KEY", key: "g", time: now });
      return;
    }

    case "G":
      dispatch({ type: "SCROLL", offset: Infinity });
      return;

    case "q":
      process.exit(0);
      return;

    case "escape":
      dispatch({ type: "POP_VIEW" });
      return;

    case "return":
      // Execute selected picker item
      if (state.pickerItems && state.pickerItems.length > 0) {
        const item = state.pickerItems[state.pickerCursor];
        if (item) {
          dispatch({ type: "SET_MODE", mode: "insert" });
          dispatch({ type: "SET_INPUT", value: item.command });
        }
      }
      return;

    default: {
      // Quick-select 1-9
      const num = parseInt(key.name, 10);
      if (num >= 1 && num <= 9 && state.pickerItems) {
        const idx = num - 1;
        if (idx < state.pickerItems.length) {
          dispatch({ type: "SET_PICKER_CURSOR", cursor: idx });
          const item = state.pickerItems[idx];
          if (item) {
            dispatch({ type: "SET_MODE", mode: "insert" });
            dispatch({ type: "SET_INPUT", value: item.command });
          }
        }
      }
    }
  }
}

// ── Insert mode handler (sub-views only) ─────────────────────────

function handleInsertMode(
  key: ParsedKey,
  _rawInput: string,
  state: TuiState,
  dispatch: Dispatch,
): void {
  switch (key.name) {
    case "escape":
      dispatch({ type: "SET_MODE", mode: "normal" });
      return;

    case "return":
      if (state.inputValue.trim()) {
        dispatch({ type: "PUSH_HISTORY", command: state.inputValue.trim() });
      }
      // The actual command execution is handled by the component layer
      return;

    case "up":
      // Previous history
      if (state.inputHistory.length > 0) {
        const newIdx =
          state.historyIndex === -1
            ? state.inputHistory.length - 1
            : Math.max(0, state.historyIndex - 1);
        dispatch({ type: "SET_HISTORY_INDEX", index: newIdx });
        dispatch({
          type: "SET_INPUT",
          value: state.inputHistory[newIdx] ?? "",
        });
      }
      return;

    case "down":
      // Next history
      if (state.historyIndex >= 0) {
        const newIdx = state.historyIndex + 1;
        if (newIdx >= state.inputHistory.length) {
          dispatch({ type: "SET_HISTORY_INDEX", index: -1 });
          dispatch({ type: "SET_INPUT", value: "" });
        } else {
          dispatch({ type: "SET_HISTORY_INDEX", index: newIdx });
          dispatch({
            type: "SET_INPUT",
            value: state.inputHistory[newIdx] ?? "",
          });
        }
      }
      return;

    case "tab": {
      // Basic autocomplete
      const current = state.inputValue.toLowerCase();
      if (!current) return;
      const match = COMMANDS.find((c) => c.startsWith(current));
      if (match) {
        dispatch({ type: "SET_INPUT", value: match });
      }
      return;
    }

    default:
      // All other keys are handled by the Ink TextInput component
      break;
  }
}

// ── Main entry point ───────────────────────────────────────────────

/**
 * Route a key event to the correct mode handler.
 * Note: in conversation mode (home view) this is NOT called —
 * the app.tsx skips calling handleKey when at home.
 */
export function handleKey(
  rawInput: string,
  state: TuiState,
  dispatch: Dispatch,
): void {
  const key = parseKey(rawInput);

  if (state.mode === "normal") {
    handleNormalMode(key, rawInput, state, dispatch);
  } else {
    handleInsertMode(key, rawInput, state, dispatch);
  }
}
