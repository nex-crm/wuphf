import { type ComponentType, lazy, Suspense } from "react";

import FileFallbackViewer from "./FileFallbackViewer";
import { type FileKind, fileKindForPath } from "./fileKind";

/**
 * FileViewer — the wiki's non-article viewer dispatcher. Given a
 * repo-root-relative file path it infers the file kind from the extension and
 * renders the matching viewer. Every heavy viewer is wrapped in `React.lazy`
 * so its chunk (and its parser dependency — xlsx / docx-preview / pptx-preview
 * / mermaid / lowlight) stays out of the main bundle and only loads when a file
 * of that kind is actually opened.
 *
 * The fallback (filename + download + open-in-new-tab) is imported eagerly: it
 * is tiny, has no heavy dependency, and is the last-resort surface we never
 * want to show a loading spinner for.
 *
 * Routing is delegated to the pure `fileKindForPath` helper (see fileKind.ts)
 * so the tree/route can pre-decide viewable-ness with `isViewablePath` without
 * importing any of the lazy viewer modules.
 */

interface ViewerProps {
  path: string;
}

// Each viewer is its own lazy chunk. Keep the import paths static string
// literals so the bundler can statically split them.
const ImageViewer = lazy(() => import("./ImageViewer"));
const MediaViewer = lazy(() => import("./MediaViewer"));
const PdfViewer = lazy(() => import("./PdfViewer"));
const CsvViewer = lazy(() => import("./CsvViewer"));
const XlsxViewer = lazy(() => import("./XlsxViewer"));
const DocxViewer = lazy(() => import("./DocxViewer"));
const PptxViewer = lazy(() => import("./PptxViewer"));
const NotebookViewer = lazy(() => import("./NotebookViewer"));
const MermaidViewer = lazy(() => import("./MermaidViewer"));
const SourceViewer = lazy(() => import("./SourceViewer"));
const GoogleDocViewer = lazy(() => import("./GoogleDocViewer"));

/**
 * Map each viewable kind to its lazy component. `fallback` is intentionally
 * absent — it is rendered eagerly outside Suspense so a missing key never
 * resolves to a lazy import.
 */
const LAZY_VIEWER_BY_KIND: Record<
  Exclude<FileKind, "fallback">,
  ComponentType<ViewerProps>
> = {
  image: ImageViewer,
  media: MediaViewer,
  pdf: PdfViewer,
  csv: CsvViewer,
  xlsx: XlsxViewer,
  docx: DocxViewer,
  pptx: PptxViewer,
  notebook: NotebookViewer,
  mermaid: MermaidViewer,
  source: SourceViewer,
  google: GoogleDocViewer,
};

interface FileViewerProps {
  /** Repo-root-relative wiki path (with the `team/` prefix, e.g. team/assets/x.pdf). */
  path: string;
}

export default function FileViewer({ path }: FileViewerProps) {
  const kind = fileKindForPath(path);

  if (kind === "fallback") {
    return <FileFallbackViewer path={path} />;
  }

  const Viewer = LAZY_VIEWER_BY_KIND[kind];
  const filename = path.split("/").pop() || path;

  return (
    <Suspense
      fallback={
        <div
          className="wk-viewer wk-viewer--loading-shell"
          data-testid="wk-viewer-loading"
        >
          <div className="wk-viewer__body">
            <div className="wk-viewer__loading" role="status">
              Loading {filename}…
            </div>
          </div>
        </div>
      }
    >
      {/* `path` keys the viewer so switching files inside the same kind remounts
          cleanly rather than reusing stale internal state. */}
      <Viewer key={path} path={path} />
    </Suspense>
  );
}

export type { FileKind } from "./fileKind";
// Re-export the routing helpers so the tree/route can decide viewable-ness and
// label a node without importing any of the lazy viewer modules.
export {
  fileKindForPath,
  fileKindLabel,
  isMarkdownPath,
  isViewablePath,
} from "./fileKind";
