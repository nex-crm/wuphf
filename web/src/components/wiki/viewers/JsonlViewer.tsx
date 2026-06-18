import { useEffect, useMemo, useState } from "react";

import { wikiFileUrl } from "@/api/wiki";

/** Hard cap on rendered records. JSONL fact/event logs can run to tens of
 * thousands of lines; we render the first MAX_RECORDS and surface a notice with
 * the true total so the tab stays responsive. */
const MAX_RECORDS = 1000;

/** Above this many distinct top-level keys the table becomes unreadable, so we
 * fall back to per-record cards even when every record is a flat object. */
const MAX_TABLE_COLUMNS = 12;

/** Stringified cell values longer than this are clipped (full value stays in
 * the cell's title attribute) so one fat field can't blow out a column. */
const MAX_CELL_CHARS = 200;

interface JsonlViewerProps {
  path: string;
}

interface ParsedRecord {
  /** 1-based line number in the source file. */
  line: number;
  /** Parsed JSON value, or undefined when the line failed to parse. */
  value?: unknown;
  /** Raw line text — shown verbatim when parsing failed. */
  raw: string;
  ok: boolean;
}

/**
 * Parse a JSON Lines document: one JSON value per non-blank line. Blank lines
 * are skipped. A line that fails to parse is kept (flagged `ok: false`) so the
 * viewer can show it as-is rather than dropping data silently.
 */
function parseJsonl(text: string): ParsedRecord[] {
  const records: ParsedRecord[] = [];
  const lines = text.split(/\r\n|\r|\n/);
  for (let i = 0; i < lines.length; i++) {
    const raw = lines[i];
    if (raw.trim() === "") continue;
    try {
      records.push({ line: i + 1, value: JSON.parse(raw), raw, ok: true });
    } catch {
      records.push({ line: i + 1, raw, ok: false });
    }
  }
  return records;
}

/** True for a plain JSON object ({}), false for arrays, null, and primitives. */
function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/** Render any JSON value as a compact, human-readable string for a table cell.
 * A missing key (undefined) and an explicit JSON null both read as "no value"
 * (an em-dash) so empty cells are unambiguous rather than blank. */
function formatCell(value: unknown): string {
  if (value === null || value === undefined) return "—";
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }
  return JSON.stringify(value);
}

function clip(text: string): string {
  return text.length > MAX_CELL_CHARS
    ? `${text.slice(0, MAX_CELL_CHARS)}…`
    : text;
}

type LoadState =
  | { status: "loading" }
  | { status: "error"; message: string }
  | { status: "ready"; records: ParsedRecord[] };

/** How the records should render: a shared-key table, or per-record cards. */
type Layout = { mode: "cards" } | { mode: "table"; keys: string[] };

/**
 * Choose a table layout only when every parsed record is a flat object and the
 * union of their keys stays small enough to read; otherwise fall back to
 * per-record cards. Pure so the component's useMemo stays a thin wrapper.
 */
function decideLayout(records: ParsedRecord[]): Layout {
  const parsed = records.filter((r) => r.ok);
  const allObjects =
    parsed.length > 0 && parsed.every((r) => isPlainObject(r.value));
  if (!allObjects) return { mode: "cards" };
  const keys: string[] = [];
  const seen = new Set<string>();
  for (const r of parsed) {
    for (const k of Object.keys(r.value as Record<string, unknown>)) {
      if (!seen.has(k)) {
        seen.add(k);
        keys.push(k);
      }
    }
    if (keys.length > MAX_TABLE_COLUMNS) return { mode: "cards" };
  }
  // Records that are all empty objects ({}) yield no columns — a 0-column table
  // is useless, so show the cards instead.
  if (keys.length === 0) return { mode: "cards" };
  return { mode: "table", keys };
}

/** Fetches and parses the JSONL file at `path`, exposing a load state. Kept as
 * a hook so the component body stays a thin render of that state. */
function useJsonlRecords(path: string): LoadState {
  const [state, setState] = useState<LoadState>({ status: "loading" });
  useEffect(() => {
    let cancelled = false;
    setState({ status: "loading" });
    fetch(wikiFileUrl(path))
      .then(async (res) => {
        if (!res.ok)
          throw new Error(`Failed to load file (HTTP ${res.status})`);
        return res.text();
      })
      .then((text) => {
        if (!cancelled) {
          setState({ status: "ready", records: parseJsonl(text) });
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setState({
            status: "error",
            message:
              err instanceof Error ? err.message : "Failed to read JSONL file",
          });
        }
      });
    return () => {
      cancelled = true;
    };
  }, [path]);
  return state;
}

