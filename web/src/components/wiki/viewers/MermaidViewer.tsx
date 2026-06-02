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

// Mirror RichArtifactEmbed: svg/svgFilters profiles exclude <script> and
// <foreignObject>, so no executable markup survives sanitization.
function sanitizeSvg(svg: string): string {
  return DOMPurify.sanitize(svg, {
    USE_PROFILES: { svg: true, svgFilters: true },
  });
}

export default function MermaidViewer({ path }: MermaidViewerProps) {
  const [svg, setSvg] = useState<string>("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  // Monotonic counter so each render() gets a unique id even when path repeats.
  const renderSeq = useRef(0);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    setSvg("");

    async function load(): Promise<void> {
      try {
        const res = await fetch(wikiFileUrl(path));
        if (!res.ok) throw new Error(`Failed to load diagram (${res.status})`);
        const text = (await res.text()).trim();
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

  const filename = path.split("/").pop() || path;

  return (
    <section className="wk-viewer wk-viewer--mermaid" aria-label={filename}>
      <div className="wk-viewer__toolbar">
        <span title={path}>{filename}</span>
      </div>
      <section className="wk-viewer__body" aria-label="Mermaid diagram">
        {loading ? (
          <p className="wk-viewer__loading">Rendering diagram…</p>
        ) : error ? (
          <div className="wk-viewer__error" role="alert">
            <p>Could not render this diagram.</p>
            <pre>{error}</pre>
          </div>
        ) : svg ? (
          <img
            className="wk-viewer__mermaid-svg"
            // The strict-mode + DOMPurify-sanitized SVG is rendered as an image
            // (data: URL), so it is loaded in an image context where scripts can
            // never run — strictly safer than injecting markup.
            src={`data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`}
            alt={`Diagram: ${filename}`}
          />
        ) : (
          <p className="wk-viewer__empty">No diagram to show.</p>
        )}
      </section>
    </section>
  );
}
