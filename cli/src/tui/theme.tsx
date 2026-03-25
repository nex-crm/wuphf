/**
 * Theme tokens for Ink components.
 * Maps existing src/lib/tui.ts style semantics to Ink-compatible color names.
 */

import React, { createContext, useContext } from "react";
import type { ReactNode } from "react";

// ── Color palette ──────────────────────────────────────────────────

export const theme = {
  colors: {
    primary: "cyan" as const,
    success: "green" as const,
    error: "red" as const,
    warning: "yellow" as const,
    muted: "gray" as const,
    text: "white" as const,
  },
  modes: {
    normal: { badge: "NORMAL", color: "cyan" as const },
    insert: { badge: "INSERT", color: "green" as const },
  },
  statusBar: {
    bg: "gray" as const,
    fg: "white" as const,
  },
  breadcrumb: {
    separator: " > ",
    color: "gray" as const,
    activeColor: "cyan" as const,
  },
} as const;

export type Theme = typeof theme;

// ── React context ──────────────────────────────────────────────────

const ThemeContext = createContext<Theme>(theme);

export function ThemeProvider({
  value,
  children,
}: {
  value?: Theme;
  children: ReactNode;
}) {
  return (
    <ThemeContext.Provider value={value ?? theme}>
      {children}
    </ThemeContext.Provider>
  );
}

export function useTheme(): Theme {
  return useContext(ThemeContext);
}
