/**
 * PptxViewer — renders a PowerPoint deck (.pptx) inside the wiki using the
 * `pptx-preview` library. `init(dom, options)` returns a previewer bound to a
 * container we own; `preview(arrayBuffer)` parses the deck and injects the
 * rendered slide DOM into that container. We render into a `ref` we own,
 * never setting inner HTML ourselves, and call `destroy()` on unmount so a
 * remount or path change starts clean.
 *
 * The dispatcher React.lazy-loads this whole module, so the static
 * `pptx-preview` import lands in this viewer's own chunk (correct code
 * splitting). We never re-import it dynamically.
 *
 * The rendered bytes come from the team's own wiki, served through the
 * authenticated `/wiki/file` endpoint (see `wikiFileUrl`), not from untrusted
 * external input.
 */

import { useEffect, useRef, useState } from "react";
import { init } from "pptx-preview";

import { wikiFileUrl } from "../../../api/wiki";

interface PptxViewerProps {
  path: string;
}

type ViewState = "loading" | "ready" | "error";

/** The slice of the pptx-preview previewer instance we depend on. */
interface PptxPreviewer {
  preview: (file: ArrayBuffer) => Promise<unknown>;
  destroy: () => void;
}

/**
 * Fetch the deck bytes, initialize a previewer bound to `container`, and
 * render the slides. Returns the previewer so the caller can `destroy()` it on
 * teardown. Kept as a top-level helper so the effect body stays flat.
 */
async function loadPresentation(
  path: string,
  container: HTMLElement,
): Promise<PptxPreviewer> {
  const res = await fetch(wikiFileUrl(path));
  if (!res.ok) {
    throw new Error(`Could not load presentation (HTTP ${res.status})`);
  }
  const buffer = await res.arrayBuffer();

  // Size slides to the container; fall back to a sane 16:9 default when the
  // container has not been laid out yet (e.g. in tests).
  const width = container.clientWidth || 960;
  const height = Math.round((width * 9) / 16);
  const previewer = init(container, {
    width,
    height,
    mode: "list",
  }) as unknown as PptxPreviewer;

  await previewer.preview(buffer);
  return previewer;
}

export default function PptxViewer({ path }: PptxViewerProps) {
  const containerRef = useRef<HTMLElement | null>(null);
  const [state, setState] = useState<ViewState>("loading");
  const [errorMessage, setErrorMessage] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    const container = containerRef.current;
    if (!container) return;

    // Clear any DOM a previous render injected before starting a new one.
    container.replaceChildren();
    setState("loading");
    setErrorMessage(null);

    // Holds the previewer instance so cleanup can destroy it even though the
    // load resolves asynchronously after this effect body returns.
    const handle: { previewer: PptxPreviewer | null } = { previewer: null };

    loadPresentation(path, container)
      .then((previewer) => {
        handle.previewer = previewer;
        if (cancelled) {
          // Unmounted before render finished — destroy what we just built.
          try {
            previewer.destroy();
          } catch {
            // ignore teardown errors
          }
          return;
        }
        setState("ready");
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setErrorMessage(
          err instanceof Error ? err.message : "Failed to render presentation",
        );
        setState("error");
      });

    return () => {
      cancelled = true;
      try {
        handle.previewer?.destroy();
      } catch {
        // The previewer may not have finished initializing; ignore teardown
        // errors so unmount never throws.
      }
      container.replaceChildren();
    };
  }, [path]);

  const fileName = path.split("/").pop() ?? path;
  const fileUrl = wikiFileUrl(path);

  return (
    <section className="wk-viewer wk-viewer--pptx" aria-label={fileName}>
      <div className="wk-viewer__toolbar">
        <span className="wk-viewer__filename" title={fileName}>
          {fileName}
        </span>
        <span style={{ flex: 1 }} />
        <a
          className="wk-viewer__action"
          href={fileUrl}
          download={fileName}
          title={`Download ${fileName}`}
        >
          Download
        </a>
        <a
          className="wk-viewer__action"
          href={fileUrl}
          target="_blank"
          rel="noreferrer noopener"
          title="Open this presentation in a new browser tab"
        >
          Open in new tab
        </a>
      </div>
      {state === "loading" && (
        <div className="wk-viewer__loading" role="status">
          Rendering slides…
        </div>
      )}
      {state === "error" && (
        <div className="wk-viewer__error" role="alert">
          <p>{errorMessage ?? "Failed to render presentation."}</p>
          <p>
            Download the file or open it in a new tab to read it in a slides
            editor.
          </p>
        </div>
      )}
      {/*
        The body is always mounted (even while loading/errored) because
        pptx-preview needs the container element present in the DOM to write
        into. We hide it until ready via the error/loading overlays above. It
        is a named <section> so it reads as a region landmark; we omit a
        focusable tabIndex because the slides scroll with the pointer.
      */}
      <section
        ref={containerRef}
        className="wk-viewer__body"
        aria-label={`${fileName} presentation`}
        hidden={state !== "ready"}
      />
    </section>
  );
}
