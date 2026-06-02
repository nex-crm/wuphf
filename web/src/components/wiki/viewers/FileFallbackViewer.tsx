import { useMemo } from "react";

import { wikiFileUrl } from "@/api/wiki";

import { fileKindLabel } from "./fileKind";

interface FileFallbackViewerProps {
  path: string;
}

/**
 * Catch-all viewer for cabinet files with no dedicated in-app renderer
 * (binaries, archives, fonts, unknown formats). Rather than a blank pane it
 * shows the filename, a kind chip, and the two actions that always work for an
 * authenticated file URL: download and open-in-new-tab.
 *
 * `wikiFileUrl(path)` carries the auth token as a query param in direct-broker
 * mode, so it is safe to use directly as an anchor href (an `<a>` cannot carry
 * an Authorization header).
 */
export default function FileFallbackViewer({ path }: FileFallbackViewerProps) {
  const src = useMemo(() => wikiFileUrl(path), [path]);
  const filename = useMemo(() => path.split("/").pop() || path, [path]);
  const label = useMemo(() => fileKindLabel(path), [path]);

  return (
    <section
      className="wk-viewer wk-viewer--fallback"
      aria-label={`File: ${filename}`}
    >
      <div className="wk-viewer__toolbar">
        <span className="wk-viewer__filename" title={path}>
          {filename}
        </span>
        <span className="wk-viewer__kind" aria-hidden="true">
          {label}
        </span>
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
          rel="noreferrer noopener"
          title="Open the raw file in a new tab"
        >
          Open in new tab
        </a>
      </div>
      <div className="wk-viewer__body">
        <div className="wk-viewer__empty">
          <p className="wk-viewer__empty-title">No preview for this file</p>
          <p>
            <code>{filename}</code> can't be previewed in the cabinet. Download
            it or open it in a new tab to view the contents.
          </p>
        </div>
      </div>
    </section>
  );
}
