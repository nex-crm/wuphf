/**
 * DocxViewer — renders a Word document (.docx) inside the wiki using the
 * `docx-preview` library. The lib parses the OOXML and writes the rendered
 * DOM directly into a container element we own, so we render into a `ref`
 * rather than setting inner HTML ourselves.
 *
 * The dispatcher React.lazy-loads this whole module, so the static
 * `docx-preview` import lands in this viewer's own chunk (correct code
 * splitting). We never re-import it dynamically.
 *
 * The rendered bytes come from the team's own wiki, served through the
 * authenticated `/wiki/file` endpoint (see `wikiFileUrl`), not from
 * untrusted external input. The viewer never sets inner HTML itself — the
 * library writes into a container ref we own and we tear it down on unmount.
 *
 * Security: docx-preview emits hyperlink markup verbatim from the document,
 * including any `href` the document author embedded — `javascript:` and
 * `data:` schemes included. Those land in our own origin's DOM, so after the
 * render resolves we walk every anchor the library injected and neutralize any
 * href whose scheme is not http(s) / mailto / a same-document fragment / a
 * relative path. Disallowed hrefs are stripped so the link text still renders
 * but the navigation is inert.
 */

import { useEffect, useRef, useState } from "react";
import { renderAsync } from "docx-preview";

import { wikiFileUrl } from "../../../api/wiki";

interface DocxViewerProps {
  path: string;
}

type ViewState = "loading" | "ready" | "error";

/**
 * Schemes we allow on rendered hyperlinks. Anything else — notably
 * `javascript:` and `data:` — is stripped so the anchor cannot navigate.
 */
const SAFE_LINK_SCHEMES = new Set(["http:", "https:", "mailto:"]);

/**
 * Return true when `href` is safe to keep on a rendered anchor: a same-document
 * fragment, a relative path, or an absolute URL using an allow-listed scheme.
 * Anything that resolves to a disallowed scheme (javascript:, data:, vbscript:,
 * file:, …) is rejected.
 */
function isSafeHref(href: string): boolean {
  const trimmed = href.trim();
  if (trimmed === "") return false;
  // Same-document fragment — never navigates cross-origin.
  if (trimmed.startsWith("#")) return true;
  try {
    // Resolve against a dummy base so relative paths parse; an absolute URL
    // keeps its own scheme. We only trust the resolved protocol.
    const url = new URL(trimmed, "https://wuphf.invalid/");
    return SAFE_LINK_SCHEMES.has(url.protocol);
  } catch {
    return false;
  }
}

/**
 * Walk every anchor docx-preview injected into `container` and neutralize any
 * unsafe href. We drop the attribute entirely (rather than rewriting it) so the
 * link text still renders but the element no longer navigates anywhere.
 */
function sanitizeRenderedLinks(container: HTMLElement): void {
  for (const anchor of Array.from(container.querySelectorAll("a[href]"))) {
    const href = anchor.getAttribute("href") ?? "";
    if (!isSafeHref(href)) {
      anchor.removeAttribute("href");
      anchor.removeAttribute("target");
    }
  }
}

/**
 * Fetch the document bytes and render them into `container`. Kept as a
 * top-level helper (rather than inlined in the effect) so the effect body
 * stays flat and the fetch/render flow is independently testable.
 */
async function loadDocument(
  path: string,
  container: HTMLElement,
): Promise<void> {
  const res = await fetch(wikiFileUrl(path));
  if (!res.ok) {
    throw new Error(`Could not load document (HTTP ${res.status})`);
  }
  const blob = await res.blob();
  await renderAsync(blob, container, undefined, {
    className: "wk-docx",
    inWrapper: true,
    ignoreWidth: false,
    ignoreHeight: false,
    breakPages: true,
    renderHeaders: true,
    renderFooters: true,
    renderFootnotes: true,
    experimental: true,
    useBase64URL: true,
  });
  // docx-preview wrote untrusted hyperlink hrefs into our origin DOM; strip any
  // that would navigate to a dangerous scheme before the user can click them.
  sanitizeRenderedLinks(container);
}

export default function DocxViewer({ path }: DocxViewerProps) {
  const containerRef = useRef<HTMLElement | null>(null);
  const [state, setState] = useState<ViewState>("loading");
  const [errorMessage, setErrorMessage] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    const container = containerRef.current;
    if (!container) return;

    // Clear any DOM the previous render injected before starting a new one.
    container.replaceChildren();
    setState("loading");
    setErrorMessage(null);

    loadDocument(path, container)
      .then(() => {
        if (!cancelled) setState("ready");
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setErrorMessage(
          err instanceof Error ? err.message : "Failed to render document",
        );
        setState("error");
      });

    return () => {
      cancelled = true;
      // Tear down the injected DOM so a remount or path change starts clean.
      container.replaceChildren();
    };
  }, [path]);

  const fileName = path.split("/").pop() ?? path;
  const fileUrl = wikiFileUrl(path);

  return (
    <section className="wk-viewer wk-viewer--docx" aria-label={fileName}>
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
          title="Open this document in a new browser tab"
        >
          Open in new tab
        </a>
      </div>
      {state === "loading" && (
        <div className="wk-viewer__loading" role="status">
          Rendering document…
        </div>
      )}
      {state === "error" && (
        <div className="wk-viewer__error" role="alert">
          <p>{errorMessage ?? "Failed to render document."}</p>
          <p>
            Download the file or open it in a new tab to read it in a Word
            editor.
          </p>
        </div>
      )}
      {/*
        The body is always mounted (even while loading/errored) because
        docx-preview needs the container element present in the DOM to write
        into. We hide it until ready via the error/loading overlays above. It
        is a named <section> so it reads as a region landmark; we omit a
        focusable tabIndex because the rendered document scrolls with the
        pointer and its links remain individually reachable.
      */}
      <section
        ref={containerRef}
        className="wk-viewer__body"
        aria-label={`${fileName} document`}
        hidden={state !== "ready"}
      />
    </section>
  );
}
