import {
  type KeyboardEvent as ReactKeyboardEvent,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
} from "react";
import { read, utils } from "xlsx";

import { wikiFileUrl } from "@/api/wiki";

interface XlsxViewerProps {
  path: string;
}

/**
 * Hard cap on rendered rows per sheet. A spreadsheet can hold a million rows;
 * mounting that many <tr> nodes would lock the tab. We render the first
 * MAX_ROWS and surface a notice with the true total so the reader knows the
 * view is truncated.
 */
const MAX_ROWS = 500;

interface ParsedSheet {
  name: string;
  /** Row-major cell strings, already truncated to MAX_ROWS. */
  rows: string[][];
  /** Total data rows in the sheet before truncation. */
  totalRows: number;
  /** Widest row in the rendered slice. */
  colCount: number;
}

/** Coerce any SheetJS cell value into a display string without crashing. */
function cellToString(value: unknown): string {
  if (value === null || value === undefined) return "";
  if (value instanceof Date) return value.toISOString();
  if (typeof value === "object") {
    try {
      return JSON.stringify(value);
    } catch {
      return "";
    }
  }
  return String(value);
}

function parseSheet(name: string, rawRows: unknown[][]): ParsedSheet {
  const totalRows = rawRows.length;
  const slice = rawRows.slice(0, MAX_ROWS);
  const rows = slice.map((r) =>
    Array.isArray(r) ? r.map(cellToString) : [cellToString(r)],
  );
  const colCount = rows.reduce((max, r) => Math.max(max, r.length), 0);
  return { name, rows, totalRows, colCount };
}

type LoadState =
  | { status: "loading" }
  | { status: "error"; message: string }
  | { status: "ready"; sheets: ParsedSheet[] };

