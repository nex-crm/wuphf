import React from "react";
import { Box, Text } from "ink";

// --- Types ---

interface CommandGroup {
  title: string;
  commands: string[];
}

// --- Constants ---

const COMMAND_GROUPS: CommandGroup[] = [
  {
    title: "Explore",
    commands: [
      "object list",
      "record list",
      "record get",
      "search",
      "insight list",
    ],
  },
  {
    title: "Write",
    commands: [
      "record create",
      "record upsert",
      "record update",
      "note create",
      "task create",
      "remember",
    ],
  },
  {
    title: "Config",
    commands: [
      "config show",
      "config set",
      "integrate list",
      "integrate connect",
    ],
  },
  {
    title: "AI / Agents",
    commands: [
      "ask",
      "recall",
      "capture",
      "agent list",
      "agent create",
      "agent start",
    ],
  },
];

const KEYBINDINGS: Array<{ key: string; action: string }> = [
  { key: "i", action: "insert" },
  { key: "Esc", action: "back" },
  { key: "?", action: "help" },
  { key: "a", action: "agents" },
  { key: "c", action: "chat" },
  { key: "o", action: "orchestration" },
  { key: "q", action: "quit" },
  { key: "j/k", action: "scroll" },
];

// --- Component ---

export function HelpScreen(): React.JSX.Element {
  return (
    <Box flexDirection="column" paddingX={2} paddingY={1}>
      {/* Keybindings header */}
      <Box marginBottom={1}>
        <Text bold color="cyan">
          {"Keybindings: "}
        </Text>
        {KEYBINDINGS.map((kb, i) => (
          <React.Fragment key={kb.key}>
            <Text bold color="yellow">
              {kb.key}
            </Text>
            <Text dimColor>{"="}</Text>
            <Text>{kb.action}</Text>
            {i < KEYBINDINGS.length - 1 && <Text dimColor>{"  "}</Text>}
          </React.Fragment>
        ))}
      </Box>

      {/* Command groups in 4-column layout */}
      <Box>
        {COMMAND_GROUPS.map((group) => (
          <Box
            key={group.title}
            flexDirection="column"
            marginRight={4}
            minWidth={16}
          >
            <Box marginBottom={1}>
              <Text bold underline color="cyan">
                {group.title}
              </Text>
            </Box>
            {group.commands.map((cmd) => (
              <Text key={cmd} dimColor={false}>
                {"  "}
                {cmd}
              </Text>
            ))}
          </Box>
        ))}
      </Box>
    </Box>
  );
}

export default HelpScreen;
