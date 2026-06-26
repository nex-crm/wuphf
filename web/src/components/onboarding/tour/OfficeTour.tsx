/**
 * OfficeTour — modal host for the guided office tour.
 *
 * Renders a full-viewport overlay above the office Shell: a skip button
 * (top-right), the active slide, and a footer with Back / progress dots /
 * Next-or-Finish. Slide internals and the mock sidebar are owned by the
 * sibling slide components and office-tour-slides.css; this file owns only
 * the shell + transition + navigation wiring.
 *
 * Keyboard: Esc skips (persists done), ArrowRight advances or finishes,
 * ArrowLeft goes back. Slide-to-slide morphing uses the View Transitions
 * API when available, with a synchronous state update as the fallback. The
 * transition is never invoked under prefers-reduced-motion.
 *
 * Spec: docs/specs/office-onboarding-uplift.md section 4.
 */

import {
  type ComponentType,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";

import { Button } from "../../ui/Button";
import { SlideAgents } from "./SlideAgents";
import { SlideIntro } from "./SlideIntro";
import { SlideIssues } from "./SlideIssues";
import { SlideWiki } from "./SlideWiki";
import {
  OFFICE_TOUR_COPY,
  OFFICE_TOUR_LABELS,
  OFFICE_TOUR_SLIDE_IDS,
  type OfficeTourSlideId,
  type OfficeTourSlideProps,
} from "./tourSlides";

interface OfficeTourProps {
  open: boolean;
  onClose: () => void;
  onFinish: () => void;
}

/**
 * Tab-focusable elements, mirrors the app's SidePanel trap selector so the
 * modal contains focus consistently with other dialogs.
 */
const FOCUSABLE_SELECTOR = [
  "a[href]",
  "button:not([disabled])",
  "textarea:not([disabled])",
  "input:not([disabled])",
  "select:not([disabled])",
  '[tabindex]:not([tabindex="-1"])',
].join(",");

/**
 * Keep Tab focus inside the overlay. `aria-modal` alone does not stop Tab from
 * walking into the office Shell behind the overlay, so cycle at the boundaries.
 * Mirrors the app's SidePanel trap.
 */
function trapFocus(event: KeyboardEvent, root: HTMLElement | null): void {
  if (!root) return;
  const focusables = Array.from(
    root.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR),
  );
  if (focusables.length === 0) {
    event.preventDefault();
    root.focus();
    return;
  }
  const [first] = focusables;
  const last = focusables[focusables.length - 1];
  const active = document.activeElement;
  if (event.shiftKey && (active === first || active === root)) {
    event.preventDefault();
    last.focus();
  } else if (!event.shiftKey && active === last) {
    event.preventDefault();
    first.focus();
  }
}

/**
 * Id → slide component. Each slide conforms to OfficeTourSlideProps. The
 * order of navigation comes from OFFICE_TOUR_SLIDE_IDS, not this map.
 */
const SLIDES: Record<OfficeTourSlideId, ComponentType<OfficeTourSlideProps>> = {
  intro: SlideIntro,
  agents: SlideAgents,
  issues: SlideIssues,
  wiki: SlideWiki,
};

/**
 * Run `update` inside a View Transition when the API exists and motion is
 * allowed; otherwise call it synchronously. Feature-detected per call so we
 * never assume the API on browsers that lack it.
 */
function runWithTransition(update: () => void): void {
  if (typeof document === "undefined") {
    update();
    return;
  }
  const prefersReducedMotion =
    typeof window !== "undefined" &&
    typeof window.matchMedia === "function" &&
    window.matchMedia("(prefers-reduced-motion: reduce)").matches;

  const doc = document as Document & {
    startViewTransition?: (cb: () => void) => unknown;
  };

  if (prefersReducedMotion || typeof doc.startViewTransition !== "function") {
    update();
    return;
  }
  doc.startViewTransition(update);
}

