import { useEffect, useMemo, useState } from "react";

import { wikiFileUrl } from "@/api/wiki";

interface CsvViewerProps {
  path: string;
}

/**
 * RFC-4180-ish CSV parser. Handles:
 *  - quoted fields ("a,b" stays one field)
 *  - escaped quotes inside quoted fields ("" -> ")
 *  - embedded commas and newlines inside quoted fields
 *  - both CRLF and LF line endings (and a bare trailing CR)
 *  - a trailing newline without emitting a spurious empty row
 *
 * No external dependency: a small hand-rolled state machine. The grid is
 * returned as a 2D array of strings; the first row is treated as the header.
 */
function parseCsv(text: string): string[][] {
  const rows: string[][] = [];
  let field = "";
  let row: string[] = [];
  let inQuotes = false;
  let fieldStarted = false;

  const pushField = () => {
    row.push(field);
    field = "";
    fieldStarted = false;
  };
  const pushRow = () => {
    pushField();
    rows.push(row);
    row = [];
  };

  for (let i = 0; i < text.length; i++) {
    const ch = text[i];

    if (inQuotes) {
      if (ch === '"') {
        if (text[i + 1] === '"') {
          field += '"';
          i++;
        } else {
          inQuotes = false;
        }
      } else {
        field += ch;
      }
      continue;
    }

    if (ch === '"' && !fieldStarted) {
      inQuotes = true;
      fieldStarted = true;
    } else if (ch === ",") {
      pushField();
    } else if (ch === "\n") {
      pushRow();
    } else if (ch === "\r") {
      // Treat CR or CRLF as a single line break.
      if (text[i + 1] === "\n") i++;
      pushRow();
    } else {
      field += ch;
      fieldStarted = true;
    }
  }

  // Flush the final field/row unless the file ended exactly on a line break
  // (in which case `field` is empty and `row` is empty — nothing buffered).
  if (field !== "" || row.length > 0) {
    pushRow();
  }

  return rows;
}

type LoadState =
  | { status: "loading" }
  | { status: "error"; message: string }
  | { status: "ready"; rows: string[][] };

export default function CsvViewer({ path }: CsvViewerProps) {
  const [state, setState] = useState<LoadState>({ status: "loading" });

  useEffect(() => {
    let cancelled = false;
    setState({ status: "loading" });
    (async () => {
      try {
        const res = await fetch(wikiFileUrl(path));
        if (cancelled) return;
        if (!res.ok) {
          throw new Error(`Failed to load file (HTTP ${res.status})`);
        }
        const text = await res.text();
        if (cancelled) return;
        setState({ status: "ready", rows: parseCsv(text) });
      } catch (err) {
        if (cancelled) return;
        setState({
          status: "error",
          message:
            err instanceof Error ? err.message : "Failed to read CSV file",
        });
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [path]);

  const filename = useMemo(() => path.split("/").pop() ?? path, [path]);

  if (state.status === "loading") {
    return (
      <section className="wk-viewer wk-viewer--csv" aria-label={filename}>
        <div className="wk-viewer__loading" role="status">
          Loading spreadsheet…
        </div>
      </section>
    );
  }

  if (state.status === "error") {
    return (
      <section className="wk-viewer wk-viewer--csv" aria-label={filename}>
        <div className="wk-viewer__error" role="alert">
          {state.message}
        </div>
      </section>
    );
  }

  const { rows } = state;
  if (rows.length === 0) {
    return (
      <section className="wk-viewer wk-viewer--csv" aria-label={filename}>
        <div className="wk-viewer__empty">This CSV file is empty.</div>
      </section>
    );
  }

  const [header, ...body] = rows;
  const colCount = rows.reduce((max, r) => Math.max(max, r.length), 0);
  const cols = Array.from({ length: colCount }, (_, i) => i);

  return (
    <section className="wk-viewer wk-viewer--csv" aria-label={filename}>
      <div className="wk-viewer__toolbar">
        <span title={filename}>{filename}</span>
        <span>
          {body.length} {body.length === 1 ? "row" : "rows"} · {colCount}{" "}
          {colCount === 1 ? "column" : "columns"}
        </span>
      </div>
      <section className="wk-viewer__body" aria-label={`${filename} table`}>
        <table>
          <thead>
            <tr>
              {cols.map((c) => (
                <th key={c} scope="col">
                  {header[c] ?? ""}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {body.map((r, ri) => (
              <tr key={`${ri}${r.join("")}`}>
                {cols.map((c) => (
                  <td key={c}>{r[c] ?? ""}</td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </section>
    </section>
  );
}
