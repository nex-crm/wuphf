import { useMemo } from "react";

import { wikiFileUrl } from "@/api/wiki";

interface PdfViewerProps {
  path: string;
}

/**
 * Browser-native PDF viewer. The served bytes are streamed into an <iframe>
 * so the host browser's built-in PDF reader handles paging, zoom, and search;
 * we add a toolbar with download + open-in-new-tab affordances.
 *
 * `wikiFileUrl(path)` carries the auth token as a query param when running in
 * direct-broker mode, so it is safe to use as both the iframe src and the
 * anchor href (an <a> cannot carry an Authorization header either).
 *
 * There is no inline error state: an <iframe>'s `onError` does not fire for a
 * 404 / cross-origin PDF (the browser swallows the navigation), so an error
 * branch driven by it would be dead code. The toolbar's Download and
 * Open-in-new-tab links are the escape hatch when the inline render fails.
 */
export default function PdfViewer({ path }: PdfViewerProps) {
  const src = useMemo(() => wikiFileUrl(path), [path]);
  const filename = useMemo(() => path.split("/").pop() || path, [path]);

  return (
    <section
      className="wk-viewer wk-viewer--pdf"
      aria-label={`PDF: ${filename}`}
    >
      <div className="wk-viewer__toolbar">
        <span title={path}>{filename}</span>
        <span style={{ flex: 1 }} />
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
          title="Open this PDF in a new browser tab"
        >
          Open in new tab
        </a>
      </div>
      {/* The iframe is itself focusable, so the body wrapper needs no tabIndex;
          it is a named region landmark instead. */}
      <section className="wk-viewer__body" aria-label={`${filename} preview`}>
        <iframe
          src={src}
          title={`PDF document: ${filename}`}
          style={{ width: "100%", height: "100%", border: 0 }}
        />
      </section>
    </section>
  );
}
