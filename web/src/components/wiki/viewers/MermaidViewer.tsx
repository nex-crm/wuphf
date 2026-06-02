import { useEffect, useRef, useState } from "react";
import DOMPurify from "dompurify";

import { wikiFileUrl } from "../../../api/wiki";

/**
 * MermaidViewer — fetches a `.mmd` / `.mermaid` text file and renders it as an
 * SVG diagram with the installed `mermaid` library.
 *
 * Security: defense in depth. `mermaid.initialize({ securityLevel: "strict" })`
 * disables click handlers + HTML labels and sanitizes its own output, AND we
 * run the rendered SVG through DOMPurify (svg profiles, scripts/foreignObject
 * stripped) — the same posture `RichArtifactEmbed` uses for agent-emitted SVG.
 * The sanitized SVG is rendered through an `<img src="data:image/svg+xml,…">`
 * rather than injected as markup, so there is no raw-HTML injection and any
 * script that somehow survived sanitization still cannot execute (browsers never
 * run script inside an image-context SVG). We also parse first and skip render
 * on a parse failure so mermaid never injects its own error SVG into the
 * document body. The diagram re-renders whenever `path` changes.
 */
interface MermaidViewerProps {
  path: string;
}

const VIEWER_ID_PREFIX = "wk-mermaid-";

/** What the body should display, derived from the load + toggle state. */
type MermaidView =
  | { kind: "loading" }
  | { kind: "error"; message: string }
  | { kind: "source"; source: string }
  | { kind: "diagram"; svg: string; filename: string }
  | { kind: "empty" };

/** The raw load + toggle flags the viewer tracks. */
interface MermaidState {
  loading: boolean;
  error: string | null;
  showSource: boolean;
  source: string;
  svg: string;
  filename: string;
}

/**
 * Collapse the load + toggle flags into a single discriminated view. Kept as a
 * standalone pure function so the branching lives here rather than inflating
 * the component's own cognitive complexity.
 */
function deriveView(state: MermaidState): MermaidView {
  if (state.loading) return { kind: "loading" };
  if (state.error) return { kind: "error", message: state.error };
  if (state.showSource && state.source) {
    return { kind: "source", source: state.source };
  }
  if (state.svg) {
    return { kind: "diagram", svg: state.svg, filename: state.filename };
  }
  return { kind: "empty" };
}

/** The rendered diagram (sanitized SVG as a data-URL image). */
function MermaidDiagram({ svg, filename }: { svg: string; filename: string }) {
  return (
    <img
      className="wk-viewer__mermaid-svg"
      // The strict-mode + DOMPurify-sanitized SVG is rendered as an image
      // (data: URL), so it is loaded in an image context where scripts can
      // never run — strictly safer than injecting markup.
      src={`data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`}
      alt={`Diagram: ${filename}`}
    />
  );
}

/**
 * Render the body for one of the viewer's display states. Each state maps to a
 * single small branch; the diagram itself lives in its own component so this
 * stays a flat dispatch rather than a nested JSX ladder.
 */
function MermaidBody({ view }: { view: MermaidView }) {
  if (view.kind === "loading") {
    return <p className="wk-viewer__loading">Rendering diagram…</p>;
  }
  if (view.kind === "error") {
    return (
      <div className="wk-viewer__error" role="alert">
        <p>Could not render this diagram.</p>
        <pre>{view.message}</pre>
      </div>
    );
  }
  if (view.kind === "source") {
    return (
      <pre className="wk-viewer__code wk-viewer__code--wrap">
        <code>{view.source}</code>
      </pre>
    );
  }
  if (view.kind === "diagram") {
    return <MermaidDiagram svg={view.svg} filename={view.filename} />;
  }
  return <p className="wk-viewer__empty">No diagram to show.</p>;
}

// Mirror RichArtifactEmbed: svg/svgFilters profiles exclude <script> and
// <foreignObject>, so no executable markup survives sanitization.
function sanitizeSvg(svg: string): string {
  return DOMPurify.sanitize(svg, {
    USE_PROFILES: { svg: true, svgFilters: true },
  });
}

export default function MermaidViewer({ path }: MermaidViewerProps) {
  const [source, setSource] = useState<string>("");
  const [svg, setSvg] = useState<string>("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  // Toolbar toggle between the rendered diagram and the raw `.mmd` source.
  const [showSource, setShowSource] = useState(false);
  const [copied, setCopied] = useState(false);
  // Monotonic counter so each render() gets a unique id even when path repeats.
  const renderSeq = useRef(0);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    setSvg("");
    setSource("");
    setShowSource(false);

    async function load(): Promise<void> {
      try {
        const res = await fetch(wikiFileUrl(path));
        if (!res.ok) throw new Error(`Failed to load diagram (${res.status})`);
        const text = (await res.text()).trim();
        if (!cancelled) setSource(text);
        if (!text) throw new Error("Diagram source is empty");

        const mermaid = (await import("mermaid")).default;
        mermaid.initialize({
          startOnLoad: false,
          securityLevel: "strict",
          suppressErrorRendering: true,
        });

        // Validate syntax before render so mermaid never injects an error
        // glyph into document.body on our behalf.
        await mermaid.parse(text);

        const id = `${VIEWER_ID_PREFIX}${++renderSeq.current}`;
        const { svg: rendered } = await mermaid.render(id, text);
        if (!cancelled) setSvg(sanitizeSvg(rendered));
      } catch (err: unknown) {
        if (!cancelled) {
          setError(
            err instanceof Error ? err.message : "Failed to render diagram",
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

  // Reset the transient "Copied" label after a short window.
  useEffect(() => {
    if (!copied) return;
    const timer = window.setTimeout(() => setCopied(false), 2000);
    return () => window.clearTimeout(timer);
  }, [copied]);

  const filename = path.split("/").pop() || path;

  const copySource = (): void => {
    if (!source) return;
    void navigator.clipboard?.writeText(source).then(
      () => setCopied(true),
      () => {
        /* clipboard denied — leave the label unchanged */
      },
    );
  };

  const downloadSvg = (): void => {
    if (!svg) return;
    const blob = new Blob([svg], { type: "image/svg+xml" });
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = `${filename.replace(/\.[^.]+$/, "")}.svg`;
    anchor.click();
    URL.revokeObjectURL(url);
  };

  const view = deriveView({
    loading,
    error,
    showSource,
    source,
    svg,
    filename,
  });

  return (
    <section className="wk-viewer wk-viewer--mermaid" aria-label={filename}>
      <div className="wk-viewer__toolbar">
        <span className="wk-viewer__filename" title={path}>
          {filename}
        </span>
        <span className="wk-viewer__spacer" aria-hidden="true" />
        {svg ? (
          <button
            type="button"
            className="wk-viewer__action"
            aria-pressed={showSource}
            onClick={() => setShowSource((value) => !value)}
            title={showSource ? "Show the diagram" : "Show the source"}
          >
            {showSource ? "Diagram" : "Code"}
          </button>
        ) : null}
        <button
          type="button"
          className="wk-viewer__action"
          onClick={copySource}
          disabled={!source}
          title="Copy the diagram source to the clipboard"
        >
          {copied ? "Copied" : "Copy"}
        </button>
        {svg ? (
          <button
            type="button"
            className="wk-viewer__action"
            onClick={downloadSvg}
            title="Download the rendered diagram as an SVG"
          >
            Download SVG
          </button>
        ) : null}
      </div>
      <section className="wk-viewer__body" aria-label="Mermaid diagram">
        <MermaidBody view={view} />
      </section>
    </section>
  );
}