export default function XlsxViewer({ path }: XlsxViewerProps) {
  const [state, setState] = useState<LoadState>({ status: "loading" });
  const [active, setActive] = useState(0);
  // Stable id roots so each tab can be referenced by the panel's
  // aria-labelledby and vice versa across re-renders.
  const tabId = useId();
  const panelId = useId();
  // Refs to each tab button so arrow-key navigation can move DOM focus
  // (roving tabindex: only the active tab is in the Tab order).
  const tabRefs = useRef<(HTMLButtonElement | null)[]>([]);

  useEffect(() => {
    let cancelled = false;
    setState({ status: "loading" });
    setActive(0);
    (async () => {
      try {
        const res = await fetch(wikiFileUrl(path));
        if (cancelled) return;
        if (!res.ok) {
          throw new Error(`Failed to load file (HTTP ${res.status})`);
        }
        const buf = await res.arrayBuffer();
        if (cancelled) return;
        const wb = read(buf, { type: "array", cellDates: true });
        const sheets: ParsedSheet[] = wb.SheetNames.map((name) => {
          const ws = wb.Sheets[name];
          const rawRows = utils.sheet_to_json<unknown[]>(ws, {
            header: 1,
            blankrows: false,
            defval: "",
          });
          return parseSheet(name, rawRows);
        });
        if (cancelled) return;
        setState({ status: "ready", sheets });
      } catch (err) {
        if (cancelled) return;
        setState({
          status: "error",
          message:
            err instanceof Error ? err.message : "Failed to parse spreadsheet",
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
      <section className="wk-viewer wk-viewer--xlsx" aria-label={filename}>
        <div className="wk-viewer__loading" role="status">
          Parsing spreadsheet…
        </div>
      </section>
    );
  }

  if (state.status === "error") {
    return (
      <section className="wk-viewer wk-viewer--xlsx" aria-label={filename}>
        <div className="wk-viewer__error" role="alert">
          {state.message}
        </div>
      </section>
    );
  }

  const { sheets } = state;
  if (sheets.length === 0) {
    return (
      <section className="wk-viewer wk-viewer--xlsx" aria-label={filename}>
        <div className="wk-viewer__empty">This workbook has no sheets.</div>
      </section>
    );
  }

  const activeIndex = Math.min(active, sheets.length - 1);
  const current = sheets[activeIndex];
  const truncated = current.totalRows > current.rows.length;
  const cols = Array.from({ length: current.colCount }, (_, i) => i);
  const [header, ...body] = current.rows;
  const hasTabs = sheets.length > 1;

  // Roving-tabindex arrow navigation: Left/Right (and Home/End) move both the
  // selected sheet and DOM focus, so a screen-reader user can sweep the
  // tablist with the arrow keys per the WAI-ARIA tabs pattern.
  const onTabKeyDown = (event: ReactKeyboardEvent<HTMLButtonElement>): void => {
    let next: number | null = null;
    switch (event.key) {
      case "ArrowRight":
      case "ArrowDown":
        next = (activeIndex + 1) % sheets.length;
        break;
      case "ArrowLeft":
      case "ArrowUp":
        next = (activeIndex - 1 + sheets.length) % sheets.length;
        break;
      case "Home":
        next = 0;
        break;
      case "End":
        next = sheets.length - 1;
        break;
      default:
        return;
    }
    event.preventDefault();
    setActive(next);
    tabRefs.current[next]?.focus();
  };

  return (
    <section className="wk-viewer wk-viewer--xlsx" aria-label={filename}>
      <div className="wk-viewer__toolbar">
        <span className="wk-viewer__filename" title={filename}>
          {filename}
        </span>
        {hasTabs ? (
          <div
            className="wk-viewer__sheet-tabs"
            role="tablist"
            aria-label="Sheets"
          >
            {sheets.map((s, i) => (
              <button
                key={s.name}
                ref={(el) => {
                  tabRefs.current[i] = el;
                }}
                id={`${tabId}-${i}`}
                className="wk-viewer__sheet-tab"
                type="button"
                role="tab"
                aria-selected={i === activeIndex}
                aria-controls={`${panelId}-${i}`}
                aria-label={`Sheet ${s.name}`}
                tabIndex={i === activeIndex ? 0 : -1}
                onClick={() => setActive(i)}
                onKeyDown={onTabKeyDown}
              >
                {s.name}
              </button>
            ))}
          </div>
        ) : (
          <span>{current.name}</span>
        )}
        <span>
          {current.totalRows} {current.totalRows === 1 ? "row" : "rows"} ·{" "}
          {current.colCount} {current.colCount === 1 ? "column" : "columns"}
        </span>
      </div>
      {hasTabs ? (
        <div
          className="wk-viewer__body"
          id={`${panelId}-${activeIndex}`}
          role="tabpanel"
          aria-labelledby={`${tabId}-${activeIndex}`}
        >
          {renderSheetBody(current, cols, header, body, truncated)}
        </div>
      ) : (
        <section
          className="wk-viewer__body"
          aria-label={`${current.name} sheet`}
        >
          {renderSheetBody(current, cols, header, body, truncated)}
        </section>
      )}
    </section>
  );
}

/**
 * Render the table (or empty/truncation states) for one sheet. Extracted so
 * the tablist's tabpanel and the single-sheet region share identical body
 * markup without duplicating it inline.
 */
function renderSheetBody(
  current: ParsedSheet,
  cols: number[],
  header: string[] | undefined,
  body: string[][],
  truncated: boolean,
) {
  return (
    <>
      {current.rows.length === 0 ? (
        <div className="wk-viewer__empty">This sheet is empty.</div>
      ) : (
        <table>
          <thead>
            <tr>
              {cols.map((c) => (
                <th key={c} scope="col">
                  {header?.[c] ?? ""}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {body.map((r, ri) => (
              <tr key={`${ri}${r.join("")}`}>
                {cols.map((c) => (
                  <td key={c}>{r[c] ?? ""}</td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {truncated ? (
        <p className="wk-viewer__empty" role="status">
          Showing the first {current.rows.length} of {current.totalRows} rows.
          Download the file to see everything.
        </p>
      ) : null}
    </>
  );
}
