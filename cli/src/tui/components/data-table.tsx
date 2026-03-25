import React from "react";
import { Box, Text } from "ink";

// --- Types ---

export interface DataTableProps {
  headers: string[];
  rows: string[][];
  /** Column indices (0-based) to right-align */
  alignRight?: number[];
  /** Max width for any column before truncation with "…" (default: 40) */
  maxColWidth?: number;
}

// --- Helpers ---

function truncate(value: string, max: number): string {
  if (value.length <= max) return value;
  return value.slice(0, max - 1) + "\u2026";
}

function padCell(value: string, width: number, right: boolean): string {
  if (right) return value.padStart(width);
  return value.padEnd(width);
}

function computeColumnWidths(
  headers: string[],
  rows: string[][],
  maxColWidth: number,
): number[] {
  return headers.map((h, i) => {
    let max = h.length;
    for (const row of rows) {
      const cell = row[i] ?? "";
      if (cell.length > max) max = cell.length;
    }
    return Math.min(max, maxColWidth);
  });
}

// --- Component ---

export function DataTable({
  headers,
  rows,
  alignRight = [],
  maxColWidth = 40,
}: DataTableProps): React.JSX.Element {
  const rightSet = new Set(alignRight);
  const widths = computeColumnWidths(headers, rows, maxColWidth);
  const separator = widths.map((w) => "\u2500".repeat(w)).join("\u2500\u253C\u2500");

  return (
    <Box flexDirection="column">
      {/* Header */}
      <Text>
        {headers.map((h, i) => {
          const truncated = truncate(h, widths[i]);
          const padded = padCell(truncated, widths[i], rightSet.has(i));
          const sep = i < headers.length - 1 ? " \u2502 " : "";
          return (
            <React.Fragment key={i}>
              <Text bold color="blue" underline>
                {padded}
              </Text>
              {sep && <Text>{sep}</Text>}
            </React.Fragment>
          );
        })}
      </Text>

      {/* Separator */}
      <Text dimColor>{separator}</Text>

      {/* Rows */}
      {rows.map((row, ri) => {
        const dim = ri % 2 === 1;
        return (
          <Text key={ri} dimColor={dim}>
            {row.map((cell, ci) => {
              const truncated = truncate(cell ?? "", widths[ci]);
              const padded = padCell(truncated, widths[ci], rightSet.has(ci));
              const sep = ci < headers.length - 1 ? " \u2502 " : "";
              return padded + sep;
            }).join("")}
          </Text>
        );
      })}

      {/* Footer */}
      <Text dimColor>
        {"\n"}{rows.length} row{rows.length !== 1 ? "s" : ""}
      </Text>
    </Box>
  );
}

export default DataTable;
