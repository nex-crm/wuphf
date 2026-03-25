/**
 * Insights dashboard view.
 * Displays insights from the context graph with priority badges,
 * categories, and linked record IDs.
 */

import React from 'react';
import { Box, Text } from 'ink';

// ── Types ──

export interface Insight {
  id: string;
  title: string;
  body: string;
  priority: 'critical' | 'high' | 'medium' | 'low';
  category: string;
  recordIds?: string[];
  timestamp: string;
}

export interface InsightsViewProps {
  insights: Insight[];
}

// ── Helpers ──

function priorityBadge(priority: Insight['priority']): React.JSX.Element {
  switch (priority) {
    case 'critical':
      return <Text color="red" bold>[CRIT]</Text>;
    case 'high':
      return <Text color="red">[HIGH]</Text>;
    case 'medium':
      return <Text color="yellow">[MED]</Text>;
    case 'low':
      return <Text dimColor>[LOW]</Text>;
  }
}

function formatTimestamp(ts: string): string {
  try {
    const d = new Date(ts);
    if (isNaN(d.getTime())) return ts;
    const now = Date.now();
    const diffMs = now - d.getTime();
    const diffMin = Math.floor(diffMs / 60_000);
    if (diffMin < 1) return 'just now';
    if (diffMin < 60) return `${diffMin}m ago`;
    const diffHrs = Math.floor(diffMin / 60);
    if (diffHrs < 24) return `${diffHrs}h ago`;
    const diffDays = Math.floor(diffHrs / 24);
    return `${diffDays}d ago`;
  } catch {
    return ts;
  }
}

// ── Sub-components ──

function InsightCard({ insight }: { insight: Insight }): React.JSX.Element {
  return (
    <Box flexDirection="column" marginBottom={1}>
      {/* Header line: priority + category + title */}
      <Box gap={1}>
        {priorityBadge(insight.priority)}
        <Text color="magenta">[{insight.category}]</Text>
        <Text bold color="blue">{insight.title}</Text>
      </Box>

      {/* Body — indented */}
      {insight.body && (
        <Box paddingLeft={2}>
          <Text>{insight.body}</Text>
        </Box>
      )}

      {/* Record IDs — muted */}
      {insight.recordIds && insight.recordIds.length > 0 && (
        <Box paddingLeft={2}>
          <Text dimColor>Records: {insight.recordIds.join(', ')}</Text>
        </Box>
      )}

      {/* Timestamp — muted */}
      <Box paddingLeft={2}>
        <Text dimColor>{formatTimestamp(insight.timestamp)}</Text>
      </Box>
    </Box>
  );
}

// ── Main view ──

export function InsightsView({ insights }: InsightsViewProps): React.JSX.Element {
  return (
    <Box
      flexDirection="column"
      borderStyle="round"
      borderColor="cyan"
      paddingX={1}
      paddingY={0}
    >
      <Text bold color="cyan">
        Insights
      </Text>
      <Box height={1} />

      {insights.length === 0 ? (
        <Text color="gray">  No insights found. Try expanding the time window.</Text>
      ) : (
        <Box flexDirection="column">
          <Text dimColor>{insights.length} insight{insights.length !== 1 ? 's' : ''}</Text>
          <Box height={1} />
          {insights.map((insight, i) => (
            <InsightCard key={insight.id ?? i} insight={insight} />
          ))}
        </Box>
      )}

      <Text color="gray">
        [Esc=back  /insights --last 24h]
      </Text>
    </Box>
  );
}
