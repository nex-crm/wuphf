import React from "react";
import { Box, Text } from "ink";

// --- Types ---

export type AgentStatus = "idle" | "running" | "error" | "stopped";

export interface AgentCardProps {
  name: string;
  status: AgentStatus;
  expertise?: string;
  lastHeartbeat?: number;
  nextHeartbeat?: number;
}

// --- Helpers ---

const STATUS_CONFIG: Record<AgentStatus, { dot: string; color: string }> = {
  idle: { dot: "\u25CB", color: "gray" },
  running: { dot: "\u25CF", color: "green" },
  error: { dot: "\u25CF", color: "red" },
  stopped: { dot: "\u25CF", color: "yellow" },
};

function timeAgo(ts: number): string {
  const diff = Date.now() - ts;
  const secs = Math.floor(diff / 1000);
  if (secs < 60) return `${secs}s ago`;
  const mins = Math.floor(secs / 60);
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.floor(mins / 60);
  return `${hrs}h ago`;
}

function timeUntil(ts: number): string {
  const diff = ts - Date.now();
  if (diff <= 0) return "now";
  const secs = Math.floor(diff / 1000);
  if (secs < 60) return `in ${secs}s`;
  const mins = Math.floor(secs / 60);
  if (mins < 60) return `in ${mins}m`;
  const hrs = Math.floor(mins / 60);
  return `in ${hrs}h`;
}

// --- Component ---

export function AgentCard({
  name,
  status,
  expertise,
  lastHeartbeat,
  nextHeartbeat,
}: AgentCardProps): React.JSX.Element {
  const cfg = STATUS_CONFIG[status];

  return (
    <Box
      flexDirection="column"
      borderStyle="round"
      borderColor={cfg.color}
      paddingX={1}
      minWidth={28}
    >
      {/* Header: name + status */}
      <Box>
        <Text color={cfg.color}>{cfg.dot}</Text>
        <Text>{" "}</Text>
        <Text bold>{name}</Text>
        <Text>{" "}</Text>
        <Text dimColor>{`(${status})`}</Text>
      </Box>

      {/* Expertise tags */}
      {expertise && (
        <Box marginTop={0}>
          <Text dimColor>{"skill: "}</Text>
          <Text color="cyan">{expertise}</Text>
        </Box>
      )}

      {/* Heartbeat info */}
      {lastHeartbeat !== undefined && (
        <Box>
          <Text dimColor>{"last: "}</Text>
          <Text>{timeAgo(lastHeartbeat)}</Text>
        </Box>
      )}
      {nextHeartbeat !== undefined && (
        <Box>
          <Text dimColor>{"next: "}</Text>
          <Text>{timeUntil(nextHeartbeat)}</Text>
        </Box>
      )}
    </Box>
  );
}

export default AgentCard;
