import React, { useState } from "react";
import { Box, Text } from "ink";
import { Picker } from "../components/picker.js";
import type { PickerItem } from "../components/picker.js";

// --- Types ---

export interface RecordRow {
  id: string;
  label: string;
  attributes: Record<string, string>;
}

export interface RecordListViewProps {
  objectType: string;
  records: RecordRow[];
  columns: string[];
  onSelect?: (record: RecordRow) => void;
  onBack?: () => void;
}

// --- Component ---

export function RecordListView({
  objectType,
  records,
  columns,
  onSelect,
  onBack: _onBack,
}: RecordListViewProps): React.JSX.Element {
  const [cursor, setCursor] = useState(0);

  // Build picker items from records
  const items: PickerItem[] = records.map((r) => ({
    command: r.id,
    label: r.label,
    detail: columns.map((col) => r.attributes[col] ?? "").join(" | "),
  }));

  const handleSelect = (item: PickerItem) => {
    const record = records.find((r) => r.id === item.command);
    if (record) onSelect?.(record);
  };

  return (
    <Box flexDirection="column" width="100%">
      {/* Header */}
      <Box paddingX={2} marginBottom={1}>
        <Text bold color="cyan">
          {objectType}
        </Text>
        <Text dimColor>{` (${records.length} records)`}</Text>
      </Box>

      {/* Column headers */}
      <Box paddingX={3}>
        <Box minWidth={4}>
          <Text bold dimColor>{"#"}</Text>
        </Box>
        <Box minWidth={20}>
          <Text bold>{"Name"}</Text>
        </Box>
        {columns.map((col) => (
          <Box key={col} minWidth={16}>
            <Text bold dimColor>{col}</Text>
          </Box>
        ))}
      </Box>

      {/* Records */}
      <Picker
        items={items}
        cursor={cursor}
        onSelect={handleSelect}
        onCursorChange={setCursor}
        quickSelect
      />
    </Box>
  );
}

export default RecordListView;
