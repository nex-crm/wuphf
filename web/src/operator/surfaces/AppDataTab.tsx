// AppDataTab — the Data tab: the app's real, persisted BACKING DATABASE.
//
// Every app has a small typed store of its own (per app, server-side). The app
// derives its model ONCE from the source it reads, persists it with the bridge
// `db.*` API (defineTable + upsert), and renders from it — see "The app's
// database" in the app-scaffold AI_RULES. This tab is a DETERMINISTIC, direct
// read of that store: GET /apps/{id}/db → the tables the app itself wrote. No AI
// reconstruction, no re-fetch of the source — what the app persisted is what
// shows here, so the two never drift.

import { useQuery } from "@tanstack/react-query";

import { get } from "../../api/client";
import { EmptyState } from "../components/EmptyState";

interface AppDataTabProps {
  appId: string;
}

interface ModelColumn {
  name: string;
  type: string;
}
interface ModelTable {
  name: string;
  columns: ModelColumn[];
  rows: Record<string, unknown>[];
}

// Parse the broker's GET /apps/{id}/db payload into clean tables. Defensive: the
// wire shape is trusted (our own broker), but tolerate missing fields so a
// half-written table never crashes the tab.
function parseTables(raw: unknown): ModelTable[] {
  const tables = (raw as { tables?: unknown })?.tables;
  if (!Array.isArray(tables)) return [];
  return tables.map((t) => {
    const tt = (t ?? {}) as {
      name?: unknown;
      columns?: unknown;
      rows?: unknown;
    };
    const columns = Array.isArray(tt.columns)
      ? tt.columns
          .map((c) => {
            const cc = (c ?? {}) as { name?: unknown; type?: unknown };
            return {
              name: String(cc.name ?? "").trim(),
              type: String(cc.type ?? "string").trim() || "string",
            };
          })
          .filter((c) => c.name)
      : [];
    // Keep only plain objects: a null or array entry would crash the cell
    // lookup (row[c.name]) at render time.
    const rows = Array.isArray(tt.rows)
      ? tt.rows.filter(
          (r): r is Record<string, unknown> =>
            !!r && typeof r === "object" && !Array.isArray(r),
        )
      : [];
    return {
      name: String(tt.name ?? "Table").trim() || "Table",
      columns,
      rows,
    };
  });
}

export function AppDataTab({ appId }: AppDataTabProps) {
  const dbQuery = useQuery({
    queryKey: ["operator-app-db", appId],
    // The app writes to its DB through the bridge in a different component
    // tree, so nothing invalidates this key. It is a cheap local read: always
    // refetch on mount so the tab never shows a stale snapshot.
    refetchOnMount: "always",
    queryFn: async (): Promise<ModelTable[]> => {
      const res = await get<{ tables?: unknown }>(
        `/apps/${encodeURIComponent(appId)}/db`,
      );
      return parseTables(res);
    },
  });

  if (dbQuery.isLoading) {
    return (
      <div className="opr-app-building" role="status">
        <span className="opr-work-dots" aria-hidden={true}>
          <span />
          <span />
          <span />
        </span>
        <div className="opr-empty-title">Reading this app’s data…</div>
      </div>
    );
  }

  if (dbQuery.isError) {
    return (
      <EmptyState
        glyph="▦"
        title="Could not read this app’s data"
        hint="The workspace could not load this app’s database right now. Try again in a moment."
      />
    );
  }

  const tables = dbQuery.data ?? [];
  if (tables.length === 0) {
    return (
      <EmptyState
        glyph="▦"
        title="No data yet"
        hint="This app has not written to its database yet. As it derives and saves its data model — the tables that power it — they appear here."
      />
    );
  }

  return (
    <div className="opr-tool-scoped opr-app-data">
      {tables.map((t, i) => (
        // Name+index key: parseTables falls back to "Table" for a half-written
        // table, so bare names can collide and misapply reconciliation.
        <ModelTableView key={`${t.name}-${i}`} table={t} />
      ))}
    </div>
  );
}

function cellValue(v: unknown): string {
  if (v == null) return "—";
  if (Array.isArray(v)) return v.map((x) => String(x)).join(", ");
  if (typeof v === "object") return JSON.stringify(v);
  const s = String(v);
  return s.length > 80 ? `${s.slice(0, 80)}…` : s;
}

function ModelTableView({ table }: { table: ModelTable }) {
  return (
    <div className="opr-data-block">
      <div className="opr-data-block-head">
        {table.name}
        <span className="opr-data-block-sub">
          {table.rows.length} {table.rows.length === 1 ? "row" : "rows"}
        </span>
      </div>
      {table.rows.length === 0 ? (
        <div className="opr-data-empty">
          Defined, no rows yet — the app has declared this table but not written
          to it.
        </div>
      ) : (
        <table className="opr-data-table">
          <thead>
            <tr>
              {table.columns.map((c) => (
                <th key={c.name}>
                  <span className="opr-data-col-name">{c.name}</span>
                  <span className="opr-data-col-type">{c.type}</span>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {table.rows.map((row, i) => (
              <tr key={`${cellValue(row[table.columns[0]?.name ?? ""])}-${i}`}>
                {table.columns.map((c) => (
                  <td key={c.name}>{cellValue(row[c.name])}</td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
