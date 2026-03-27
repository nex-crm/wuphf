import React from "react";
import { Box, Text } from "ink";

// --- Colors (Bubbletea scheme) ---

const NEX_BLUE = "#2980fb";
const NEX_PURPLE = "#cf72d9";
const MODE_NORMAL_BG = NEX_BLUE;
const MODE_INSERT_BG = "#03a04c";

// --- Types ---

export type Mode = "normal" | "insert";

export interface SessionStats {
  tokensUsed?: number;
  costUsd?: number;
  model?: string;
  startTime?: number;
}

export interface StatusBarProps {
  mode: Mode;
  breadcrumbs: string[];
  scrollPercent?: number;
  hint?: string;
  /** When true, hides the mode badge (conversation view handles its own input) */
  conversationMode?: boolean;
  /** Session stats for token/cost/elapsed display */
  session?: SessionStats;
}

// --- Formatting helpers ---

export function formatTokens(n: number): string {
  if (n < 1000) return String(n);
  if (n < 10_000) return `${(n / 1000).toFixed(1)}k`;
  if (n < 1_000_000) return `${Math.round(n / 1000)}k`;
  return `${(n / 1_000_000).toFixed(1)}M`;
}

export function formatCost(usd: number): string {
  if (usd < 0.001) return "$0.00";
  if (usd < 1) return `$${usd.toFixed(3)}`;
  return `$${usd.toFixed(2)}`;
}

export function formatElapsed(startTime: number, now?: number): string {
  const ms = (now ?? Date.now()) - startTime;
  const totalSecs = Math.max(0, Math.floor(ms / 1000));
  const mins = Math.floor(totalSecs / 60);
  const secs = totalSecs % 60;
  if (mins === 0) return `${secs}s`;
  return `${mins}m${String(secs).padStart(2, "0")}s`;
}

// --- Separator ---

const SEP = " \u2503 "; // ┃

// --- Session stats fragment ---

function SessionStatsFragment({ session }: { session?: SessionStats }): React.JSX.Element | null {
  if (!session) return null;

  return (
    <>
      {session.tokensUsed !== undefined && session.tokensUsed > 0 && (
        <>
          <Text dimColor>{SEP}</Text>
          <Text dimColor>{"tokens "}</Text>
          <Text>{formatTokens(session.tokensUsed)}</Text>
        </>
      )}
      {session.costUsd !== undefined && session.costUsd > 0 && (
        <>
          <Text dimColor>{SEP}</Text>
          <Text>{formatCost(session.costUsd)}</Text>
        </>
      )}
      {session.startTime !== undefined && (
        <>
          <Text dimColor>{SEP}</Text>
          <Text dimColor>{formatElapsed(session.startTime)}</Text>
        </>
      )}
    </>
  );
}

// --- Component ---

export function StatusBar({
  mode,
  breadcrumbs,
  scrollPercent,
  hint,
  conversationMode = false,
  session,
}: StatusBarProps): React.JSX.Element {
  const breadcrumbText = breadcrumbs.join(" > ");

  if (conversationMode) {
    // Simplified bar for conversation mode — brand + tokens + cost on left, hint on right
    return (
      <Box justifyContent="space-between">
        <Box>
          <Text dimColor>{" "}</Text>
          <Text color={NEX_PURPLE} bold>
            {"\u258C"}
          </Text>
          <Text bold>{"wuphf"}</Text>

          <SessionStatsFragment session={session} />

          {scrollPercent !== undefined && (
            <>
              <Text dimColor>{SEP}</Text>
              <Text>{`${scrollPercent}%`}</Text>
            </>
          )}
        </Box>

        <Box>
          <Text dimColor>{hint || "/help for commands  |  Esc=back"}</Text>
          <Text dimColor>{" "}</Text>
        </Box>
      </Box>
    );
  }

  // Sub-view mode — show mode badge with Bubbletea colors
  const modeBg = mode === "normal" ? MODE_NORMAL_BG : MODE_INSERT_BG;
  const modeLabel = mode.toUpperCase();

  return (
    <Box justifyContent="space-between">
      <Box>
        <Text backgroundColor={modeBg} color="black" bold>
          {` ${modeLabel} `}
        </Text>
        <Text>{" "}</Text>
        <Text>{breadcrumbText}</Text>

        {scrollPercent !== undefined && (
          <>
            <Text dimColor>{SEP}</Text>
            <Text>{`${scrollPercent}%`}</Text>
          </>
        )}

        <SessionStatsFragment session={session} />

        {hint && (
          <>
            <Text dimColor>{SEP}</Text>
            <Text dimColor>{hint}</Text>
          </>
        )}
      </Box>

      <Box>
        <Text color={NEX_PURPLE} bold>
          {"\u258C"}
        </Text>
        <Text bold>{"wuphf"}</Text>
        <Text>{" "}</Text>
      </Box>
    </Box>
  );
}

export default StatusBar;
