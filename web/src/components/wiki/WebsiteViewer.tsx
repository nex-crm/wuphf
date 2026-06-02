import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { appUrl } from "../../api/wiki";
import { useAppStore } from "../../stores/app";

// Mirrors the selector in useFocusTrap. We keep a local copy rather than
// reaching into that hook because our full-screen trap is boolean-gated (the
// overlay shares its DOM, iframe included, with the inline view) instead of
// mount-scoped like the hook.
const FOCUSABLE_SELECTOR = [
  "a[href]",
  "button:not([disabled])",
  "input:not([disabled])",
  "select:not([disabled])",
  "textarea:not([disabled])",
  '[tabindex]:not([tabindex="-1"])',
].join(",");

/**
 * Wrap focus to the opposite edge when Tab / Shift+Tab would otherwise leave
 * the full-screen container. Mirrors useFocusTrap's cycle, but the sandboxed
 * iframe is deliberately NOT treated as a trap edge: focus inside a cross-origin
 * frame is opaque to us, so the frame keeps native Tab behaviour while the
 * surrounding chrome cycles. Returns true when it handled the event (the caller
 * should then preventDefault).
 */
function cycleFullscreenTab(
  container: HTMLElement,
  shiftKey: boolean,
): boolean {
  const focusables = Array.from(
    container.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR),
  ).filter((el) => el.offsetParent !== null || el === container);
  if (focusables.length === 0) return true;
  const [first] = focusables;
  const last = focusables[focusables.length - 1];
  const active = document.activeElement;
  const atEdge = shiftKey
    ? active === first || !container.contains(active)
    : active === last || !container.contains(active);
  if (!atEdge) return false;
  (shiftKey ? last : first).focus();
  return true;
}

interface WebsiteViewerProps {
  /**
   * Repo-root-relative app/website folder under `team/` (e.g.
   * "team/site/dashboard"). The broker serves the folder's index.html and
   * resolves relative assets against it.
   */
  path: string;
  /** Optional display label; falls back to the folder's leaf name. */
  title?: string;
  /**
   * Leave the embedded surface and return to the previous view. Wired to the
   * wiki's navigation so Exit drops back to wherever the user came from
   * (catalog/article), not a dead end.
   */
  onExit?: () => void;
}

/**
 * WebsiteViewer — embeds an agent-authored app/website cabinet folder in a
 * sandboxed iframe pointed at the broker's GET /wiki/app/<folder>/index.html
 * route. Unlike the rich-artifact embed (which sanitises agent HTML into the
 * PARENT origin via shadow DOM), an app folder is a multi-file surface that
 * needs to run its own scripts and resolve relative assets, so it loads via
 * `src` into an iframe rather than `srcdoc`.
 *
 * SECURITY MODEL:
 *   - The iframe sandbox grants scripts/popups/forms/modals/downloads but
 *     deliberately OMITS `allow-same-origin`. Without it the browser assigns
 *     the frame a unique opaque origin, so the embedded app cannot read our
 *     cookies, localStorage, or reach back into the parent document — defence
 *     in depth alongside the response CSP the broker sets on /wiki/app.
 *   - `appUrl` carries NO auth token (the route is loopback-served); see the
 *     comment on appUrl for why a token in the URL would be a credential leak.
 *   - referrerPolicy="no-referrer" keeps the parent URL (which can carry
 *     workspace state) out of any request the app makes.
 *
 * Full-screen mode expands the frame to cover the workspace and collapses the
 * left sidebar via the shared app store, restoring the sidebar's prior state on
 * exit so we never strand the user with a hidden nav they did not collapse.
 */
