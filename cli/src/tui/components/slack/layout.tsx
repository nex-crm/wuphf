/**
 * SlackLayout — root 3-panel responsive layout.
 *
 * Arranges sidebar + main panel + optional thread panel side by side.
 * Responsive breakpoints:
 *   < 60 cols:   main only (no sidebar)
 *   60–79 cols:  narrow sidebar (20) + main
 *   80–119 cols: sidebar (24) + main; thread replaces main if open
 *   ≥ 120 cols:  sidebar (28) + main + thread (45)
 */

import React from "react";
import type { ReactNode } from "react";
import { Box, Text } from "ink";

// ── Layout calculation ────────────────────────────────────────────

export interface LayoutMetrics {
  sidebarWidth: number;
  showSidebar: boolean;
  mainWidth: number;
  threadWidth: number;
  showThread: boolean;
  /** When true, thread replaces main (narrow terminal + thread open) */
  threadReplacesMain: boolean;
}

export function computeLayout(cols: number, threadOpen: boolean): LayoutMetrics {
  const SIDEBAR_WIDE = 28;
  const SIDEBAR_NORMAL = 24;
  const SIDEBAR_NARROW = 20;
  const THREAD_WIDTH = 45;
  const BORDER = 1; // right border on sidebar

  if (cols < 60) {
    return {
      sidebarWidth: 0,
      showSidebar: false,
      mainWidth: cols,
      threadWidth: 0,
      showThread: false,
      threadReplacesMain: false,
    };
  }

  if (cols < 80) {
    if (threadOpen) {
      // Thread replaces main at narrow widths
      return {
        sidebarWidth: SIDEBAR_NARROW,
        showSidebar: true,
        mainWidth: 0,
        threadWidth: cols - SIDEBAR_NARROW - BORDER,
        showThread: true,
        threadReplacesMain: true,
      };
    }
    return {
      sidebarWidth: SIDEBAR_NARROW,
      showSidebar: true,
      mainWidth: cols - SIDEBAR_NARROW - BORDER,
      threadWidth: 0,
      showThread: false,
      threadReplacesMain: false,
    };
  }

  if (cols < 120) {
    if (threadOpen) {
      // Thread replaces main at medium widths
      return {
        sidebarWidth: SIDEBAR_NORMAL,
        showSidebar: true,
        mainWidth: 0,
        threadWidth: cols - SIDEBAR_NORMAL - BORDER,
        showThread: true,
        threadReplacesMain: true,
      };
    }
    return {
      sidebarWidth: SIDEBAR_NORMAL,
      showSidebar: true,
      mainWidth: cols - SIDEBAR_NORMAL - BORDER,
      threadWidth: 0,
      showThread: false,
      threadReplacesMain: false,
    };
  }

  // Wide: full 3-panel layout
  if (threadOpen) {
    const mainW = cols - SIDEBAR_WIDE - THREAD_WIDTH - BORDER - 1;
    return {
      sidebarWidth: SIDEBAR_WIDE,
      showSidebar: true,
      mainWidth: Math.max(mainW, 30),
      threadWidth: THREAD_WIDTH,
      showThread: true,
      threadReplacesMain: false,
    };
  }

  return {
    sidebarWidth: SIDEBAR_WIDE,
    showSidebar: true,
    mainWidth: cols - SIDEBAR_WIDE - BORDER,
    threadWidth: 0,
    showThread: false,
    threadReplacesMain: false,
  };
}

// ── Component ─────────────────────────────────────────────────────

export type FocusSection = "sidebar" | "messages" | "compose" | "thread";

export interface SlackLayoutProps {
  cols: number;
  rows: number;
  threadOpen: boolean;
  focusSection: FocusSection;
  sidebar: ReactNode;
  main: ReactNode;
  thread?: ReactNode;
  /** Overlay content (quick switcher) — rendered on top */
  overlay?: ReactNode;
}

/**
 * FocusBadge — small colored label showing which section has focus.
 * Renders at the top of each panel, only visible when that panel is focused.
 */
function FocusBadge({ label, visible }: { label: string; visible: boolean }): React.JSX.Element | null {
  if (!visible) return null;
  return (
    <Box>
      <Text color="black" backgroundColor="cyan" bold>{` ${label} `}</Text>
    </Box>
  );
}

export function SlackLayout({
  cols,
  rows,
  threadOpen,
  focusSection,
  sidebar,
  main,
  thread,
  overlay,
}: SlackLayoutProps): React.JSX.Element {
  const layout = computeLayout(cols, threadOpen);

  return (
    <Box flexDirection="column" width={cols} height={rows}>
      <Box flexDirection="row" flexGrow={1}>
        {/* Sidebar */}
        {layout.showSidebar && (
          <Box
            width={layout.sidebarWidth}
            flexDirection="column"
            flexShrink={0}
            borderStyle="single"
            borderLeft={false}
            borderTop={false}
            borderBottom={false}
            borderRight={true}
            borderColor={focusSection === "sidebar" ? "cyan" : "gray"}
          >
            <FocusBadge label="SIDEBAR" visible={focusSection === "sidebar"} />
            {sidebar}
          </Box>
        )}

        {/* Main panel */}
        {layout.mainWidth > 0 && !layout.threadReplacesMain && (
          <Box width={layout.mainWidth} flexDirection="column" flexGrow={1}>
            {main}
          </Box>
        )}

        {/* Thread panel */}
        {layout.showThread && thread && (
          <Box
            width={layout.threadWidth}
            flexDirection="column"
            flexShrink={0}
            borderStyle="single"
            borderRight={false}
            borderTop={false}
            borderBottom={false}
            borderLeft={true}
            borderColor={focusSection === "thread" ? "cyan" : "gray"}
          >
            <FocusBadge label="THREAD" visible={focusSection === "thread"} />
            {thread}
          </Box>
        )}
      </Box>

      {/* Overlay (quick switcher, modals) */}
      {overlay}
    </Box>
  );
}

export default SlackLayout;
