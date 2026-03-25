import React from "react";
import { Box, Text } from "ink";

// --- Types ---

export interface CalendarEvent {
  id: string;
  agentName: string;
  day: number; // 0=Sun, 1=Mon ... 6=Sat
  hour: number; // 0-23
  label: string;
}

export interface CalendarViewProps {
  events: CalendarEvent[];
  weekOffset?: number; // 0 = current week
  onBack?: () => void;
}

// --- Constants ---

const DAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
const HOUR_SLOTS = [8, 10, 12, 14, 16, 18, 20]; // Show key hours

// --- Helpers ---

function getWeekDateRange(offset: number): { start: Date; end: Date } {
  const now = new Date();
  const dayOfWeek = now.getDay();
  const start = new Date(now);
  start.setDate(now.getDate() - dayOfWeek + offset * 7);
  start.setHours(0, 0, 0, 0);
  const end = new Date(start);
  end.setDate(start.getDate() + 6);
  return { start, end };
}

function formatDate(d: Date): string {
  return `${d.getMonth() + 1}/${d.getDate()}`;
}

// --- Component ---

export function CalendarView({
  events,
  weekOffset = 0,
  onBack: _onBack,
}: CalendarViewProps): React.JSX.Element {
  const { start, end } = getWeekDateRange(weekOffset);

  return (
    <Box flexDirection="column" width="100%">
      {/* Header */}
      <Box paddingX={2} marginBottom={1}>
        <Text bold color="cyan">
          {"Agent Calendar"}
        </Text>
        <Text dimColor>
          {`  ${formatDate(start)} - ${formatDate(end)}`}
        </Text>
      </Box>

      {/* Day headers */}
      <Box paddingX={2}>
        <Box width={6}>
          <Text dimColor>{"Time"}</Text>
        </Box>
        {DAYS.map((day) => (
          <Box key={day} width={12}>
            <Text bold>{day}</Text>
          </Box>
        ))}
      </Box>

      {/* Hour rows */}
      {HOUR_SLOTS.map((hour) => (
        <Box key={hour} paddingX={2}>
          <Box width={6}>
            <Text dimColor>{`${String(hour).padStart(2, "0")}:00`}</Text>
          </Box>
          {DAYS.map((_, dayIdx) => {
            const slotEvents = events.filter(
              (e) => e.day === dayIdx && e.hour >= hour && e.hour < hour + 2,
            );
            return (
              <Box key={dayIdx} width={12}>
                {slotEvents.length > 0 ? (
                  <Text color="cyan">
                    {slotEvents[0].label.slice(0, 10)}
                  </Text>
                ) : (
                  <Text dimColor>{"\u00B7"}</Text>
                )}
              </Box>
            );
          })}
        </Box>
      ))}

      {/* Legend */}
      <Box paddingX={2} marginTop={1}>
        <Text dimColor>
          {"Scheduled heartbeats shown. Use \u2190/\u2192 to change week."}
        </Text>
      </Box>

      {/* Navigation hint */}
      <Box marginTop={1} paddingX={2}>
        <Text dimColor>{"[Esc=back]"}</Text>
      </Box>
    </Box>
  );
}

export default CalendarView;
