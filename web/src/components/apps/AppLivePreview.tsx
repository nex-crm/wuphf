import { useQuery } from "@tanstack/react-query";

import { ensureAppDev } from "../../api/apps";
import {
  type AppErrorPayload,
  type AppSelectPayload,
  CustomAppFrame,
} from "./CustomAppFrame";

const DEV_POLL_MS = 1500;

/**
 * The dev preview iframe loads `devUrl` directly with allow-same-origin, so the
 * URL is security-critical: it must be a loopback http origin (the broker always
 * returns one, but the iframe sandbox model can't depend on a server invariant).
 * Reject anything else rather than frame a non-loopback origin.
 */
function isLoopbackDevUrl(url: string | undefined): url is string {
  if (!url) return false;
  try {
    const u = new URL(url);
    return (
      u.protocol === "http:" &&
      (u.hostname === "127.0.0.1" || u.hostname === "localhost")
    );
  } catch {
    return false;
  }
}

interface AppLivePreviewProps {
  appId: string;
  title: string;
  /** Dev-only: toggle the in-app "select to edit" inspector. */
  selectMode?: boolean;
  /** Dev-only: a selected element resolved to a source location. */
  onSelect?: (sel: AppSelectPayload) => void;
  /** Dev-only: a runtime error surfaced from inside the running app. */
  onAppError?: (err: AppErrorPayload) => void;
}

/**
 * AppLivePreview runs the app's live dev server (Vite + HMR behind the broker's
 * CSP-injecting proxy) and renders it the instant it boots — edits hot-reload in
 * the frame with no rebuild. While the server is installing/starting it streams
 * the boot log instead of a frozen placeholder, so the wait is never dead air.
 */
export function AppLivePreview({
  appId,
  title,
  selectMode,
  onSelect,
  onAppError,
}: AppLivePreviewProps) {
  const { data, isError, error } = useQuery({
    queryKey: ["app-dev", appId],
    queryFn: () => ensureAppDev(appId),
    // Poll while booting; stop once the server is ready or has errored.
    refetchInterval: (query) => {
      const s = query.state.data;
      return s?.ready || s?.error ? false : DEV_POLL_MS;
    },
  });

  if (isError) {
    return (
      <div className="app-build-preview__state" role="alert">
        <p className="app-build-preview__state-title">Preview unavailable</p>
        <p className="app-build-preview__state-detail">
          {error instanceof Error
            ? error.message
            : "Could not start the live preview."}
        </p>
      </div>
    );
  }

  if (data?.error) {
    return (
      <div className="app-build-preview__bootlog" role="alert">
        <p className="app-build-preview__state-title">
          Preview failed to start
        </p>
        <pre className="app-build-preview__bootlog-text">
          {data.boot_log || data.error}
        </pre>
      </div>
    );
  }

  // Ready, but the broker handed back something that isn't a loopback origin —
  // never frame it (defense-in-depth for the sandbox model).
  if (data?.ready && !isLoopbackDevUrl(data.url)) {
    return (
      <div className="app-build-preview__state" role="alert">
        <p className="app-build-preview__state-title">Preview blocked</p>
        <p className="app-build-preview__state-detail">
          The preview server returned an unexpected address, so it was not
          loaded.
        </p>
      </div>
    );
  }

  if (!(data?.ready && isLoopbackDevUrl(data.url))) {
    return (
      <div className="app-build-preview__state" role="status">
        <span className="app-build-preview__spinner" aria-hidden="true" />
        <p className="app-build-preview__state-title">Starting live preview…</p>
        {data?.boot_log ? (
          <pre className="app-build-preview__bootlog-text">{data.boot_log}</pre>
        ) : (
          <p className="app-build-preview__state-detail">
            Installing dependencies and booting the dev server — this is a one
            time warm-up.
          </p>
        )}
      </div>
    );
  }

  return (
    <CustomAppFrame
      appId={appId}
      devUrl={data.url}
      title={title}
      selectMode={selectMode}
      onSelect={onSelect}
      onAppError={onAppError}
    />
  );
}
