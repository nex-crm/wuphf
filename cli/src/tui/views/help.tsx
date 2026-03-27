import React from "react";
import { Box, Text } from "ink";
import { HelpScreen } from "../components/help-screen.js";

// --- Types ---

export interface HelpViewProps {
  onBack?: () => void;
}

// --- Component ---

export function HelpView({
  onBack: _onBack,
}: HelpViewProps): React.JSX.Element {
  return (
    <Box flexDirection="column" width="100%">
      {/* Title */}
      <Box paddingX={2} marginBottom={1}>
        <Text bold color="cyan">
          {"WUPHF CLI Help"}
        </Text>
      </Box>

      {/* Help content */}
      <HelpScreen />
    </Box>
  );
}

export default HelpView;
