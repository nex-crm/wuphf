import { useCallback, useEffect, useState } from "react";

import { wikiFileUrl } from "../../../api/wiki";
import { useFocusTrap } from "../editor/inserts/useFocusTrap";

interface ImageViewerProps {
  path: string;
}

type LoadStatus = "loading" | "ready" | "error";

/**
 * Renders a raster or vector image straight from the wiki file surface.
 *
 * The broker serves SVGs sandboxed, so a plain `<img>` is safe for every
 * supported extension (png/jpg/jpeg/gif/webp/avif/svg/bmp/ico). The element
 * `src` points at `wikiFileUrl(path)` directly — that URL already carries the
 * auth token when running in direct-broker mode, so no fetch is needed for the
 * happy path.
 *
 * `<img>` cannot surface a parse/transport error through a promise, so we lean
 * on its native `onLoad` / `onError` events to drive the loading and error
 * states the shared viewer contract requires.
 *
 * The zoomable image is a real `<button>` so it is reachable by keyboard and
 * announced correctly; activating it opens a token-styled lightbox overlay
 * that fits the image to the viewport. Escape or a click on the backdrop
 * closes it.
 */

/**
 * The lightbox is a focus-trapped modal. Splitting it into its own component
 * means `useFocusTrap` mounts/unmounts with the dialog: on open it moves focus
 * into the dialog and traps Tab inside it, and on close it restores focus to
 * the zoom button that opened it. Escape is handled by the parent's keydown
 * effect, so this component owns only Tab/focus management plus click-to-close.
 */
function ImageLightbox({
  src,
  filename,
  onClose,
}: {
  src: string;
  filename: string;
  onClose: () => void;
}) {
  const dialogRef = useFocusTrap<HTMLDivElement>();
  return (
    <div
      ref={dialogRef}
      className="wk-viewer__lightbox"
      role="dialog"
      aria-modal="true"
      aria-label={`Image preview: ${filename}`}
    >
      {/*
        A full-bleed backdrop button carries the click-to-dismiss affordance
        so it is keyboard-operable on its own (Enter/Space) without a
        non-interactive element owning a click handler; Escape also closes
        it via the parent's keydown effect.
      */}
      <button
        type="button"
        className="wk-viewer__lightbox-backdrop"
        aria-label="Close image preview"
        onClick={onClose}
      />
      <button
        type="button"
        className="wk-viewer__lightbox-close"
        aria-label="Close image preview"
        onClick={onClose}
      >
        ×
      </button>
      {/* The image is presentational inside the labelled dialog. */}
      <img className="wk-viewer__lightbox-media" src={src} alt={filename} />
    </div>
  );
}

export default function ImageViewer({ path }: ImageViewerProps) {
  const [status, setStatus] = useState<LoadStatus>("loading");
  const [zoomed, setZoomed] = useState(false);
  // Track which path the current status describes. When `path` changes the
  // render below resets state synchronously (React's "adjust state during
  // render" pattern) so a new image never inherits the previous one's
  // ready/error flag or a stale open lightbox — and `path` is genuinely read
  // here, so no effect-dependency lint workaround is needed.
  const [loadedPath, setLoadedPath] = useState(path);

  const src = wikiFileUrl(path);
  const filename = path.split("/").pop() || path;

  if (loadedPath !== path) {
    setLoadedPath(path);
    setStatus("loading");
    setZoomed(false);
  }

  const openZoom = useCallback(() => {
    if (status === "ready") setZoomed(true);
  }, [status]);

  const closeZoom = useCallback(() => setZoomed(false), []);

  // Escape closes the lightbox. Only bound while open so it never swallows a
  // global Escape elsewhere in the wiki.
  useEffect(() => {
    if (!zoomed) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") closeZoom();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [zoomed, closeZoom]);

  return (
    <div className="wk-viewer wk-viewer--image">
      <div className="wk-viewer__toolbar">
        <span className="wk-viewer__filename" title={path}>
          {filename}
        </span>
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
          title="Open this image in a new browser tab"
        >
          Open in new tab
        </a>
      </div>

      <div className="wk-viewer__body">
        {status === "error" ? (
          <div className="wk-viewer__error" role="alert">
            Could not load image “{filename}”.
          </div>
        ) : (
          <>
            {status === "loading" ? (
              <div className="wk-viewer__loading" aria-hidden="true">
                Loading image…
              </div>
            ) : null}
            {/*
              A button wrapper makes zoom reachable by keyboard without putting
              an interactive role on the decorative <img>. The image stays
              mounted while loading so its native load/error events fire; it is
              hidden until ready to avoid a flash of a broken image.
            */}
            <button
              type="button"
              className="wk-viewer__zoom"
              aria-label={`Zoom image ${filename}`}
              hidden={status !== "ready"}
              onClick={openZoom}
            >
              <img
                className="wk-viewer__media"
                src={src}
                alt={filename}
                onLoad={() => setStatus("ready")}
                onError={() => setStatus("error")}
              />
            </button>
          </>
        )}
      </div>

      {zoomed ? (
        <ImageLightbox src={src} filename={filename} onClose={closeZoom} />
      ) : null}
    </div>
  );
}
