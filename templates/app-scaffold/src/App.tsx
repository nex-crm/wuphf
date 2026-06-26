import { useMemo, useState } from "react";
import {
  Badge,
  Button,
  Group,
  Stack,
  Table,
  Text,
  TextInput,
  Title,
} from "@mantine/core";
import { useTable } from "@refinedev/react-table";
import { type ColumnDef, flexRender } from "@tanstack/react-table";

import type { OfficeTask } from "./wuphf-bridge";

/**
 * Starter App — a real refine + Mantine data grid over the office task list.
 *
 * This is the pattern that matters: declare columns, call `useTable` (resource
 * "tasks" → bridgeDataProvider → getTasks()), and render the headless table model
 * into Mantine's <Table>. Sorting, filtering and pagination come from refine; you
 * never hand-roll them. Replace the columns + resource to build a different tool.
 */

// Map a lifecycle/status string to a Mantine badge color. Pure presentation.
function statusColor(status: string): string {
  const s = status.toLowerCase();
  if (s.includes("done") || s.includes("complete")) return "green";
  if (s.includes("running") || s.includes("progress")) return "blue";
  if (s.includes("block") || s.includes("fail") || s.includes("reject"))
    return "red";
  if (s.includes("plan") || s.includes("draft")) return "yellow";
  return "gray";
}

const columns: ColumnDef<OfficeTask>[] = [
  {
    id: "title",
    accessorKey: "title",
    header: "Task",
    cell: ({ getValue }) => (
      <Text fw={550}>{String(getValue() ?? "")}</Text>
    ),
  },
  {
    id: "owner",
    accessorKey: "owner",
    header: "Owner",
    cell: ({ getValue }) => {
      const owner = String(getValue() ?? "");
      return owner ? (
        <Text c="dimmed">{owner}</Text>
      ) : (
        <Text c="dimmed">—</Text>
      );
    },
  },
  {
    id: "status",
    // Prefer the richer lifecycle_state, fall back to status.
    accessorFn: (row) => row.lifecycle_state ?? row.status ?? "",
    header: "Status",
    cell: ({ getValue }) => {
      const status = String(getValue() ?? "");
      return status ? (
        <Badge color={statusColor(status)} variant="light">
          {status}
        </Badge>
      ) : (
        <Text c="dimmed">—</Text>
      );
    },
  },
];

export function App() {
  const [search, setSearch] = useState("");

  const { reactTable, refineCore } = useTable<OfficeTask>({
    columns,
    refineCoreProps: {
      resource: "tasks",
      // The sealed sandbox has no app-owned URL; keep table state in React.
      syncWithLocation: false,
      pagination: { pageSize: 10 },
    },
  });

  const { tableQuery } = refineCore;
  const isLoading = tableQuery.isLoading;
  const error = tableQuery.error;

  // Client-side title search over the loaded page set. (For server-side
  // filtering, drive refineCore.setFilters instead.)
  const rows = reactTable.getRowModel().rows;
  const filteredRows = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return rows;
    return rows.filter((r) =>
      String(r.original.title ?? "").toLowerCase().includes(q),
    );
  }, [rows, search]);

  const total = refineCore.tableQuery.data?.total ?? 0;
  const pageCount = reactTable.getPageCount();
  const pageIndex = reactTable.getState().pagination.pageIndex;

  return (
    <Stack className="app" p="lg" gap="md">
      <header>
        <Title order={2}>Office tasks</Title>
        <Text c="dimmed" size="sm">
          Live office task list, read through the WUPHF bridge. Click a column
          header to sort; type to filter; page through below.
        </Text>
      </header>

      <Group justify="space-between" align="flex-end" wrap="nowrap">
        <TextInput
          placeholder="Filter by title…"
          value={search}
          onChange={(e) => setSearch(e.currentTarget.value)}
          w={280}
          aria-label="Filter tasks by title"
        />
        <Text c="dimmed" size="sm">
          {total} task{total === 1 ? "" : "s"}
        </Text>
      </Group>

      {error ? (
        <Text c="red">
          {error instanceof Error ? error.message : "Failed to load tasks."}
        </Text>
      ) : isLoading ? (
        <Text c="dimmed">Loading tasks…</Text>
      ) : total === 0 ? (
        <Text c="dimmed">
          No tasks yet. When the office starts work, tasks show up here.
        </Text>
      ) : (
        <>
          <Table
            highlightOnHover
            withTableBorder
            withColumnBorders
            verticalSpacing="sm"
          >
            <Table.Thead>
              {reactTable.getHeaderGroups().map((hg) => (
                <Table.Tr key={hg.id}>
                  {hg.headers.map((header) => {
                    const canSort = header.column.getCanSort();
                    const sorted = header.column.getIsSorted();
                    return (
                      <Table.Th
                        key={header.id}
                        onClick={
                          canSort
                            ? header.column.getToggleSortingHandler()
                            : undefined
                        }
                        style={{ cursor: canSort ? "pointer" : "default" }}
                      >
                        {flexRender(
                          header.column.columnDef.header,
                          header.getContext(),
                        )}
                        {sorted === "asc" ? " ▲" : sorted === "desc" ? " ▼" : ""}
                      </Table.Th>
                    );
                  })}
                </Table.Tr>
              ))}
            </Table.Thead>
            <Table.Tbody>
              {filteredRows.length === 0 ? (
                <Table.Tr>
                  <Table.Td colSpan={columns.length}>
                    <Text c="dimmed">No tasks match “{search}”.</Text>
                  </Table.Td>
                </Table.Tr>
              ) : (
                filteredRows.map((row) => (
                  <Table.Tr key={row.id}>
                    {row.getVisibleCells().map((cell) => (
                      <Table.Td key={cell.id}>
                        {flexRender(
                          cell.column.columnDef.cell,
                          cell.getContext(),
                        )}
                      </Table.Td>
                    ))}
                  </Table.Tr>
                ))
              )}
            </Table.Tbody>
          </Table>

          {pageCount > 1 ? (
            <Group justify="flex-end" gap="xs">
              <Button
                variant="default"
                size="xs"
                onClick={() => reactTable.previousPage()}
                disabled={!reactTable.getCanPreviousPage()}
              >
                Previous
              </Button>
              <Text size="sm" c="dimmed">
                Page {pageIndex + 1} of {pageCount}
              </Text>
              <Button
                variant="default"
                size="xs"
                onClick={() => reactTable.nextPage()}
                disabled={!reactTable.getCanNextPage()}
              >
                Next
              </Button>
            </Group>
          ) : null}
        </>
      )}
    </Stack>
  );
}
