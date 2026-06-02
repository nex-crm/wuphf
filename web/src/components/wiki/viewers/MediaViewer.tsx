import { useState } from "react";

import { wikiFileUrl } from "../../../api/wiki";

interface MediaViewerProps {
  path: string;
}

const VIDEO_EXTS = new Set(["mp4", "webm", "mov", "m4v"]);
const AUDIO_EXTS = new Set(["mp3", "wav", "ogg", "m4a", "aac", "flac"]);

type MediaKind = "video" | "audio";

/**
 * Classify a wiki path by its extension. Returns `null` for anything this
 * viewer cannot play so the dispatcher's routing never silently mounts an
 * empty `<video>`/`<audio>` element.
 */
function mediaKind(path: string): MediaKind | null {
  const ext = path.split(".").pop()?.toLowerCase() ?? "";
  if (VIDEO_EXTS.has(ext)) return "video";
  if (AUDIO_EXTS.has(ext)) return "audio";
  return null;
}

/**
 * Plays a wiki video or audio file with the browser's native controls.
 *
 * The element `src` is `wikiFileUrl(path)` — the broker handles HTTP Range
 * streaming server-side, so seeking and partial loads work without any client
 * coordination. Extension picks the element: mp4/webm/mov/m4v render in a
 * `<video controls>`, while mp3/wav/ogg/m4a/aac/flac render in an
 * `<audio controls>`.
 *
 * Media elements report failures through their native `error` event rather
 * than a rejected promise, so we drive the shared loading/error states off
 * `onError` and the readiness events (`onLoadedData` for video,
 * `onCanPlay` for audio).
 */
export default function MediaViewer({ path }: MediaViewerProps) {
  const [status, setStatus] = useState<"loading" | "ready" | "error">(
    "loading",
  );
  // Track which path the current status describes so a path change resets the
  // ready/error flag synchronously during render (React's "adjust state during
  // render" pattern), without an effect whose only job is mirroring a prop.
  const [loadedPath, setLoadedPath] = useState(path);

  const src = wikiFileUrl(path);
  const filename = path.split("/").pop() || path;
  const kind = mediaKind(path);

  if (loadedPath !== path) {
    setLoadedPath(path);
    setStatus("loading");
  }

  if (!kind) {
    return (
      <div className="wk-viewer wk-viewer--media">
        <div className="wk-viewer__toolbar">
          <span className="wk-viewer__filename" title={path}>
            {filename}
          </span>
        </div>
        <div className="wk-viewer__body">
          <div className="wk-viewer__empty">
            “{filename}” is not a playable media file.
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className={`wk-viewer wk-viewer--media wk-viewer--${kind}`}>
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
          title={`Open this ${kind} in a new browser tab`}
        >
          Open in new tab
        </a>
      </div>

      <div className="wk-viewer__body">
        {status === "error" ? (
          <div className="wk-viewer__error" role="alert">
            Could not play {kind} file “{filename}”.
          </div>
        ) : (
          <>
            {status === "loading" ? (
              <div className="wk-viewer__loading" aria-hidden="true">
                Loading {kind}…
              </div>
            ) : null}
            {kind === "video" ? (
              <video
                className="wk-viewer__media"
                src={src}
                controls={true}
                preload="metadata"
                hidden={status !== "ready"}
                aria-label={`Video: ${filename}`}
                onLoadedData={() => setStatus("ready")}
                onError={() => setStatus("error")}
              >
                <track kind="captions" />
              </video>
            ) : (
              <div className="wk-viewer__audio-card">
                <p className="wk-viewer__audio-name" title={filename}>
                  {filename}
                </p>
                <audio
                  className="wk-viewer__audio"
                  src={src}
                  controls={true}
                  preload="metadata"
                  hidden={status !== "ready"}
                  aria-label={`Audio: ${filename}`}
                  onCanPlay={() => setStatus("ready")}
                  onError={() => setStatus("error")}
                >
                  <track kind="captions" />
                </audio>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}
