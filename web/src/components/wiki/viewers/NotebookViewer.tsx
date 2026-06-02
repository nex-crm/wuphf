import { type ReactNode, useEffect, useMemo, useState } from "react";
import ReactMarkdown from "react-markdown";
import { common, createLowlight } from "lowlight";
import remarkGfm from "remark-gfm";

import { wikiFileUrl } from "../../../api/wiki";
import { highlightToReact } from "./highlightToReact";

/**
 * NotebookViewer — fetches a Jupyter `.ipynb` (JSON) file and renders its cells
 * in order:
 *
 *   - markdown cells via the app's react-markdown + remark-gfm pipeline,
 *   - code cells syntax-highlighted with lowlight (highlight.js grammars). The
 *     hast tree lowlight returns is rendered to React elements directly — no
 *     raw-HTML injection, no extra serialization dep,
 *   - text/plain, image/png and text/html outputs, rendered safely. HTML
 *     outputs are NOT executed: they go into a sandboxed <iframe srcDoc> with
 *     no allow-scripts, so embedded <script> never runs.
 *
 * Every output is capped in size so a multi-megabyte cell output cannot lock
 * the tab.
 */
interface NotebookViewerProps {
  path: string;
}

// ── nbformat v4 surface (only the fields we render) ─────────────────────────

type StringOrLines = string | string[];

interface StreamOutput {
  output_type: "stream";
  name?: string;
  text?: StringOrLines;
}
interface DataOutput {
  output_type: "execute_result" | "display_data";
  execution_count?: number | null;
  data?: Record<string, StringOrLines>;
}
interface ErrorOutput {
  output_type: "error";
  ename?: string;
  evalue?: string;
  traceback?: string[];
}
type NotebookOutput =
  | StreamOutput
  | DataOutput
  | ErrorOutput
  | { output_type: string };

interface CodeCell {
  cell_type: "code";
  source?: StringOrLines;
  execution_count?: number | null;
  outputs?: NotebookOutput[];
}
interface MarkdownCell {
  cell_type: "markdown";
  source?: StringOrLines;
}
interface RawCell {
  cell_type: "raw";
  source?: StringOrLines;
}
type NotebookCell = CodeCell | MarkdownCell | RawCell | { cell_type: string };

interface Notebook {
  cells?: NotebookCell[];
  metadata?: {
    kernelspec?: { name?: string; display_name?: string };
    language_info?: { name?: string };
  };
}

const lowlight = createLowlight(common);

// Hard caps so a hostile / huge notebook cannot freeze the tab.
const MAX_OUTPUT_CHARS = 50_000;
const MAX_HTML_CHARS = 200_000;
// Cap the base64 length of an embedded image. A crafted notebook could carry
// an arbitrarily large `image/png` payload; decoding ~2.7 MB of base64 (~2 MB
// of bytes) into an <img src> is already generous, and anything larger gets a
// placeholder instead of being mounted.
const MAX_IMAGE_B64_CHARS = 2_700_000;
// ANSI CSI sequences: ESC (0x1b) "[" params final-letter. Build the ESC from
// its char code so the source carries no literal control character.
const ANSI_RE = new RegExp(`${String.fromCharCode(27)}\\[[0-9;]*[A-Za-z]`, "g");

function joinSource(s: StringOrLines | undefined): string {
  if (Array.isArray(s)) return s.join("");
  return s ?? "";
}

function stripAnsi(s: string): string {
  return s.replace(ANSI_RE, "");
}

function capText(s: string, max = MAX_OUTPUT_CHARS): string {
  return s.length > max ? `${s.slice(0, max)}\n… (output truncated)` : s;
}

// ── lowlight hast → React (no raw-HTML injection) ───────────────────────────

function highlight(code: string, language: string): ReactNode {
  try {
    const tree =
      language && lowlight.registered(language)
        ? lowlight.highlight(language, code)
        : lowlight.highlightAuto(code);
    // Shared converter walks lowlight's hast Root into React nodes.
    return highlightToReact(tree);
  } catch {
    // On any highlighting failure fall back to the raw (already React-escaped)
    // source text — never throw out of the render path.
    return code;
  }
}

