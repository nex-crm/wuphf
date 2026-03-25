import React from "react";
import { Box, Text } from "ink";

// --- Types ---

export type TimelineEventType =
  | "created"
  | "updated"
  | "deleted"
  | "note"
  | "task"
  | "relationship";

export interface TimelineEvent {
  id: string;
  type: TimelineEventType;
  summary: string;
  actor?: string;
  timestamp: string; // ISO or display string
}

export interface TimelineViewProps {
  recordLabel: string;
  recordId: string;
  events: TimelineEvent[];
  onBack?: () => void;
}

// --- Icon mapping ---

const EVENT_ICONS: Record<TimelineEventType, { icon: string; color: string }> =
  {
    created: { icon: "●", color: "green" },
    updated: { icon: "◆", color: "blue" },
    deleted: { icon: "✕", color: "red" },
    note: { icon: "✎", color: "#4d97ff" },
    task: { icon: "☐", color: "cyan" },
    relationship: { icon: "⇄", color: "#cf72d9" },
  };

// --- Component ---

export function TimelineView({
  recordLabel,
  recordId,
  events,
  onBack: _onBack,
}: TimelineViewProps): React.JSX.Element {
  return (
    <Box flexDirection="column" width="100%">
      {/* Header */}
      <Box paddingX={2} marginBottom={1} flexDirection="column">
        <Box>
          <Text bold color="cyan">
            {"Timeline"}
          </Text>
        </Box>
        <Box>
          <Text dimColor>{`${recordLabel} │ ${recordId}`}</Text>
        </Box>
      </Box>

      {/* Events */}
      {events.length === 0 ? (
        <Box paddingX={2}>
          <Text dimColor>{"No timeline events."}</Text>
        </Box>
      ) : (
        <Box flexDirection="column" paddingX={2}>
          {events.map((event, idx) => {
            const { icon, color } = EVENT_ICONS[event.type] ?? {
              icon: "?",
              color: "white",
            };
            const isLast = idx === events.length - 1;

            return (
              <Box key={event.id} flexDirection="column">
                {/* Event row */}
                <Box>
                  <Text color={color}>{icon}</Text>
                  <Text>{" "}</Text>
                  <Text bold color="blue">
                    {event.summary}
                  </Text>
                  {event.actor ? (
                    <>
                      <Text>{" "}</Text>
                      <Text dimColor>{`by ${event.actor}`}</Text>
                    </>
                  ) : null}
                  <Text>{" "}</Text>
                  <Text dimColor>{event.timestamp}</Text>
                </Box>
                {/* Vertical connector */}
                {!isLast && (
                  <Box>
                    <Text dimColor>{"│"}</Text>
                  </Box>
                )}
              </Box>
            );
          })}
        </Box>
      )}

      {/* Footer hint */}
      <Box marginTop={1} paddingX={2}>
        <Text dimColor>{"[Esc=back]"}</Text>
      </Box>
    </Box>
  );
}

export default TimelineView;
