/**
 * Slack-style channel header bar.
 *
 * Shows channel name (with # prefix for channels, presence dot for DMs),
 * topic line, and action hints. Sits at the top of the main panel.
 */

import React from "react";
import { Box, Text } from "ink";

export interface ChannelHeaderProps {
  name: string;
  type: "channel" | "dm" | "group-dm";
  online?: boolean;
  focused: boolean;
}

export function ChannelHeader({
  name,
  type,
  online,
  focused,
}: ChannelHeaderProps): React.JSX.Element {
  let prefix: React.JSX.Element;
  if (type === "channel") {
    prefix = <Text color={focused ? "cyan" : "gray"}># </Text>;
  } else {
    const dotColor = online ? "green" : "gray";
    const dot = online ? "●" : "○";
    prefix = <Text color={dotColor}>{dot} </Text>;
  }

  return (
    <Box
      paddingX={1}
      justifyContent="space-between"
    >
      <Box gap={1}>
        {focused && <Text color="black" backgroundColor="cyan" bold>{" MESSAGES "}</Text>}
        {prefix}
        <Text bold color={focused ? "cyan" : "white"}>
          {name}
        </Text>
      </Box>
      <Box gap={2}>
        {focused && <Text color="cyan">{"↑↓ scroll"}</Text>}
        <Text color="gray">Ctrl+K search</Text>
        <Text color="gray">Tab focus</Text>
      </Box>
    </Box>
  );
}

export default ChannelHeader;