export function OfficeTour({ open, onClose, onFinish }: OfficeTourProps) {
  const [index, setIndex] = useState(0);
  const overlayRef = useRef<HTMLDivElement>(null);
  const prevFocusRef = useRef<HTMLElement | null>(null);

  const total = OFFICE_TOUR_SLIDE_IDS.length;
  const lastIndex = total - 1;
  const isLast = index >= lastIndex;
  const activeId = OFFICE_TOUR_SLIDE_IDS[index];
  // Zero-padded "02 / 04" wayfinding marker, rendered top-left to balance the
  // skip control top-right and tell the user how far through the tour they are.
  const stepLabel = `${String(index + 1).padStart(2, "0")} / ${String(
    total,
  ).padStart(2, "0")}`;

  // Reset to the first slide whenever the tour (re)opens, so a replay always
  // starts at the intro rather than wherever the user last left off.
  useEffect(() => {
    if (open) setIndex(0);
  }, [open]);

  const goNext = useCallback(() => {
    runWithTransition(() => {
      setIndex((current) => Math.min(current + 1, lastIndex));
    });
  }, [lastIndex]);

  const goBack = useCallback(() => {
    runWithTransition(() => {
      setIndex((current) => Math.max(current - 1, 0));
    });
  }, []);

  const finish = useCallback(() => {
    onFinish();
    onClose();
  }, [onFinish, onClose]);

  // The primary footer button advances on every slide except the last, where
  // it becomes the finish CTA that hands the user off into a composer.
  const onPrimary = useCallback(() => {
    if (isLast) {
      finish();
    } else {
      goNext();
    }
  }, [isLast, finish, goNext]);

  // Keyboard navigation. Bound only while open. Esc skips (persists done via
  // onClose), arrows move between slides / trigger finish on the last slide.
  useEffect(() => {
    if (!open) return;
    const handler = (event: KeyboardEvent) => {
      switch (event.key) {
        case "Escape":
          event.preventDefault();
          onClose();
          break;
        case "ArrowRight":
          event.preventDefault();
          onPrimary();
          break;
        case "ArrowLeft":
          event.preventDefault();
          if (index > 0) goBack();
          break;
        case "Tab":
          trapFocus(event, overlayRef.current);
          break;
        default:
          break;
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [open, onClose, onPrimary, goBack, index]);

  // Move focus into the overlay on open so the keyboard handler and screen
  // readers have a sensible anchor (the overlay is focusable via tabIndex),
  // and restore focus to wherever it was on close so a keyboard user is never
  // left in a focusless state after Esc / Skip / Finish. Mirrors the focus
  // discipline in HelpModal.
  useEffect(() => {
    if (open) {
      prevFocusRef.current =
        document.activeElement instanceof HTMLElement
          ? document.activeElement
          : null;
      overlayRef.current?.focus();
    } else {
      prevFocusRef.current?.focus();
      prevFocusRef.current = null;
    }
  }, [open]);

  if (!open) return null;

  const ActiveSlide = SLIDES[activeId];
  const copy = OFFICE_TOUR_COPY[activeId];

  return (
    <div
      ref={overlayRef}
      className="office-tour-overlay"
      role="dialog"
      aria-modal="true"
      aria-label={OFFICE_TOUR_LABELS.dialog}
      tabIndex={-1}
      data-testid="office-tour"
    >
      {/* Top-left wayfinding marker. Decorative for SR (the footer dot rail
          already announces "Step N of M"); this is the visual counterpart. */}
      <span className="office-tour-step" aria-hidden="true">
        {stepLabel}
      </span>

      <button
        type="button"
        className="office-tour-skip"
        onClick={onClose}
        data-testid="office-tour-skip"
      >
        {OFFICE_TOUR_LABELS.skip}
      </button>

      <div className="office-tour-body" data-slide={activeId}>
        {/* key={activeId} remounts the slide on change so its entrance
            animation replays; `active` is always true for the mounted
            slide and tells the slide to play its entrance. */}
        <ActiveSlide key={activeId} active={true} />
      </div>

      <footer className="office-tour-footer">
        <div className="office-tour-footer-side">
          {index > 0 ? (
            <Button
              type="button"
              variant="ghost"
              onClick={goBack}
              data-testid="office-tour-back"
            >
              {OFFICE_TOUR_LABELS.back}
            </Button>
          ) : (
            // Slide 1 has no Back, so the empty cell carries the keyboard
            // affordance instead of reading as dead space.
            <span className="office-tour-kbd-hint" aria-hidden="true">
              Use <kbd>←</kbd> <kbd>→</kbd> to move
            </span>
          )}
        </div>

        {/* Decorative progress indicator, not a tab strip: the dots are not
            clickable, so they carry no interactive role. A single labelled
            role="img" announces "step N of M" to assistive tech; individual
            dots are hidden from the a11y tree. */}
        <div
          className="office-tour-dots"
          role="img"
          aria-label={`Step ${index + 1} of ${OFFICE_TOUR_SLIDE_IDS.length}`}
        >
          {OFFICE_TOUR_SLIDE_IDS.map((id, i) => (
            <span
              key={id}
              className={`office-tour-dot${i === index ? " office-tour-dot-active" : ""}`}
              aria-hidden="true"
              data-testid={`office-tour-dot-${id}`}
            />
          ))}
        </div>

        <div className="office-tour-footer-side office-tour-footer-side-end">
          <Button
            type="button"
            onClick={onPrimary}
            data-testid="office-tour-primary"
          >
            {isLast ? OFFICE_TOUR_LABELS.finish : OFFICE_TOUR_LABELS.next}
          </Button>
        </div>
      </footer>

      {/* Visually-hidden live region: announces the current slide headline to
          assistive tech on each navigation. Headline-only (no static prefix)
          so the announcement is correct whether the user moved forward or
          back, and so screen-reader users hear the slide that changed rather
          than boilerplate. */}
      <p className="office-tour-transition-fallback" aria-live="polite">
        {copy.headline}
      </p>
    </div>
  );
}