// ── outputs ─────────────────────────────────────────────────────────────────

function StreamOutputView({ output }: { output: StreamOutput }) {
  const text = capText(stripAnsi(joinSource(output.text)));
  const isErr = output.name === "stderr";
  return (
    <pre
      className={`wk-nb__stream${isErr ? " wk-nb__stream--err" : ""}`}
      data-stream={output.name ?? "stdout"}
    >
      {text}
    </pre>
  );
}

function ErrorOutputView({ output }: { output: ErrorOutput }) {
  const head = [output.ename, output.evalue].filter(Boolean).join(": ");
  const tb = capText(
    (output.traceback ?? []).map(stripAnsi).join("\n"),
    MAX_OUTPUT_CHARS,
  );
  return (
    <pre className="wk-nb__stream wk-nb__stream--err" role="alert">
      {head ? <strong>{head}</strong> : null}
      {tb ? (head ? `\n\n${tb}` : tb) : null}
    </pre>
  );
}

function DataOutputView({
  output,
  cellIndex,
}: {
  output: DataOutput;
  cellIndex: number;
}) {
  const data = output.data ?? {};
  const png = data["image/png"];
  if (png) {
    const b64 = joinSource(png).replace(/\s/g, "");
    if (b64.length > MAX_IMAGE_B64_CHARS) {
      // Oversized image payload — skip mounting it so a crafted notebook can
      // not balloon memory; tell the reader why the image is not shown.
      return (
        <p className="wk-nb__stream" role="status">
          Image output from cell {cellIndex + 1} is too large to display.
        </p>
      );
    }
    return (
      <img
        className="wk-nb__image"
        src={`data:image/png;base64,${b64}`}
        alt={`Output from notebook cell ${cellIndex + 1}`}
      />
    );
  }
  const html = data["text/html"];
  if (html) {
    // Render arbitrary HTML output (pandas tables, plotly) inside a sandboxed
    // iframe WITHOUT allow-scripts, so any embedded <script> is inert.
    const raw = capText(joinSource(html), MAX_HTML_CHARS);
    const doc = `<!doctype html><html><head><base target="_blank"></head><body>${raw}</body></html>`;
    return (
      <iframe
        className="wk-nb__html"
        title="Notebook HTML output"
        sandbox=""
        srcDoc={doc}
      />
    );
  }
  const plain = data["text/plain"];
  if (plain) {
    return (
      <pre className="wk-nb__stream">
        {capText(stripAnsi(joinSource(plain)))}
      </pre>
    );
  }
  return null;
}

function CellOutputView({
  output,
  cellIndex,
}: {
  output: NotebookOutput;
  cellIndex: number;
}) {
  if (output.output_type === "stream") {
    return <StreamOutputView output={output as StreamOutput} />;
  }
  if (output.output_type === "error") {
    return <ErrorOutputView output={output as ErrorOutput} />;
  }
  if (
    output.output_type === "execute_result" ||
    output.output_type === "display_data"
  ) {
    return (
      <DataOutputView output={output as DataOutput} cellIndex={cellIndex} />
    );
  }
  return null;
}

// ── cells ─────────────────────────────────────────────────────────────────

function CodeCellView({
  cell,
  language,
  cellIndex,
}: {
  cell: CodeCell;
  language: string;
  cellIndex: number;
}) {
  const source = joinSource(cell.source);
  const highlighted = useMemo(
    () => highlight(source, language),
    [source, language],
  );
  const count =
    typeof cell.execution_count === "number" ? cell.execution_count : " ";
  const outputs = cell.outputs ?? [];

  return (
    <div className="wk-nb__cell wk-nb__cell--code">
      <div className="wk-nb__gutter" aria-hidden="true">{`In [${count}]:`}</div>
      <div className="wk-nb__code">
        <pre>
          <code className={language ? `language-${language}` : undefined}>
            {highlighted}
          </code>
        </pre>
        {outputs.length > 0 ? (
          <div className="wk-nb__outputs">
            {outputs.map((output, i) => (
              <CellOutputView key={i} output={output} cellIndex={cellIndex} />
            ))}
          </div>
        ) : null}
      </div>
    </div>
  );
}