/** Shared-key table body: one column per key, missing/null cells as em-dashes,
 * and an unparseable line shown verbatim across the full row width. */
function JsonlTable({
  records,
  keys,
}: {
  records: ParsedRecord[];
  keys: string[];
}) {
  return (
    <table>
      <thead>
        <tr>
          {keys.map((k) => (
            <th key={k} scope="col">
              {k}
            </th>
          ))}
        </tr>
      </thead>
      <tbody>
        {records.map((r) => {
          if (!r.ok) {
            return (
              <tr key={r.line} className="wk-viewer__row--error">
                <td colSpan={keys.length} title={r.raw}>
                  {clip(r.raw)}
                </td>
              </tr>
            );
          }
          const obj = r.value as Record<string, unknown>;
          return (
            <tr key={r.line}>
              {keys.map((k) => {
                const cell = formatCell(obj[k]);
                return (
                  <td key={k} title={cell}>
                    {clip(cell)}
                  </td>
                );
              })}
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}

/** Per-record card body: one pretty-printed JSON block per record, used when
 * the records are heterogeneous or too wide for a table. */
function JsonlCards({ records }: { records: ParsedRecord[] }) {
  return (
    <ol className="wk-viewer__records">
      {records.map((r) => (
        <li key={r.line} className="wk-viewer__record">
          <pre
            className={
              r.ok
                ? "wk-viewer__record-json"
                : "wk-viewer__record-json wk-viewer__record-json--error"
            }
          >
            {r.ok ? JSON.stringify(r.value, null, 2) : r.raw}
          </pre>
        </li>
      ))}
    </ol>
  );
}

/**
 * JsonlViewer — renders a `.jsonl` (JSON Lines) file as a readable record view.
 * When every record is a flat object with a small, shared key set it renders a
 * table; otherwise it falls back to one pretty-printed card per record. Either
 * way a non-technical reader sees structured fields instead of a raw blob, and
 * a malformed line is surfaced verbatim rather than dropped.
 */
export default function JsonlViewer({ path }: JsonlViewerProps) {
  const state = useJsonlRecords(path);
  const filename = useMemo(() => path.split("/").pop() ?? path, [path]);

  // Decide table-vs-cards once per load (see decideLayout).
  const layout = useMemo(
    () => (state.status === "ready" ? decideLayout(state.records) : null),
    [state],
  );

  if (state.status === "loading") {
    return (
      <section className="wk-viewer wk-viewer--jsonl" aria-label={filename}>
        <div className="wk-viewer__loading" role="status">
          Loading records…
        </div>
      </section>
    );
  }

  if (state.status === "error") {
    return (
      <section className="wk-viewer wk-viewer--jsonl" aria-label={filename}>
        <div className="wk-viewer__error" role="alert">
          {state.message}
        </div>
      </section>
    );
  }

  const { records } = state;
  if (records.length === 0) {
    return (
      <section className="wk-viewer wk-viewer--jsonl" aria-label={filename}>
        <div className="wk-viewer__empty">This JSONL file is empty.</div>
      </section>
    );
  }

  const total = records.length;
  const shown = records.slice(0, MAX_RECORDS);
  const truncated = total > shown.length;
  const badLines = records.filter((r) => !r.ok).length;
  const src = wikiFileUrl(path);

  return (
    <section className="wk-viewer wk-viewer--jsonl" aria-label={filename}>
      <div className="wk-viewer__toolbar">
        <span className="wk-viewer__filename" title={filename}>
          {filename}
        </span>
        <span className="wk-viewer__meta">
          {total} {total === 1 ? "record" : "records"}
          {badLines > 0
            ? ` · ${badLines} unparseable ${badLines === 1 ? "line" : "lines"}`
            : ""}
        </span>
        <span className="wk-viewer__spacer" aria-hidden="true" />
        <a
          className="wk-viewer__action"
          href={src}
          download={filename}
          title={`Download ${filename}`}
        >
          Download
        </a>
        <a
          className="wk-viewer__action"
          href={src}
          target="_blank"
          rel="noreferrer noopener"
          title="Open the raw file in a new tab"
        >
          Open in new tab
        </a>
      </div>
      <section className="wk-viewer__body" aria-label={`${filename} records`}>
        {layout?.mode === "table" ? (
          <JsonlTable records={shown} keys={layout.keys} />
        ) : (
          <JsonlCards records={shown} />
        )}
        {truncated ? (
          <p className="wk-viewer__notice" role="status">
            Showing the first {shown.length} of {total} records. Download the
            file to see everything.
          </p>
        ) : null}
      </section>
    </section>
  );
}
