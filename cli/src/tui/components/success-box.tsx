import React from "react";
import { Box, Text } from "ink";

// --- Colors ---

const SYS_SUCCESS = "#03a04c";

// --- Types ---

export interface SuccessBoxProps {
  message: string;
  detail?: string;
}

// --- Component ---

export function SuccessBox({ message, detail }: SuccessBoxProps): React.JSX.Element {
  return (
    <Box
      flexDirection="column"
      borderStyle="round"
      borderColor={SYS_SUCCESS}
      paddingX={1}
    >
      {/* Success header + message */}
      <Box>
        <Text color={SYS_SUCCESS} bold>{"✔ "}</Text>
        <Text color={SYS_SUCCESS}>{message}</Text>
      </Box>

      {/* Optional detail line */}
      {detail && (
        <Box marginTop={0}>
          <Text dimColor>{"  "}{detail}</Text>
        </Box>
      )}
    </Box>
  );
}

export default SuccessBox;