export default function WebsiteViewer({
  path,
  title,
  onExit,
}: WebsiteViewerProps) {
  const src = useMemo(() => appUrl(path), [path]);
  const folderName = useMemo(() => path.split("/").pop() || path, [path]);
  const label = title ?? folderName;

  const [fullscreen, setFullscreen] = useState(false);
  // Announced to assistive tech on each full-screen transition. The element is
  // always mounted (sr-only) and we swap its text, the repo's live-region
  // convention (see WikiTree / TiptapWikiEditor), so AT reliably reads each
  // change rather than missing a conditionally-mounted node.
  const [announcement, setAnnouncement] = useState("");

  const sectionRef = useRef<HTMLElement | null>(null);
  // The full-screen toggle is the affordance focus returns to on exit, so AT
  // and keyboard users land back where they triggered the transition instead of
  // having focus dropped to <body>.
  const toggleRef = useRef<HTMLButtonElement | null>(null);

  const toggleSidebarCollapsed = useAppStore((s) => s.toggleSidebarCollapsed);

  // Remember whether the sidebar was already collapsed when we entered
  // full-screen so we restore the user's prior state (collapsed or not) on
  // exit, rather than always re-opening it. The store exposes a toggle (not a
  // setter), so we only flip it when the current state differs from the target.
  const priorSidebarCollapsed = useRef<boolean | null>(null);

  const enterFullscreen = useCallback(() => {
    priorSidebarCollapsed.current = useAppStore.getState().sidebarCollapsed;
    if (!useAppStore.getState().sidebarCollapsed) {
      toggleSidebarCollapsed();
    }
    setFullscreen(true);
    setAnnouncement("Entered full screen");
  }, [toggleSidebarCollapsed]);

  const exitFullscreen = useCallback(() => {
    const restore = priorSidebarCollapsed.current;
    if (
      restore !== null &&
      useAppStore.getState().sidebarCollapsed !== restore
    ) {
      toggleSidebarCollapsed();
    }
    priorSidebarCollapsed.current = null;
    setFullscreen(false);
    setAnnouncement("Exited full screen");
  }, [toggleSidebarCollapsed]);

  const toggleFullscreen = useCallback(() => {
    if (fullscreen) {
      exitFullscreen();
    } else {
      enterFullscreen();
    }
  }, [fullscreen, enterFullscreen, exitFullscreen]);

  // Escape leaves full-screen first (a single, predictable un-maximise), so the
  // user is never trapped behind a collapsed sidebar with no obvious way out.
  useEffect(() => {
    if (!fullscreen) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        exitFullscreen();
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [fullscreen, exitFullscreen]);

  // Full-screen focus containment. The overlay is position:fixed and covers the
  // workspace, but the shell behind it (sidebar rows, etc.) stays in the
  // document, so without a trap Tab would walk focus out behind the overlay.
  // role="dialog" + aria-modal communicates the modal boundary; this effect
  // enforces it for keyboard/AT:
  //   - on enter: move focus into the dialog (the toggle button, which lives in
  //     the dialog's toolbar) so focus is never stranded behind the overlay;
  //   - while open: Tab / Shift+Tab cycle within the section's focusable set —
  //     and crucially we do NOT count the sandboxed iframe as a trap edge, since
  //     focus inside a cross-origin frame is opaque to us, so the frame keeps
  //     normal Tab behaviour while the surrounding chrome cycles;
  //   - on exit: restore focus to the toggle button that opened full screen.
  // We cannot use the shared useFocusTrap hook here because it is mount-scoped
  // (traps on mount, restores on unmount) and our overlay shares its DOM —
  // including the iframe, which must not remount on toggle or the embedded app
  // would lose its in-frame state — with the non-full-screen view.
  useEffect(() => {
    if (!fullscreen) return;
    const section = sectionRef.current;
    if (!section) return;

    const previouslyFocused =
      document.activeElement instanceof HTMLElement
        ? document.activeElement
        : null;

    // Move focus into the dialog. The toggle button is the most predictable
    // landing spot — it is the control the user just activated and the one
    // focus returns to on exit.
    toggleRef.current?.focus();

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Tab") return;
      if (cycleFullscreenTab(section, event.shiftKey)) {
        event.preventDefault();
      }
    };

    section.addEventListener("keydown", onKeyDown);
    return () => {
      section.removeEventListener("keydown", onKeyDown);
      // Restore focus to the toggle if it is still mounted (it is, unless the
      // whole viewer unmounted — in which case there is nothing sensible to
      // restore to and the browser handles it); otherwise fall back to the
      // element that held focus before we entered, never leaving it on <body>.
      const restoreTarget = toggleRef.current ?? previouslyFocused;
      restoreTarget?.focus();
    };
  }, [fullscreen]);

  // Guard against unmounting while full-screen (e.g. the user navigates away
  // via a keyboard shortcut): restore the sidebar so it is never left collapsed
  // by a surface that is no longer on screen.
  useEffect(() => {
    return () => {
      const restore = priorSidebarCollapsed.current;
      if (
        restore !== null &&
        useAppStore.getState().sidebarCollapsed !== restore
      ) {
        toggleSidebarCollapsed();
      }
    };
  }, [toggleSidebarCollapsed]);

  // Full screen is a modal surface that covers the workspace, so mark it as a
  // dialog and aria-modal while maximised; inline it is just a labelled region
  // (no dialog role). Built as a spread so the modal attrs travel together and
  // the section never advertises aria-modal without the dialog role that
  // supports it.
  const modalProps:
    | { role: "dialog"; "aria-modal": true }
    | Record<never, never> = fullscreen
    ? { role: "dialog", "aria-modal": true }
    : {};

  return (
    <section
      ref={sectionRef}
      className={`wk-viewer wk-viewer--website${
        fullscreen ? " wk-viewer--fullscreen" : ""
      }`}
      data-testid="wk-website-viewer"
      data-fullscreen={fullscreen ? "true" : "false"}
      aria-label={`App: ${label}`}
      {...modalProps}
    >
      {/*
        Live region for full-screen transitions. Always mounted, sr-only, text
        swapped on toggle — the repo's announce-by-swap convention.
      */}
      <div className="sr-only" role="status" aria-live="polite">
        {announcement}
      </div>
      <div className="wk-viewer__toolbar">
        <span className="wk-viewer__filename" title={path}>
          {label}
        </span>
        <span className="wk-viewer__kind" aria-hidden="true">
          App
        </span>
        <span className="wk-website-frame-spacer" />
        <button
          ref={toggleRef}
          type="button"
          className="wk-viewer__action"
          aria-pressed={fullscreen}
          onClick={toggleFullscreen}
          title={fullscreen ? "Exit full screen" : "Full screen"}
        >
          {fullscreen ? "Exit full screen" : "Full screen"}
        </button>
        <a
          className="wk-viewer__action"
          href={src}
          target="_blank"
          rel="noreferrer noopener"
          title="Open this app in a new tab"
        >
          Open in new tab
        </a>
        {onExit ? (
          <button
            type="button"
            className="wk-viewer__action"
            onClick={() => {
              // Drop full-screen (and restore the sidebar) before leaving so we
              // never navigate away with the nav still collapsed.
              if (fullscreen) exitFullscreen();
              onExit();
            }}
            title="Close this app"
          >
            Exit
          </button>
        ) : null}
      </div>
      <div className="wk-viewer__body">
        {/*
          src (not srcdoc): an app folder is multi-file and needs its scripts to
          resolve relative assets against the /wiki/app/<folder>/ path. The
          sandbox intentionally omits allow-same-origin so the frame runs in an
          opaque origin and cannot touch the parent.
        */}
        <iframe
          // Remount when the target app changes so a previous app's in-frame
          // state never bleeds into the next.
          key={src}
          src={src}
          title={label}
          sandbox="allow-scripts allow-popups allow-forms allow-modals allow-downloads"
          referrerPolicy="no-referrer"
          className="wk-website-frame"
        />
      </div>
    </section>
  );
}
