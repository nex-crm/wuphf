import React, { useState, useEffect } from "react";
import { Text } from "ink";
import { useSpinner } from "./spinner.js";

// ── Types ───────────────────────────────────────────────────────────

export type ToolStatus = "running" | "done" | "error";

export interface ToolIndicatorProps {
  /** Name of the tool being executed */
  name: string;
  /** Current status */
  status: ToolStatus;
  /** Elapsed time in seconds (shown when done) */
  elapsed?: number;
}

// ── Component ───────────────────────────────────────────────────────

export function ToolIndicator({
  name,
  status,
  elapsed,
}: ToolIndicatorProps): React.JSX.Element {
  const frame = useSpinner();

  if (status === "done") {
    const time = elapsed !== undefined ? ` (${elapsed.toFixed(1)}s)` : "";
    return (
      <Text>
        <Text color="green">{"✓"}</Text>
        <Text>{` ${name}`}</Text>
        <Text dimColor>{time}</Text>
      </Text>
    );
  }

  if (status === "error") {
    return (
      <Text>
        <Text color="red">{"✗"}</Text>
        <Text>{` ${name}`}</Text>
      </Text>
    );
  }

  // running
  return (
    <Text>
      <Text color="cyan">{frame}</Text>
      <Text>{` ${name}`}</Text>
    </Text>
  );
}

export default ToolIndicator;
