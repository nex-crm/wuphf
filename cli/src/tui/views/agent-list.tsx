import React from "react";
import { Box, Text } from "ink";
import { AgentCard } from "../components/agent-card.js";
import type { AgentCardProps } from "../components/agent-card.js";

// --- Types ---

export interface AgentListViewProps {
  agents: AgentCardProps[];
  onBack?: () => void;
}

// --- Component ---

export function AgentListView({
  agents,
  onBack: _onBack,
}: AgentListViewProps): React.JSX.Element {
  const running = agents.filter((a) => a.status === "running").length;
  const total = agents.length;

  return (
    <Box flexDirection="column" width="100%">
      {/* Header */}
      <Box paddingX={2} marginBottom={1}>
        <Text bold color="cyan">
          {"Agents"}
        </Text>
        <Text dimColor>{` (${running}/${total} running)`}</Text>
      </Box>

      {/* Agent cards grid */}
      <Box flexWrap="wrap" paddingX={1} gap={1}>
        {agents.map((agent) => (
          <AgentCard
            key={agent.name}
            name={agent.name}
            status={agent.status}
            expertise={agent.expertise}
            lastHeartbeat={agent.lastHeartbeat}
            nextHeartbeat={agent.nextHeartbeat}
          />
        ))}
      </Box>

      {agents.length === 0 && (
        <Box paddingX={2}>
          <Text dimColor>{"No agents configured. Use "}</Text>
          <Text color="cyan">{"agent create <slug> --template <name>"}</Text>
          <Text dimColor>{" to add one."}</Text>
        </Box>
      )}
    </Box>
  );
}

export default AgentListView;
