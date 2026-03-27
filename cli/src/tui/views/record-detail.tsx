import React, { useState } from "react";
import { Box, Text } from "ink";
import { Viewport } from "../components/viewport.js";

// --- Types ---

export interface RecordDetailViewProps {
  objectType: string;
  recordId: string;
  recordLabel: string;
  attributes: Record<string, string>;
  onBack?: () => void;
}

// --- Helpers ---

function formatAttributes(attrs: Record<string, string>): string {
  const maxKeyLen = Math.max(
    ...Object.keys(attrs).map((k) => k.length),
    0,
  );

  return Object.entries(attrs)
    .map(([key, val]) => {
      const padded = key.padEnd(maxKeyLen);
      return `  ${padded}  ${val}`;
    })
    .join("\n");
}

// --- Component ---

export function RecordDetailView({
  objectType,
  recordId,
  recordLabel,
  attributes,
  onBack: _onBack,
}: RecordDetailViewProps): React.JSX.Element {
  const [scrollOffset, setScrollOffset] = useState(0);
  const content = formatAttributes(attributes);

  return (
    <Box flexDirection="column" width="100%">
      {/* Header */}
      <Box paddingX={2} marginBottom={1} flexDirection="column">
        <Box>
          <Text bold color="cyan">
            {recordLabel}
          </Text>
        </Box>
        <Box>
          <Text dimColor>{`${objectType} \u2502 ${recordId}`}</Text>
        </Box>
      </Box>

      {/* Attributes */}
      <Box paddingX={1}>
        <Viewport
          content={content}
          scrollOffset={scrollOffset}
          onScroll={setScrollOffset}
        />
      </Box>

      {/* Timeline hint */}
      <Box paddingX={2} marginTop={1}>
        <Text dimColor>
          {"Tip: run "}
        </Text>
        <Text color="cyan">{`wuphf record timeline ${recordId}`}</Text>
        <Text dimColor>{" to see activity"}</Text>
      </Box>
    </Box>
  );
}

export default RecordDetailView;
