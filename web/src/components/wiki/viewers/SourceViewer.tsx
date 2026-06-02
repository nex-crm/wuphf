import { type ReactNode, useEffect, useMemo, useState } from "react";
import { common, createLowlight } from "lowlight";

import { wikiFileUrl } from "@/api/wiki";

import { highlightToReact } from "./highlightToReact";

interface SourceViewerProps {
  path: string;
}

/**
 * lowlight registry. `common` covers the languages we map below; building it
 * once at module scope avoids re-registering grammars on every render. The
 * dispatcher React.lazy-loads this whole module, so the heavy grammar bundle
 * lands in this viewer's own chunk.
 */
const lowlight = createLowlight(common);

/**
 * Curated extension -> highlight.js language map. Anything outside this set
 * falls back to plain (escaped) text rather than risking a mis-detection.
 * Keys are the lowercase extension WITHOUT the leading dot.
 */
const EXT_TO_LANG: Record<string, string> = {
  ts: "typescript",
  tsx: "typescript",
  js: "javascript",
  jsx: "javascript",
  json: "json",
  py: "python",
  go: "go",
  rust: "rust",
  rs: "rust",
  sql: "sql",
  css: "css",
  html: "xml",
  xml: "xml",
  yaml: "yaml",
  yml: "yaml",
  sh: "bash",
  bash: "bash",
  md: "markdown",
};

/**
 * Files larger than this are not highlighted or fully rendered — we show the
 * leading slice plus a notice so the tab never locks up tokenizing a huge log.
 */
const MAX_BYTES = 512 * 1024; // 512 KiB

function extensionOf(path: string): string {
  const name = path.split("/").pop() ?? path;
  const dot = name.lastIndexOf(".");
  if (dot <= 0 || dot === name.length - 1) return "";
  return name.slice(dot + 1).toLowerCase();
}

interface LoadedSource {
  /** Highlighted React nodes, one entry per source line. */
  lines: ReactNode[];
  /** True when the file exceeded MAX_BYTES and only a prefix is shown. */
  truncated: boolean;
}

/**
 * Highlight `text` for `language` (or render as plain React text when unknown),
 * then split into per-line React nodes. lowlight returns a hast tree whose text
 * is rendered via React (inherently escaped) — there is no innerHTML and nothing
 * to sanitize. Each line is highlighted on its own so the per-line gutter layout
 * stays intact; lowlight tokenizes line-by-line consistently, so a span never
 * needs to straddle a newline.
 */
function highlightToLines(text: string, language: string): ReactNode[] {
  const rawLines = text.split("\n");
  return rawLines.map((line) => {
    // A blank line renders as a single space so the row keeps its height under
    // `white-space: pre` (matching the previous `lineHtml || " "` behavior).
    if (line.length === 0) return " ";
    if (!language) return line;
    try {
      const tree = lowlight.highlight(language, line);
      return highlightToReact(tree);
    } catch {
      // On any highlighting failure fall back to the raw (React-escaped) line.
      return line;
    }
  });
}

/**
 * Source / plaintext viewer. Fetches the file as text, syntax-highlights known
 * languages with lowlight (already a repo dep), and renders <pre><code> with
 * line numbers and optional wrapping. Unknown extensions render as plain text;
 * very large files are capped with a visible notice.
 */
export default function SourceViewer({ path }: SourceViewerProps) {
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [source, setSource] = useState<LoadedSource | null>(null);
  const [wrap, setWrap] = useState(false);

  const src = useMemo(() => wikiFileUrl(path), [path]);
  const filename = useMemo(() => path.split("/").pop() || path, [path]);
  const ext = useMemo(() => extensionOf(path), [path]);
  const language = EXT_TO_LANG[ext] ?? "";

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    setSource(null);

    (async () => {
      try {
        const res = await fetch(src);
        if (!res.ok) {
          throw new Error(`Request failed with status ${res.status}`);
        }
        const fullText = await res.text();
        if (cancelled) return;

        const truncated = fullText.length > MAX_BYTES;
        const text = truncated ? fullText.slice(0, MAX_BYTES) : fullText;
        // An empty file yields `[""]` from split; treat it as no lines so the
        // empty state renders instead of a single blank gutter row.
        const lines = text.length === 0 ? [] : highlightToLines(text, language);
        setSource({ lines, truncated });
      } catch (err: unknown) {
        if (cancelled) return;
        setError(err instanceof Error ? err.message : "Failed to load file");
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [src, language]);

  const langLabel = language || (ext ? ext.toUpperCase() : "text");

  return (
    <section
      className="wk-viewer wk-viewer--source"
      aria-label={`Source file: ${filename}`}
    >
      <div className="wk-viewer__toolbar">
        <span title={path}>{filename}</span>
        <span aria-hidden="true">·</span>
        <span>{langLabel}</span>
        <span style={{ flex: 1 }} />
        <button
          type="button"
          className="wk-viewer__action"
          aria-pressed={wrap}
          onClick={() => setWrap((value) => !value)}
          title={wrap ? "Disable line wrap" : "Enable line wrap"}
        >
          {wrap ? "Wrap: on" : "Wrap: off"}
        </button>
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
          rel="noreferrer"
          title="Open the raw file in a new tab"
        >
          Raw
        </a>
      </div>
      <section className="wk-viewer__body" aria-label={`${filename} source`}>
        {loading ? (
          <div className="wk-viewer__loading" role="status">
            Loading {filename}…
          </div>
        ) : error ? (
          <div className="wk-viewer__error" role="alert">
            <p>Could not load this file.</p>
            <p>{error}</p>
            <a href={src} target="_blank" rel="noreferrer">
              Open the raw file
            </a>
          </div>
        ) : source && source.lines.length > 0 ? (
          <>
            {source.truncated && (
              <p className="wk-viewer__notice" role="status">
                This file is large; showing the first {MAX_BYTES / 1024} KB. Use
                Download or Raw for the full file.
              </p>
            )}
            <pre
              className={
                wrap
                  ? "wk-viewer__code wk-viewer__code--wrap"
                  : "wk-viewer__code"
              }
            >
              <code>
                {source.lines.map((lineNodes, index) => (
                  <span
                    // Index keys are stable here: the line list is derived
                    // from immutable fetched text and never reorders.
                    key={index}
                    className="wk-viewer__line"
                  >
                    <span className="wk-viewer__gutter" aria-hidden="true">
                      {index + 1}
                    </span>
                    <span className="wk-viewer__content">{lineNodes}</span>
                  </span>
                ))}
              </code>
            </pre>
          </>
        ) : (
          <div className="wk-viewer__empty">This file is empty.</div>
        )}
      </section>
    </section>
  );
}
