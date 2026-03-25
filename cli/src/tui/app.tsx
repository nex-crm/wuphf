/**
 * Root Ink application.
 * Wraps everything in theme context + TuiContext, handles global key events,
 * renders the router and a single status bar.
 *
 * Ctrl+C behavior:
 *  - During loading: cancels current operation, shows "^C (interrupted)"
 *  - When idle: first press shows "Press Ctrl+C again to exit",
 *    second press within 1s exits the process
 */

import React, { useState, useEffect, useCallback, useMemo, useRef } from "react";
import { Box, Text, useInput, useApp } from "ink";
import { createStore } from "./store.js";
import type { TuiState } from "./store.js";
import { handleKey } from "./keybindings.js";
import { ThemeProvider } from "./theme.js";
import { Router } from "./router.js";
import { StatusBar as ComponentStatusBar } from "./components/status-bar.js";
import { TuiContext } from "./tui-context.js";
import type { TuiContextValue } from "./tui-context.js";

// Register all views with the router (side-effect import)
import "./register-views.js";

// Re-export context hook for convenience
export { useTuiState } from "./tui-context.js";
export type { TuiContextValue } from "./tui-context.js";

// ── Constants ─────────────────────────────────────────────────────────

const CTRL_C_WINDOW_MS = 1000;
const INTERRUPTED_DISPLAY_MS = 2000;
const EXIT_HINT_DISPLAY_MS = 2000;

// ── Status bar adapter ─────────────────────────────────────────────

function AppStatusBar({ state }: { state: TuiState }) {
  const breadcrumbs = state.viewStack.map((v) => v.name);
  const currentViewName = state.viewStack[state.viewStack.length - 1]?.name ?? "home";
  const inputViews = new Set(["home", "chat", "ask-chat"]);
  const isInputView = inputViews.has(currentViewName);

  const hint = state.loading
    ? state.loadingHint || "loading..."
    : isInputView
      ? currentViewName === "home"
        ? "Tab=complete  /help=commands  Ctrl+C=exit"
        : "Esc=back  |  type to send"
      : "Esc=back  |  j/k=nav  Enter=select";

  return (
    <ComponentStatusBar
      mode={state.mode}
      breadcrumbs={breadcrumbs.length > 0 ? breadcrumbs : ["home"]}
      hint={hint}
      conversationMode={isInputView}
      session={state.session ?? undefined}
    />
  );
}

// ── App component ──────────────────────────────────────────────────