function MarkdownCellView({ cell }: { cell: MarkdownCell }) {
  return (
    <div className="wk-nb__cell wk-nb__cell--markdown">
      <ReactMarkdown remarkPlugins={[remarkGfm]}>
        {joinSource(cell.source)}
      </ReactMarkdown>
    </div>
  );
}

function RawCellView({ cell }: { cell: RawCell }) {
  return (
    <div className="wk-nb__cell wk-nb__cell--raw">
      <pre className="wk-nb__stream">{joinSource(cell.source)}</pre>
    </div>
  );
}

// ── viewer ──────────────────────────────────────────────────────────────────

export default function NotebookViewer({ path }: NotebookViewerProps) {
  const [notebook, setNotebook] = useState<Notebook | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    setNotebook(null);

    async function load(): Promise<void> {
      try {
        const res = await fetch(wikiFileUrl(path));
        if (!res.ok) throw new Error(`Failed to load notebook (${res.status})`);
        const json = (await res.json()) as Notebook;
        if (!json || typeof json !== "object") {
          throw new Error("Not a valid notebook file");
        }
        if (!cancelled) setNotebook(json);
      } catch (err: unknown) {
        if (!cancelled) {
          setError(
            err instanceof Error ? err.message : "Failed to load notebook",
          );
        }
      } finally {
        if (!cancelled) setLoading(false);
      }
    }

    void load();
    return () => {
      cancelled = true;
    };
  }, [path]);

  const filename = path.split("/").pop() || path;
  const src = wikiFileUrl(path);
  const cells = notebook?.cells ?? [];
  const language =
    notebook?.metadata?.language_info?.name ||
    notebook?.metadata?.kernelspec?.name ||
    "python";
  const codeCellCount = cells.filter((c) => c.cell_type === "code").length;
  // True when the notebook carries code cells but none has been executed yet —
  // matches cabinet's "hasn't been run" hint so a freshly-authored notebook
  // does not read as broken when it shows no outputs.
  const hasAnyOutputs = cells.some(
    (c) => c.cell_type === "code" && ((c as CodeCell).outputs?.length ?? 0) > 0,
  );

  return (
    <section className="wk-viewer wk-viewer--notebook" aria-label={filename}>
      <div className="wk-viewer__toolbar">
        <span className="wk-viewer__filename" title={path}>
          {filename}
        </span>
        {notebook ? (
          <span className="wk-viewer__meta">{`${cells.length} cells · ${codeCellCount} code · ${language}`}</span>
        ) : null}
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
          title="Open the raw notebook JSON in a new tab"
        >
          Raw
        </a>
      </div>
      <section className="wk-viewer__body" aria-label="Notebook contents">
        {loading ? (
          <p className="wk-viewer__loading">Loading notebook…</p>
        ) : error ? (
          <div className="wk-viewer__error" role="alert">
            <p>Could not load this notebook.</p>
            <pre>{error}</pre>
          </div>
        ) : cells.length === 0 ? (
          <p className="wk-viewer__empty">This notebook has no cells.</p>
        ) : (
          <div className="wk-nb">
            {!hasAnyOutputs && codeCellCount > 0 ? (
              <p className="wk-viewer__notice" role="status">
                This notebook has not been run yet — code and markdown cells are
                shown below, and outputs appear once the author runs it.
              </p>
            ) : null}
            {cells.map((cell, i) => {
              if (cell.cell_type === "markdown") {
                return <MarkdownCellView key={i} cell={cell as MarkdownCell} />;
              }
              if (cell.cell_type === "raw") {
                return <RawCellView key={i} cell={cell as RawCell} />;
              }
              if (cell.cell_type === "code") {
                return (
                  <CodeCellView
                    key={i}
                    cell={cell as CodeCell}
                    language={language}
                    cellIndex={i}
                  />
                );
              }
              return null;
            })}
          </div>
        )}
      </section>
    </section>
  );
}
