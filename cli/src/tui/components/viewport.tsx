import React, { useMemo } from "react";
import { Box, Text } from "ink";

// --- Types ---

export interface ViewportProps {
  content: string;
  scrollOffset: number;
  onScroll: (offset: number) => void;
  height?: number;
}

// --- Component ---

export function Viewport({
  content,
  scrollOffset,
  onScroll: _onScroll,
  height,
}: ViewportProps): React.JSX.Element {
  const effectiveHeight = height ?? 20;

  const { visibleLines, scrollPercent, totalLines } = useMemo(() => {
    const allLines = content.split("\n");
    const total = allLines.length;
    const clampedOffset = Math.max(
      0,
      Math.min(scrollOffset, Math.max(0, total - effectiveHeight)),
    );
    const slice = allLines.slice(clampedOffset, clampedOffset + effectiveHeight);

    const pct =
      total <= effectiveHeight
        ? 100
        : Math.round((clampedOffset / (total - effectiveHeight)) * 100);

    return {
      visibleLines: slice,
      scrollPercent: pct,
      totalLines: total,
    };
  }, [content, scrollOffset, effectiveHeight]);

  return (
    <Box flexDirection="column">
      <Box flexDirection="column" height={effectiveHeight}>
        {visibleLines.map((line, i) => (
          <Text key={i} wrap="truncate">
            {line}
          </Text>
        ))}
      </Box>
      {totalLines > effectiveHeight && (
        <Box justifyContent="flex-end">
          <Text dimColor>{`${scrollPercent}%`}</Text>
        </Box>
      )}
    </Box>
  );
}

export default Viewport;