export function App() {
  const store = useMemo(() => createStore(), []);
  const [state, setState] = useState<TuiState>(store.getState);
  const [interrupted, setInterrupted] = useState(false);
  const [exitHint, setExitHint] = useState(false);
  const { exit } = useApp();

  // Track last Ctrl+C press time for double-press detection
  const lastCtrlCTime = useRef(0);

  // Subscribe to store changes
  useEffect(() => {
    const unsub = store.subscribe(() => {
      setState(store.getState());
    });
    return unsub;
  }, [store]);

  // Override process.exit for clean shutdown within Ink
  const originalExit = useMemo(() => process.exit, []);
  useEffect(() => {
    const handler = ((_code?: number) => {
      exit();
      // Ensure the Node process actually terminates after Ink unmounts
      setTimeout(() => originalExit(_code ?? 0), 150);
    }) as typeof process.exit;
    process.exit = handler;
    return () => {
      process.exit = originalExit;
    };
  }, [exit, originalExit]);

  // Cancel function: aborts current loading operation
  const cancel = useCallback(() => {
    const currentState = store.getState();
    if (currentState.loading) {
      store.dispatch({ type: "SET_LOADING", loading: false, hint: "" });
      setInterrupted(true);
      setTimeout(() => setInterrupted(false), INTERRUPTED_DISPLAY_MS);
    }
  }, [store]);

  // Clear interrupted when new loading starts
  useEffect(() => {
    if (state.loading) {
      setInterrupted(false);
      setExitHint(false);
    }
  }, [state.loading]);

  // Ctrl+C handler
  const handleCtrlC = useCallback(() => {
    const now = Date.now();
    const currentState = store.getState();

    // During loading: cancel the operation
    if (currentState.loading) {
      cancel();
      lastCtrlCTime.current = now;
      return;
    }

    // When idle: double-press to exit
    if (now - lastCtrlCTime.current <= CTRL_C_WINDOW_MS) {
      // Second press within window → exit
      process.exit(0);
      return;
    }

    // First idle press → show exit hint
    lastCtrlCTime.current = now;
    setExitHint(true);
    setTimeout(() => setExitHint(false), EXIT_HINT_DISPLAY_MS);
  }, [store, cancel]);

  // Global key handler
  const onInput = useCallback(
    (
      input: string,
      key: { escape?: boolean; ctrl?: boolean; shift?: boolean; tab?: boolean; upArrow?: boolean; downArrow?: boolean; return?: boolean; backspace?: boolean; delete?: boolean; name?: string },
    ) => {
      const currentView =
        state.viewStack[state.viewStack.length - 1]?.name ?? "home";

      // Views that have their own text input — pass keys through
      // so users can type without vim mode. Only intercept Escape (back)
      // and Ctrl+C (cancel/exit).
      const inputViews = new Set(["home", "chat", "ask-chat"]);
      const isInputView = inputViews.has(currentView);

      if (isInputView) {
        if (key.ctrl && key.name === "c") {
          handleCtrlC();
          return;
        }
        if (key.escape && currentView !== "home") {
          store.dispatch({ type: "POP_VIEW" });
          return;
        }
        // Home stream: only intercept Tab (autocomplete) and arrows (autocomplete nav)
        if (currentView === "home") {
          const g = globalThis as Record<string, unknown>;

          if (key.tab && !key.shift) {
            const tabFn = g.__nexHomeTabComplete as ((d: number) => boolean) | undefined;
            if (tabFn) tabFn(1);
            return;
          }
          if (key.upArrow || key.downArrow) {
            const acNav = g.__nexHomeAutocompleteNav as ((d: number) => boolean) | undefined;
            if (acNav && acNav(key.upArrow ? -1 : 1)) return;
          }
        }
        // Everything else → TextInput
        return;
      }

      // In non-input views, use the full keybinding handler
      let raw = input;
      if (key.escape) raw = "\x1b";
      if (key.ctrl && key.name) {
        if (key.name === "c") {
          handleCtrlC();
          return;
        }
        const code = key.name.charCodeAt(0) - 96;
        if (code > 0 && code <= 26) raw = String.fromCharCode(code);
      }
      if (
        key.name === "up" ||
        key.name === "down" ||
        key.name === "left" ||
        key.name === "right"
      ) {
        const arrowMap: Record<string, string> = {
          up: "\x1b[A",
          down: "\x1b[B",
          right: "\x1b[C",
          left: "\x1b[D",
        };
        raw = arrowMap[key.name] ?? raw;
      }
      if (key.name === "return") raw = "\r";
      if (key.name === "tab") raw = "\t";
      if (key.name === "backspace") raw = "\x7f";

      handleKey(raw, store.getState(), store.dispatch);
    },
    [store, state.viewStack, handleCtrlC],
  );

  useInput(onInput);

  const tuiContextValue = useMemo<TuiContextValue>(
    () => ({ state, dispatch: store.dispatch, store, cancel }),
    [state, store, cancel],
  );

  return (
    <ThemeProvider>
      <TuiContext.Provider value={tuiContextValue as TuiContextValue}>
        <Box flexDirection="column" minHeight={10}>
          {/* Main viewport */}
          <Box flexDirection="column" flexGrow={1}>
            <Router viewStack={state.viewStack} dispatch={store.dispatch} />
          </Box>

          {/* Interrupted indicator */}
          {interrupted && (
            <Box paddingX={2}>
              <Text dimColor>{"^C"}</Text>
              <Text color="red">{" (interrupted)"}</Text>
            </Box>
          )}

          {/* Exit hint */}
          {exitHint && !interrupted && (
            <Box paddingX={2}>
              <Text dimColor>{"Press Ctrl+C again to exit"}</Text>
            </Box>
          )}

          {/* Status bar */}
          <AppStatusBar state={state} />
        </Box>
      </TuiContext.Provider>
    </ThemeProvider>
  );
}
