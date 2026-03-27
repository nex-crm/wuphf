import React from "react";
import { Box, Text } from "ink";

// --- Colors ---

const SYS_ERROR = "#e23428";

// --- Error-to-suggestion mapping ---

export type ErrorCategory = "auth" | "rate-limit" | "network" | "server" | "unknown";

const SUGGESTIONS: Record<ErrorCategory, string[]> = {
  auth: [
    "Run 'wuphf init' to configure your API key",
    "Or set WUPHF_API_KEY in your environment",
  ],
  "rate-limit": [
    "Wait a moment and retry the request",
    "Consider batching operations to reduce API calls",
  ],
  network: [
    "Check your internet connection",
    "Verify the API endpoint is reachable",
    "Try again in a few seconds",
  ],
  server: [
    "The server encountered an error — retry shortly",
    "If the issue persists, check https://status.nex.dev",
  ],
  unknown: [
    "Run with --verbose for more details",
    "Report the issue at https://github.com/najmuzzaman-mohammad/wuphf/issues",
  ],
};

// --- Helpers ---

export function categorizeError(error: Error): ErrorCategory {
  const name = error.name?.toLowerCase() ?? "";
  const msg = error.message?.toLowerCase() ?? "";

  if (name.includes("auth") || msg.includes("api key") || msg.includes("unauthorized")) {
    return "auth";
  }
  if (name.includes("ratelimit") || msg.includes("rate limit") || msg.includes("rate limited")) {
    return "rate-limit";
  }
  if (
    msg.includes("fetch failed") ||
    msg.includes("econnrefused") ||
    msg.includes("enotfound") ||
    msg.includes("network") ||
    msg.includes("timeout")
  ) {
    return "network";
  }
  if (name.includes("server") || msg.includes("api error")) {
    return "server";
  }
  return "unknown";
}

export function getSuggestions(category: ErrorCategory): string[] {
  return SUGGESTIONS[category];
}

// --- Types ---

export interface ErrorBoxProps {
  message: string;
  category?: ErrorCategory;
  suggestions?: string[];
}

// --- Component ---

export function ErrorBox({
  message,
  category = "unknown",
  suggestions,
}: ErrorBoxProps): React.JSX.Element {
  const tips = suggestions ?? SUGGESTIONS[category];

  return (
    <Box
      flexDirection="column"
      borderStyle="round"
      borderColor={SYS_ERROR}
      paddingX={1}
    >
      {/* Error header */}
      <Box>
        <Text color={SYS_ERROR} bold>{"✖ Error"}</Text>
      </Box>

      {/* Error message */}
      <Box marginTop={0}>
        <Text color={SYS_ERROR}>{message}</Text>
      </Box>

      {/* Suggestions */}
      {tips.length > 0 && (
        <Box flexDirection="column" marginTop={1}>
          <Text dimColor bold>{"Suggestions:"}</Text>
          {tips.map((tip) => (
            <Box key={tip}>
              <Text dimColor>{"  • "}</Text>
              <Text>{tip}</Text>
            </Box>
          ))}
        </Box>
      )}
    </Box>
  );
}

export default ErrorBox;
