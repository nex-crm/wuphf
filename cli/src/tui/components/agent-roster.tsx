/**
 * Discord-style agent roster panel.
 * Shows all registered agents with live status indicators.
 */

import React, { useState, useEffect, useRef } from "react";
import { Box, Text } from "ink";
import { useSpinner } from "./spinner.js";
import { getAgentService } from "../services/agent-service.js";
import type { AgentPhase } from "../../agent/types.js";

// ── Status indicators ───────────────────────────────────────────

const ACTIVE_PHASES: Set<AgentPhase> = new Set(["build_context", "stream_llm", "execute_tool"]);

/** Animated spinner — only mounted when agent is active. */
function ActiveIcon(): React.JSX.Element {
  const spinner = useSpinner(80);
  return <Text color="green">{spinner}</Text>;
}

/** Static icon — no hooks, no timers, no re-renders. */
function StaticIcon({ phase }: { phase: AgentPhase }): React.JSX.Element {
  if (phase === "error") return <Text color="red">{"●"}</Text>;
  if (phase === "done") return <Text color="green">{"●"}</Text>;
  return <Text color="gray">{"○"}</Text>;
}

// ── Phase label ─────────────────────────────────────────────────

function phaseLabel(phase: AgentPhase): string {
  switch (phase) {
    case "build_context": return "preparing";
    case "stream_llm":    return "thinking";
    case "execute_tool":  return "running tool";
    case "done":          return "done";
    case "error":         return "error";
    default:              return "";
  }
}

// ── Single agent row ────────────────────────────────────────────

interface AgentRowProps {
  name: string;
  phase: AgentPhase;
  error?: string;
}

const AgentRow = React.memo(function AgentRow({ name, phase, error }: AgentRowProps): React.JSX.Element {
  const isActive = ACTIVE_PHASES.has(phase);

  return (
    <Box gap={1}>
      {isActive ? <ActiveIcon /> : <StaticIcon phase={phase} />}
      <Text color={isActive ? "white" : phase === "error" ? "red" : "gray"} bold={isActive}>
        {name}
      </Text>
      {isActive && (
        <Text dimColor>{phaseLabel(phase)}</Text>
      )}
      {phase === "error" && error && (
        <Text color="red" dimColor>{error.slice(0, 15)}</Text>
      )}
    </Box>
  );
});

// ── Roster panel ────────────────────────────────────────────────

interface RosterEntry {
  slug: string;
  name: string;
  phase: AgentPhase;
  error?: string;
}

export function AgentRoster(): React.JSX.Element {
  const [agents, setAgents] = useState<RosterEntry[]>([]);
  const prevRef = useRef<string>("");

  useEffect(() => {
    const agentService = getAgentService();

    const refresh = () => {
      const list = agentService.list().map(a => ({
        slug: a.config.slug,
        name: a.config.name,
        phase: a.state.phase,
        error: a.state.error,
      }));
      // Only update state if data actually changed (avoids unnecessary re-renders)
      const key = list.map(a => `${a.slug}:${a.phase}:${a.error ?? ""}`).join("|");
      if (key !== prevRef.current) {
        prevRef.current = key;
        setAgents(list);
      }
    };

    refresh();
    const unsub = agentService.subscribe(refresh);
    return unsub;
  }, []);

  return (
    <Box
      flexDirection="column"
      borderStyle="single"
      borderColor="gray"
      paddingX={1}
      width={22}
    >
      <Text color="gray" bold>{"AGENTS"}</Text>
      <Box marginTop={1} flexDirection="column" gap={0}>
        {agents.length === 0 ? (
          <Text dimColor>{"none yet"}</Text>
        ) : (
          agents.map(a => (
            <AgentRow key={a.slug} name={a.name} phase={a.phase} error={a.error} />
          ))
        )}
      </Box>
    </Box>
  );
}

export default AgentRoster;
