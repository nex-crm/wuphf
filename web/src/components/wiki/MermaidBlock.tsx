import { useEffect, useRef, useState } from "react";
import DOMPurify from "dompurify";

/**
 * MermaidBlock — renders a fenced ```mermaid code block from a compiled
 * article as an SVG diagram. Mermaid is imported lazily (dynamic import) so it
 * never weighs on the initial bundle, and only the read view pulls it in.
 *
 * Security posture mirrors MermaidViewer / RichArtifactEmbed: strict security
 * level + DOMPurify (svg profiles) + render through a `data:` image so no raw
 * HTML is injected and any surviving script cannot execute in an image
 * context. Parse failures fall back to the raw source as a code block rather
 * than letting mermaid inject its own error glyph into the document.
 */

interface MermaidBlockProps {
  code: string;
}

type RenderState =
  | { status: "loading" }
  | { status: "ok"; svg: string }
  | { status: "fallback" };

const RENDER_ID_PREFIX = "wk-mermaid-block-";

function sanitizeSvg(svg: string): string {
  return DOMPurify.sanitize(svg, {
    USE_PROFILES: { svg: true, svgFilters: true },
  });
}

export default function MermaidBlock({ code }: MermaidBlockProps) {
  const [state, setState] = useState<RenderState>({ status: "loading" });
  const seq = useRef(0);

  useEffect(() => {
    let cancelled = false;
    setState({ status: "loading" });
    const source = code.trim();
    if (!source) {
      setState({ status: "fallback" });
      return;
    }

    async function render(): Promise<void> {
      try {
        const mermaid = (await import("mermaid")).default;
        mermaid.initialize({
          startOnLoad: false,
          securityLevel: "strict",
          suppressErrorRendering: true,
        });
        // Validate before render so mermaid never injects an error SVG.
        await mermaid.parse(source);
        const id = `${RENDER_ID_PREFIX}${++seq.current}`;
        const { svg } = await mermaid.render(id, source);
        if (!cancelled) setState({ status: "ok", svg: sanitizeSvg(svg) });
      } catch {
        if (!cancelled) setState({ status: "fallback" });
      }
    }

    void render();
    return () => {
      cancelled = true;
    };
  }, [code]);

  if (state.status === "ok") {
    return (
      <figure className="wk-mermaid">
        <img
          className="wk-mermaid-svg"
          src={`data:image/svg+xml;charset=utf-8,${encodeURIComponent(state.svg)}`}
          alt="Diagram"
        />
      </figure>
    );
  }

  if (state.status === "fallback") {
    // Could not render — show the source so the content is never lost.
    return (
      <pre className="wk-mermaid-fallback">
        <code>{code}</code>
      </pre>
    );
  }

  return (
    <figure className="wk-mermaid" aria-busy="true">
      <span className="wk-mermaid-loading">Rendering diagram…</span>
    </figure>
  );
}
