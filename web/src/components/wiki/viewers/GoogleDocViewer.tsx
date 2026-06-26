import { useEffect, useMemo, useState } from "react";

import { wikiFileUrl } from "@/api/wiki";

import {
  detectGoogle,
  googleKindLabel,
} from "../editor/refclone/lib/detect-google";

interface GoogleDocViewerProps {
  path: string;
}

interface Resolved {
  embedUrl: string;
  openUrl: string;
  label: string;
}

type ViewState =
  | { status: "loading" }
  | { status: "ready"; resolved: Resolved }
  | { status: "empty" } // file held no recognizable Google URL
  | { status: "error"; message: string };

/**
 * Extract a Google Workspace URL from a link file's text. Supports both:
 *  - Google Drive's `.gdoc`/`.gsheet`/… desktop-shortcut format (JSON with a
 *    `url` field), and
 *  - our plain `.glink` convention (the file body is the URL, optionally with
 *    surrounding lines).
 */
function extractUrl(text: string): string {
  const trimmed = text.trim();
  if (trimmed.startsWith("{")) {
    try {
      const parsed = JSON.parse(trimmed) as Record<string, unknown>;
      const url = parsed.url ?? parsed.URL ?? parsed.href;
      return typeof url === "string" ? url.trim() : "";
    } catch {
      return "";
    }
  }
  // First http(s) line wins; fall back to the whole trimmed body.
  const line = trimmed
    .split(/\r?\n/)
    .map((l) => l.trim())
    .find((l) => /^https?:\/\//i.test(l));
  return (line ?? trimmed).trim();
}

/**
 * Embeds a Google Doc / Sheet / Slides / Form / Drive file referenced by a
 * `.gdoc`/`.gsheet`/`.gslides`/`.gform`/`.glink` link file in the wiki. The
 * file's bytes are fetched through the wiki file API, the Google URL is
 * extracted and classified (see `detectGoogle`), and the embeddable variant is
 * rendered in an iframe. The iframe `src` is constrained to Google origins by
 * `detectGoogle`'s allowlist, so an arbitrary/unsafe URL never reaches it.
 */
export default function GoogleDocViewer({ path }: GoogleDocViewerProps) {
  const filename = useMemo(() => path.split("/").pop() || path, [path]);
  const [state, setState] = useState<ViewState>({ status: "loading" });

  useEffect(() => {
    let cancelled = false;
    setState({ status: "loading" });
    (async () => {
      try {
        const res = await fetch(wikiFileUrl(path));
        if (!res.ok) {
          throw new Error(`Could not load ${filename} (${res.status})`);
        }
        const url = extractUrl(await res.text());
        const google = detectGoogle(url);
        if (cancelled) return;
        if (!google) {
          setState({ status: "empty" });
          return;
        }
        setState({
          status: "ready",
          resolved: {
            embedUrl: google.embedUrl,
            openUrl: google.openUrl,
            label: googleKindLabel(google.kind),
          },
        });
      } catch (err: unknown) {
        if (cancelled) return;
        setState({
          status: "error",
          message: err instanceof Error ? err.message : "Failed to load file.",
        });
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [path, filename]);

  return (
    <section
      className="wk-viewer wk-viewer--google"
      aria-label={`Google document: ${filename}`}
    >
      <div className="wk-viewer__toolbar">
        <span title={path}>
          {state.status === "ready" ? state.resolved.label : "Google"} ·{" "}
          {filename}
        </span>
        <span style={{ flex: 1 }} />
        {state.status === "ready" && state.resolved.openUrl ? (
          <a
            className="wk-viewer__action"
            href={state.resolved.openUrl}
            target="_blank"
            rel="noreferrer"
            title="Open in Google"
          >
            Open in Google
          </a>
        ) : null}
      </div>
      {state.status === "ready" ? (
        <div
          className="wk-viewer__note"
          style={{
            padding: "6px 14px",
            fontSize: 11,
            color: "var(--wk-text-muted)",
            borderBottom: "1px solid var(--wk-border)",
            background: "var(--wk-paper-dark)",
          }}
        >
          Embedding requires the document's sharing to be “Anyone with the link”
          or “Published to the web”.
        </div>
      ) : null}
      <section className="wk-viewer__body" aria-label={`${filename} preview`}>
        {state.status === "loading" ? (
          <div className="wk-viewer__loading" role="status">
            Loading {filename}…
          </div>
        ) : state.status === "ready" ? (
          <iframe
            key={state.resolved.embedUrl}
            src={state.resolved.embedUrl}
            title={`Google document: ${filename}`}
            allow="clipboard-write; fullscreen"
            style={{ width: "100%", height: "100%", border: 0 }}
          />
        ) : state.status === "empty" ? (
          <div className="wk-viewer__loading" role="status">
            This file does not contain a recognized Google Docs, Sheets, Slides,
            Forms, or Drive link.
          </div>
        ) : (
          <div className="wk-viewer__loading" role="alert">
            {state.message}
          </div>
        )}
      </section>
    </section>
  );
}
