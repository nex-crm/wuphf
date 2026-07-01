// AppDataTab — the Data tab: the app's BACKING DATA MODEL, shown as its database.
//
// Apps do not ship a real DB, but they DO define a data model — the entities
// they manage and the fields (including COMPUTED ones like an urgency score or
// an action item) they render. This tab reconstructs that model as a set of
// typed tables ("think of it as the app's DB", and it may have several) by
// reading the app's source plus the real records it reads, then deriving the
// entities + rows once via the AI bridge. It is a best-effort readable view of
// the data that powers the app, not a live store.

import { useQuery } from "@tanstack/react-query";

import { get, post } from "../../api/client";
import { hasAnyCapability } from "../apps/appCapabilities";
import { useAppCapabilities } from "../apps/useOperatorApps";
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

const DATA_MODEL_PROMPT = `You are given a WUPHF app's source code and a sample of the REAL records it reads.
Reconstruct the app's BACKING DATA MODEL as a set of database tables — think of it as the app's DB.
Return ONLY JSON of exactly this shape (no prose, no markdown fences):
{"tables":[{"name":"Emails","columns":[{"name":"sender","type":"string"},{"name":"urgencyScore","type":"number"}],"rows":[{"sender":"a@b.com","urgencyScore":72}]}]}
Rules:
- One table per entity the app manages. It is fine, and often right, to have MULTIPLE tables.
- "columns" are that entity's typed fields. Use types: string, number, date, boolean, or string[].
- "rows" are the entity's records built from the provided real records: COMPUTE any field the app
  defines but the raw source lacks (e.g. an urgency score 0-100, a one-line summary, an action item,
  a group and its count) from the record content, exactly as the app itself would.
- At most 8 rows per table. Keep string values short. Return ONLY the JSON.`;

// Pull the record array out of a bridge payload (GMAIL_FETCH_EMAILS nests under
// result.data.messages; other reads may be a bare array).
function extractRecords(raw: unknown): unknown[] {
  if (Array.isArray(raw)) return raw;
  const o = (raw ?? {}) as { messages?: unknown; data?: unknown };
  if (Array.isArray(o.messages)) return o.messages;
  const data = (o.data ?? {}) as { messages?: unknown };
  if (Array.isArray(data.messages)) return data.messages;
  if (Array.isArray(o.data)) return o.data;
  return [];
}

// Parse the AI's derived model defensively (it may return a JSON string or an
// object, and fields may be missing) into a clean set of tables.
function parseTables(raw: unknown): ModelTable[] {
  let obj: unknown = raw;
  if (typeof obj === "string") {
    try {
      obj = JSON.parse(obj);
    } catch {
      return [];
    }
  }
  const tables = (obj as { tables?: unknown })?.tables;
  if (!Array.isArray(tables)) return [];
  return tables
    .map((t) => {
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
      const rows = Array.isArray(tt.rows)
        ? (tt.rows as Record<string, unknown>[])
        : [];
      return {
        name: String(tt.name ?? "Table").trim() || "Table",
        columns,
        rows,
      };
    })
    .filter((t) => t.columns.length > 0);
}

export function AppDataTab({ appId }: AppDataTabProps) {
  const capsQuery = useAppCapabilities(appId);
  const caps = capsQuery.data;
  const bridgeApis = caps?.bridge_apis ?? [];
  const readsEmails = bridgeApis.includes("getEmails");
  const readsTasks = bridgeApis.includes("getTasks");
  const canModel = Boolean(
    caps && hasAnyCapability(caps) && (readsEmails || readsTasks),
  );

  const modelQuery = useQuery({
    queryKey: ["operator-app-data-model", appId],
    enabled: canModel,
    staleTime: 5 * 60_000,
    queryFn: async (): Promise<ModelTable[]> => {
      // 1. The app's source — the definition of its data model.
      const detail = await get<{ source?: Record<string, string> }>(
        `/apps/${encodeURIComponent(appId)}?source=1`,
      );
      const files = detail.source ?? {};
      const appCode = [files["src/App.tsx"], files["src/main.tsx"]]
        .filter(Boolean)
        .join("\n\n")
        .slice(0, 10_000);

      // 2. The real records the app reads.
      let records: unknown[] = [];
      if (readsEmails) {
        const r = await post<{ result?: unknown }>("/apps/integrations/call", {
          platform: "gmail",
          action: "GMAIL_FETCH_EMAILS",
          params: { query: "newer_than:14d", max_results: 12 },
          app_id: appId,
        });
        records = extractRecords(r.result);
      } else if (readsTasks) {
        const r = await get<{ tasks?: unknown[] }>(
          "/tasks?all_channels=true&include_done=true&viewer_slug=human",
        );
        records = (r.tasks ?? []).slice(0, 12);
      }

      // 3. Derive the DB (tables + typed columns + computed rows) once.
      const ai = await post<{ result?: unknown }>("/apps/ai", {
        prompt: DATA_MODEL_PROMPT,
        input: { appCode, records },
        json: true,
        app_id: appId,
      });
      return parseTables(ai.result);
    },
  });

  if (capsQuery.isLoading) {
    return (
      <div className="opr-app-building" role="status">
        <span className="opr-work-dots" aria-hidden={true}>
          <span />
          <span />
          <span />
        </span>
        <div className="opr-empty-title">Reading this app…</div>
      </div>
    );
  }

  if (!canModel) {
    return (
      <EmptyState
        glyph="▦"
        title="No data model yet"
        hint="This app does not read a workspace collection yet. Once it reads email or tasks, its data model — the tables that power it — shows here."
      />
    );
  }

  if (modelQuery.isLoading) {
    return (
      <div className="opr-app-building" role="status">
        <span className="opr-work-dots" aria-hidden={true}>
          <span />
          <span />
          <span />
        </span>
        <div className="opr-empty-title">Building the data model…</div>
      </div>
    );
  }

  const tables = modelQuery.data ?? [];
  if (modelQuery.isError || tables.length === 0) {
    return (
      <EmptyState
        glyph="▦"
        title="Could not build the data model"
        hint="The workspace could not derive this app's tables right now. Try again in a moment."
      />
    );
  }

  return (
    <div className="opr-tool-scoped opr-app-data">
      {tables.map((t) => (
        <ModelTableView key={t.name} table={t} />
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
    </div>
  );
}
